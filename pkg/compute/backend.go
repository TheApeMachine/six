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
	// emitter is the post-ALU outbound publisher. The orchestrator wires
	// this to its mesh.Field so structural Associations minted by the
	// post-exec hook (see postexec.go) flow back into the routing layer
	// as honest residents instead of getting orphaned. Nil emitter means
	// the post-hook is a no-op — a useful default for unit tests that
	// only exercise kernel dispatch.
	emitter Emitter
}

// SetEmitter installs (or replaces) the outbound publisher used by the
// post-ALU hook to push freshly-minted Association Values back into the
// substrate. Safe to call once before traffic starts; the field pointer
// inside the orchestrator is stable for the lifetime of a Cycle, so a
// single SetEmitter call after NewBackend covers the whole Machine.
func (b *Backend) SetEmitter(emitter Emitter) {
	if b == nil {
		return
	}

	b.emitter = emitter
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
				best := b.selectSubstrate(work.Value)
				if best == nil {
					b.popped.Add(-1)
					primitive.FreeValue(work.Value)
					break
				}

				best.Inflight.Add(1)
				b.popped.Add(-1)

				b.pool.Schedule(func() {
					defer best.Inflight.Add(-1)
					defer primitive.FreeValue(work.Value)
					start := time.Now()

					if err := b.executeOnSubstrate(best, work.Value); err != nil {
						errnie.Error(err)
						return
					}

					best.RecordExecution(time.Since(start))
					b.updateKernels(work.Value)

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

func (b *Backend) selectSubstrate(value *primitive.Value) *SubstrateStats {
	if b == nil {
		return nil
	}

	if value != nil {
		if _, ok := primitive.ArenaIndex(value); !ok && b.cpuStats != nil {
			return b.cpuStats
		}
	}

	if best := b.findLowestPressureSubstrate(); best != nil {
		return best
	}

	return b.cpuStats
}

func (b *Backend) executeOnSubstrate(stats *SubstrateStats, value *primitive.Value) error {
	if stats == nil || stats.Substrate == nil || value == nil {
		return nil
	}

	if idx, ok := primitive.ArenaIndex(value); ok {
		return stats.Substrate.Execute([]uint32{idx})
	}

	ptrExec, ok := stats.Substrate.(pointerExecutingSubstrate)
	if !ok {
		return primitive.ErrNotArenaValue
	}

	return ptrExec.ExecutePointers([]unsafe.Pointer{unsafe.Pointer(value)})
}

func (b *Backend) executeValue(value *primitive.Value) (*SubstrateStats, error) {
	stats := b.selectSubstrate(value)
	if err := b.executeOnSubstrate(stats, value); err != nil {
		return stats, err
	}

	return stats, nil
}

// nearestAffinitySubstrate is implemented by CUDA/Metal backends (and test spies) for
// packed batch Hamming distance; the generic kernel.Substrate contract stays minimal.
type nearestAffinitySubstrate interface {
	NearestAffinity(query unsafe.Pointer, candidates unsafe.Pointer, count int) ([]uint32, error)
}

type pointerExecutingSubstrate interface {
	ExecutePointers(frames []unsafe.Pointer) error
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

// updateKernels is the post-ALU hook the backend invokes after every
// successful kernel dispatch. The substrate-side scoring of the result
// region happens here, and any new Values that fall out of that scoring
// (Associations from the README "Signals" algorithm) are pushed back
// onto the orchestrator's mesh.Field through the configured emitter.
//
// Keeping this on Backend (rather than the queue or the orchestrator)
// keeps the lookup off the hot Pop path: the post-hook runs on the same
// pool goroutine that just executed the kernel, so the work-item is
// already cache-warm and the sweep over signals[] is touching freshly
// written words.
func (b *Backend) updateKernels(result *primitive.Value) {
	if b == nil {
		return
	}

	runStructuralPostExec(result, b.emitter)
}
