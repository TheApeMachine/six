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
	"github.com/theapemachine/six/pkg/viz"
)

/*
substrateState tracks per-substrate pressure so the load balancer can
make informed dispatch decisions without locks. All fields are atomically
updated from the dispatch hot path.
*/
type substrateState struct {
	idx       int
	substrate kernel.Substrate
	inflight  atomic.Int64
	emaNanos  atomic.Int64
}

/*
Backend acts as an intelligent Multi-Substrate Load Balancer. It tracks
in-flight depth and exponential moving average service time per substrate,
dispatching work to whichever has the least pressure.

Frames may carry a residency tag (word 121). When present, pick adds
transferPenalty to scores for substrates that would pull the buffer
across a physical hop relative to where it last completed.

All compute flows through Execute. The programmer compiles intent into
the Value's layout; the substrate reads the opcode and dispatches to
the appropriate kernel.
*/
type Backend struct {
	ctx             context.Context
	cancel          context.CancelFunc
	states          []*substrateState
	queue           *pool.Queue
	transferPenalty int64
	correlationSeq  atomic.Uint64
}

/*
BackendOption configures the multi-substrate router.
*/
type BackendOption func(*Backend)

/*
WithTransferPenalty sets the additive pressure cost (same units as
inflight×ema) for moving a frame off its last executing substrate.
*/
func WithTransferPenalty(nanosProduct int64) BackendOption {
	return func(backend *Backend) {
		backend.transferPenalty = nanosProduct
	}
}

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
		ctx:             ctx,
		cancel:          cancel,
		states:          make([]*substrateState, 0),
		transferPenalty: 1 << 20,
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

	idx := 0

	for device := 0; device < cuda.Available(); device++ {
		errnie.Info("compute.backend: CUDA substrate registered")
		backend.states = append(backend.states, &substrateState{
			idx:       idx,
			substrate: cuda.NewBackend(device),
		})

		idx++
	}

	for device := 0; device < metal.Available(); device++ {
		errnie.Info("compute.backend: Metal substrate registered")
		backend.states = append(backend.states, &substrateState{
			idx:       idx,
			substrate: metal.NewBackend(device),
		})

		idx++
	}

	errnie.Info("compute.backend: CPU substrate registered")
	backend.states = append(backend.states, &substrateState{
		idx:       idx,
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
Score = inflight * emaServiceTime plus transferPenalty when the frame's
residency tag disagrees with the candidate slot.
*/
func (backend *Backend) pick(frames []unsafe.Pointer) *substrateState {
	residentIdx := -1

	for _, ptr := range frames {
		if ptr == nil {
			continue
		}

		if idx := kernel.ResidencySubstrateIndex(ptr); idx >= 0 {
			residentIdx = idx

			break
		}
	}

	var best *substrateState
	bestScore := int64(^uint64(0) >> 1)

	for _, st := range backend.states {
		inflight := st.inflight.Load()
		ema := st.emaNanos.Load()

		if ema == 0 {
			ema = 1
		}

		score := inflight * ema

		if residentIdx >= 0 && residentIdx != st.idx && backend.transferPenalty > 0 {
			score += backend.transferPenalty
		}

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

func (backend *Backend) stampResidency(frames []unsafe.Pointer, st *substrateState) {
	if st == nil {
		return
	}

	for _, ptr := range frames {
		kernel.StampFrameResidency(ptr, st.idx)
	}
}

func (backend *Backend) ensureCorrelationIDs(frames []unsafe.Pointer) {
	for _, ptr := range frames {
		kernel.EnsureFrameCorrelationSeq(&backend.correlationSeq, ptr)
	}
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
	backend.ensureCorrelationIDs(frames)

	st := backend.pick(frames)

	st.inflight.Add(1)

	viz.DefaultBus.Publish(viz.PoolScheduleEvent(
		st.substrate.Name(),
		int(st.inflight.Load()),
		len(backend.states),
	))

	start := time.Now()

	err := st.substrate.Execute(frames)

	elapsed := time.Since(start)
	st.inflight.Add(-1)
	st.observe(elapsed)

	viz.DefaultBus.Publish(viz.PoolCompleteEvent(
		st.substrate.Name(),
		int(elapsed.Milliseconds()),
	))

	if err == nil {
		backend.stampResidency(frames, st)
	}

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

	framePtr := unsafe.Pointer(compiler.Frame())

	backend.ensureCorrelationIDs([]unsafe.Pointer{framePtr})

	st := backend.pick([]unsafe.Pointer{framePtr})

	target := targetFor(st.substrate)
	value := compiler.Compile(target)

	st.inflight.Add(1)

	viz.DefaultBus.Publish(viz.PoolScheduleEvent(
		st.substrate.Name(),
		int(st.inflight.Load()),
		len(backend.states),
	))

	start := time.Now()

	err := st.substrate.Execute([]unsafe.Pointer{
		unsafe.Pointer(value),
	})

	elapsed := time.Since(start)
	st.inflight.Add(-1)
	st.observe(elapsed)

	viz.DefaultBus.Publish(viz.PoolCompleteEvent(
		st.substrate.Name(),
		int(elapsed.Milliseconds()),
	))

	if err == nil {
		backend.stampResidency([]unsafe.Pointer{unsafe.Pointer(value)}, st)
	}

	return err
}

func (backend *Backend) Name() string {
	return "backend"
}
