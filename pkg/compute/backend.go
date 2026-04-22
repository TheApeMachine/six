package compute

import (
	"context"
	"math"
	"runtime"
	"sync/atomic"
	"time"

	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/compute/kernel/metal"
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
	popped     atomic.Int64
}

func NewBackend(ctx context.Context, q *Queue) *Backend {
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
				errnie.Trace("backend.Run: scheduling function")
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
				errnie.Trace("backend.Run: scheduling value")
				// Values can run anywhere. Find the lowest pressure substrate!
				best := b.findLowestPressureSubstrate()
				best.Inflight.Add(1)
				b.popped.Add(-1)

				b.pool.Schedule(func() {
					defer best.Inflight.Add(-1)
					defer primitive.FreeValue(work.Value)
					start := time.Now()

					// Execute on the chosen kernel
					frames := []unsafe.Pointer{unsafe.Pointer(work.Value)}
					indices, err := primitive.IndicesFromPointers(frames)

					if err = best.Substrate.Execute(indices); err != nil {
						errnie.Error(err)
						return
					}

					// Record how long it took to update the EMA budget
					best.RecordExecution(time.Since(start))

					// Update kernels if needed
					b.updateKernels(work.Value)

					// Return the computed result to the queue's output stream
					if err := b.queue.Return(work.Value); err != nil {
						errnie.Error(err)
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

// nearestAffinitySubstrate is implemented by CUDA/Metal backends (and test spies) for
// packed batch Hamming distance; the generic kernel.Substrate contract stays minimal.
type nearestAffinitySubstrate interface {
	NearestAffinity(query unsafe.Pointer, candidates unsafe.Pointer, count int) ([]uint32, error)
}

// affinityBatchRowWords is the packed row width for query/candidate buffers shared by
// cpu.AffinityDistances and GPU NearestAffinity kernels. Keep in sync with
// kernel/cpu/affinity_distances.go (affinityDistanceVectorWords).
const affinityBatchRowWords = 8

/*
AffinityDistances returns per-candidate Hamming distance from query to each row.
It prefers the lowest-pressure substrate that supports NearestAffinity, then falls
back to the CPU batch implementation (same as mesh AffinityRouter wiring).
*/
func (b *Backend) AffinityDistances(
	query *[primitive.AffinityWords]uint64,
	candidates [][primitive.AffinityWords]uint64,
) []uint32 {
	if b == nil || query == nil || len(candidates) == 0 {
		return nil
	}

	if best := b.findLowestPressureSubstrate(); best != nil {
		if sub, ok := best.Substrate.(nearestAffinitySubstrate); ok {
			var packedQuery [affinityBatchRowWords]uint64
			copy(packedQuery[:], query[:])

			packedCandidates := make([]uint64, len(candidates)*affinityBatchRowWords)
			for i, row := range candidates {
				base := i * affinityBatchRowWords
				copy(packedCandidates[base:base+primitive.AffinityWords], row[:])
			}

			if dist, err := sub.NearestAffinity(
				unsafe.Pointer(&packedQuery[0]),
				unsafe.Pointer(&packedCandidates[0]),
				len(candidates),
			); err == nil {
				return dist
			}
		}
	}

	return cpu.AffinityDistances(query, candidates)
}

func (b *Backend) updateKernels(result *primitive.Value) {
	// Implement any kernel updates here based on the result
}
