package compute

import (
	"context"
	"io"
	"math"
	"runtime"
	"sync/atomic"
	"time"

	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/compute/kernel/metal"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
)

// SubstrateStats tracks the pressure for a specific compute kernel
type SubstrateStats struct {
	Substrate kernel.Substrate
	Inflight  atomic.Int64
	EmaTime   atomic.Int64 // Exponential Moving Average of execution time in nanoseconds
}

// Score calculates the current pressure of this substrate.
// Lower is better. Score = Inflight * EMA Service Time.
func (s *SubstrateStats) Score() int64 {
	inflight := s.Inflight.Load()
	ema := s.EmaTime.Load()
	if ema == 0 {
		ema = 1000 // default baseline if no tasks have run yet
	}
	return inflight * ema
}

func (s *SubstrateStats) RecordExecution(duration time.Duration) {
	// Simple EMA: NewEMA = (CurrentDuration * 0.2) + (OldEMA * 0.8)
	old := s.EmaTime.Load()
	newEma := int64(float64(duration.Nanoseconds())*0.2 + float64(old)*0.8)
	s.EmaTime.Store(newEma)
}

/*
Backend is a small load balancer over compute substrates (CUDA, Metal, CPU).
It picks the lowest-pressure candidate using inflight × EMA service time.
*/
type Backend struct {
	ctx        context.Context
	cancel     context.CancelFunc
	err        error
	substrates []*SubstrateStats
	cpuStats   *SubstrateStats
	pool       *pool.Pool
	queue      *Queue
	output     io.Writer
	popped     atomic.Int64
}

func NewBackend(ctx context.Context, q *Queue, out io.Writer) *Backend {
	ctx, cancel := context.WithCancel(ctx)

	// We reserve 1 goroutine for the orchestrator loop so it doesn't
	// compete with the worker pool for OS threads.
	poolSize := uint64(runtime.NumCPU())
	if poolSize > 1 {
		poolSize--
	}

	backend := &Backend{
		ctx:    ctx,
		cancel: cancel,
		err:    nil,
		pool:   pool.NewPool(poolSize),
		queue:  q,
		output: out,
	}

	for device := 0; device < cuda.Available(); device++ {
		backend.substrates = append(backend.substrates, &SubstrateStats{
			Substrate: cuda.NewBackend(device),
		})
	}

	for device := 0; device < metal.Available(); device++ {
		backend.substrates = append(backend.substrates, &SubstrateStats{
			Substrate: metal.NewBackend(device),
		})
	}

	cpuBackend := cpu.NewBackend(ctx)
	backend.cpuStats = &SubstrateStats{Substrate: cpuBackend}
	backend.substrates = append(backend.substrates, backend.cpuStats)

	// Start the orchestrator loop
	go backend.Run()

	return backend
}

/*
Run is the main orchestrator loop. It pulls work from the queue,
schedules it on the pool, and processes the results.
*/
/*
Inflight returns the total number of tasks currently executing across all substrates.
*/
func (b *Backend) Inflight() int {
	if b == nil {
		return 0
	}

	total := int(b.popped.Load())
	for _, st := range b.substrates {
		total += int(st.Inflight.Load())
	}

	return total
}

func (b *Backend) Run() {
	for {
		select {
		case <-b.ctx.Done():
			return
		default:
			// Get the next highest priority work item from the Queue
			// (This blocks until work is available)
			work, err := b.queue.Pop()
			b.popped.Add(1)

			if err != nil {
				b.popped.Add(-1)
				errnie.Error(err)
				break
			}

			if work.Type == WorkTypeFunc {
				// Functions MUST run on the CPU.
				// We increment CPU inflight, run it, and decrement.
				b.cpuStats.Inflight.Add(1)
				b.popped.Add(-1)

				b.pool.Schedule(func() {
					defer b.cpuStats.Inflight.Add(-1)
					work.Function()
				})

				break
			}

			if work.Type == WorkTypeValue {
				// Values can run anywhere. Find the lowest pressure substrate!
				best := b.findLowestPressureSubstrate()
				best.Inflight.Add(1)
				b.popped.Add(-1)

				b.pool.Schedule(func() {
					defer best.Inflight.Add(-1)

					start := time.Now()

					// Execute on the chosen kernel
					frames := []unsafe.Pointer{unsafe.Pointer(work.Value)}
					indices, err := primitive.IndicesFromPointers(frames)

					var execErr error

					if err == nil {
						execErr = best.Substrate.Execute(indices)
					} else {
						// Fallback to CPU for non-arena frames
						if cb, ok := best.Substrate.(*cpu.Backend); ok {
							execErr = cb.ExecutePointers(frames)
						} else {
							// If best wasn't CPU, force it to CPU
							if cb, ok := b.cpuStats.Substrate.(*cpu.Backend); ok {
								execErr = cb.ExecutePointers(frames)
							}
						}
					}

					if execErr != nil {
						// Handle error if needed
						errnie.Error(execErr)
						return
					}

					errVal, _ := work.Value.Property(primitive.LABELS)
					beliefGap := float64(errVal) / 512.0

					if work.Value.SchedulingNext() == 0 {
						work.Value.Set(core.Cfg.Value.Region.Properties.Start+int(primitive.STATUS), uint64(primitive.RESOLVED))
					} else if beliefGap <= core.Cfg.System.BeliefEpsilon {
						work.Value.Set(core.Cfg.Value.Region.Properties.Start+int(primitive.STATUS), uint64(primitive.RESOLVED))
						work.Value.SetSchedulingNext(0)
					} else {
						work.Value.Set(core.Cfg.Value.Region.Properties.Start+int(primitive.STATUS), uint64(primitive.READY))
					}

					if work.Value.EmitRequested() {
						child := primitive.EmitCloneHost(work.Value)
						if child != nil {
							child.Set(core.Cfg.Value.Region.Prev.Start, work.Value.ID())
							work.Value.Set(core.Cfg.Value.Region.Next.Start, child.ID())
							child.Set(core.Cfg.Value.Region.Properties.Start+int(primitive.EMIT), 0)

							if b.output != nil {
								if _, err := io.Copy(b.output, child); err != nil {
									errnie.Error(err)
								}
							}
						}
					}

					// Record how long it took to update the EMA budget
					best.RecordExecution(time.Since(start))

					// Update kernels if needed
					b.updateKernels(work.Value)

					// Stream the result into the IO pipeline!
					if b.output != nil {
						_, err := io.Copy(b.output, work.Value)

						if err != nil {
							errnie.Error(err)
						}
					}
				})
			}
		}
	}
}

func (b *Backend) findLowestPressureSubstrate() *SubstrateStats {
	var best *SubstrateStats
	var lowestScore int64 = math.MaxInt64

	for _, stats := range b.substrates {
		score := stats.Score()
		if score < lowestScore {
			lowestScore = score
			best = stats
		}
	}
	return best
}

func (b *Backend) updateKernels(result *primitive.Value) {
	// Implement any kernel updates here based on the result
}
