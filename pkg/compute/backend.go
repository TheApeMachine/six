package compute

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/compute/kernel/metal"
	"github.com/theapemachine/six/pkg/compute/programmer"
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

All compute flows through Execute. The programmer compiles intent into
the Value's layout; the substrate reads the opcode and dispatches to
the appropriate kernel.
*/
type Backend struct {
	ctx    context.Context
	cancel context.CancelFunc
	states []*substrateState
	queue  *pool.Queue
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
Execute dispatches pre-compiled Value frames to the best available
substrate. The programmer must have already compiled the intent into
each Value's layout before calling this. The substrate reads the
opcode from the program region and dispatches internally.
*/
func (backend *Backend) Execute(
	frames []unsafe.Pointer,
) error {
	st := backend.pick()

	st.inflight.Add(1)
	start := time.Now()

	err := st.substrate.Execute(frames)

	st.inflight.Add(-1)
	st.observe(time.Since(start))

	return err
}

/*
targetFor maps a substrate's Name to the programmer.Target so the
compiler emits the correct layout just before execution.
*/
func targetFor(substrate kernel.Substrate) programmer.Target {
	switch substrate.Name() {
	case "metal":
		return programmer.Metal
	case "cuda":
		return programmer.CUDA
	default:
		return programmer.CPU
	}
}

/*
CompileAndExecute picks the lowest-pressure substrate, compiles the
program for that specific target, and executes the resulting frame.
This is the deferred-compilation path: callers build an uncompiled
Compiler and hand it off — the backend decides which hardware to
target at dispatch time.
*/
func (backend *Backend) CompileAndExecute(
	program any,
) error {
	compiler, ok := program.(*programmer.Compiler)

	if !ok {
		return errnie.Error(fmt.Errorf("CompileAndExecute: expected *programmer.Compiler"))
	}

	st := backend.pick()

	target := targetFor(st.substrate)
	value := compiler.Compile(target)

	st.inflight.Add(1)
	start := time.Now()

	err := st.substrate.Execute([]unsafe.Pointer{
		unsafe.Pointer(value),
	})

	st.inflight.Add(-1)
	st.observe(time.Since(start))

	return err
}

func (backend *Backend) Name() string {
	return "backend"
}
