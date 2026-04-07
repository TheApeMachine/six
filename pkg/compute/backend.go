package compute

import (
	"context"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/compute/kernel/metal"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/pool"
)

/*
substrateState tracks per-substrate pressure so the load balancer can
make informed dispatch decisions without locks. All fields are atomically
updated from the dispatch hot path.
*/
type substrateState struct {
	substrate kernel.Substrate
	inflight  atomic.Int64
	emaNanos  atomic.Int64
}

/*
Backend acts as an intelligent Multi-Substrate Load Balancer. It tracks
in-flight depth and exponential moving average service time per substrate,
dispatching work to whichever has the least pressure.

Both GPU and CPU substrates stay saturated because the selection is
per-call: a burst of requests fans out across all available hardware
rather than queueing behind whichever substrate was "first".
*/
type Backend struct {
	ctx      context.Context
	cancel   context.CancelFunc
	states   []*substrateState
	queue    *pool.Queue
}

/*
BackendOption configures the multi-substrate router.
*/
type BackendOption func(*Backend)

/*
NewBackend initializes the unified Load Balancer by probing for
all available compute substrates. Each substrate gets its own
pressure tracker so dispatch stays lock-free.
*/
func NewBackend(
	ctx context.Context,
	queue *pool.Queue,
	opts ...BackendOption,
) *Backend {
	ctx, cancel := context.WithCancel(ctx)

	backend := &Backend{
		ctx:    ctx,
		cancel: cancel,
		states: make([]*substrateState, 0),
	}

	for _, opt := range opts {
		opt(backend)
	}

	if err := validate.Require(map[string]any{
		"ctx":    backend.ctx,
		"cancel": backend.cancel,
	}); err != nil {
		errnie.Error(err)
		return nil
	}

	for idx := 0; idx < cuda.Available(); idx++ {
		errnie.Info("compute.backend: CUDA substrate registered")
		backend.states = append(backend.states, &substrateState{
			substrate: cuda.NewBackend(idx),
		})
	}

	for idx := 0; idx < metal.Available(); idx++ {
		errnie.Info("compute.backend: Metal substrate registered")
		backend.states = append(backend.states, &substrateState{
			substrate: metal.NewBackend(idx),
		})
	}

	errnie.Info("compute.backend: CPU substrate registered")
	backend.states = append(backend.states, &substrateState{
		substrate: cpu.NewBackend(backend.ctx),
	})

	if err := validate.Require(map[string]any{
		"states": backend.states,
	}); err != nil {
		errnie.Error(err)
		return nil
	}

	return backend
}

/*
pick selects the substrate with the lowest pressure score.
Score = inflight * emaServiceTime. This naturally favors idle substrates
and fast substrates equally — a GPU with 2 in-flight but 100ns EMA
scores the same as a CPU with 1 in-flight and 200ns EMA.
*/
func (backend *Backend) pick() *substrateState {
	var best *substrateState
	bestScore := int64(^uint64(0) >> 1)

	for _, st := range backend.states {
		inflight := st.inflight.Load()
		ema := st.emaNanos.Load()

		if ema == 0 {
			ema = 1
		}

		score := inflight * ema

		if score < bestScore {
			bestScore = score
			best = st
		}
	}

	return best
}

/*
observe updates the EMA service time after a dispatch completes.
Uses a simple α=1/8 exponential moving average (shift instead of
multiply for zero-alloc hot path).
*/
func (st *substrateState) observe(elapsed time.Duration) {
	nanos := elapsed.Nanoseconds()
	old := st.emaNanos.Load()

	if old == 0 {
		st.emaNanos.Store(nanos)
		return
	}

	st.emaNanos.Store(old + (nanos-old)>>3)
}

/*
BatchDistances computes Hamming distances from a query affinity vector
to a contiguous array of candidate vectors. Selects the substrate with
lowest pressure (inflight × EMA latency), tracks timing, and updates
the EMA so subsequent calls rebalance automatically.
*/
func (backend *Backend) BatchDistances(
	query unsafe.Pointer,
	candidates unsafe.Pointer,
	count int,
	distances []uint32,
) error {
	st := backend.pick()

	st.inflight.Add(1)
	start := time.Now()

	err := st.substrate.BatchDistances(query, candidates, count, distances)

	st.inflight.Add(-1)
	st.observe(time.Since(start))

	return err
}

func (backend *Backend) Name() string {
	return "backend"
}
