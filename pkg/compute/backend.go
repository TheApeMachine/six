package compute

import (
	"context"
	"errors"
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
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
)

/*
substrateState tracks per-substrate pressure so the load balancer can
make informed dispatch decisions without locks. All fields are atomically
updated from the dispatch hot path.
*/
type substrateState struct {
	idx       int
	substrate kernel.Substrate
	target    programmer.CompilerTarget
	inflight  atomic.Int64
	emaNanos  atomic.Int64
}

/*
Backend acts as an intelligent Multi-Substrate Load Balancer. It tracks
in-flight depth and exponential moving average service time per substrate,
dispatching work to whichever has the least pressure.

Frames may carry a residency tag (word 119). When present, pick adds
transferPenalty to scores for substrates that would pull the buffer
across a physical hop relative to where it last completed.

All compute flows through Execute. Values carry fixed-layout program words;
the substrate reads the opcode and dispatches to the appropriate kernel.
*/
type Backend struct {
	ctx             context.Context
	cancel          context.CancelFunc
	states          []*substrateState
	transferPenalty int64
	// exploreEvery, when non-zero, zeros the transfer penalty on every Nth
	// pick so the router empirically re-samples a faster substrate instead of
	// sticking on a CPU spill forever under static additive tolls.
	exploreEvery   uint64
	pickSeq        atomic.Uint64
	correlationSeq atomic.Uint64
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
WithExploreEvery re-schedules exploration hops: every n pick() calls, the
load balancer ignores residency and transferPenalty so a frame may migrate
off a temporarily cheap substrate even if a static toll would block it.
Set to 0 (default) to disable.
*/
func WithExploreEvery(n uint64) BackendOption {
	return func(backend *Backend) {
		backend.exploreEvery = n
	}
}

/*
NewBackend initializes the unified Load Balancer by probing for
all available compute substrates. Each substrate gets its own
pressure tracker so dispatch stays lock-free.
*/
func NewBackend(
	ctx context.Context,
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
			target:    programmer.CUDA,
		})

		idx++
	}

	for device := 0; device < metal.Available(); device++ {
		errnie.Info("compute.backend: Metal substrate registered")
		backend.states = append(backend.states, &substrateState{
			idx:       idx,
			substrate: metal.NewBackend(device),
			target:    programmer.Metal,
		})

		idx++
	}

	errnie.Info("compute.backend: CPU substrate registered")
	backend.states = append(backend.states, &substrateState{
		idx:       idx,
		substrate: cpu.NewBackend(backend.ctx),
		target:    programmer.CPU,
	})

	if err := validate.Require(map[string]any{
		"states": backend.states,
	}); err != nil {
		errnie.Error(err)
		return nil
	}

	return backend
}

func (backend *Backend) cpuBackend() *cpu.Backend {
	for _, st := range backend.states {
		if st.substrate.Name() != "cpu" {
			continue
		}

		cb, ok := st.substrate.(*cpu.Backend)
		if !ok {
			continue
		}

		return cb
	}

	return nil
}

func pointerSliceFromIndices(indices []uint32) []unsafe.Pointer {
	out := make([]unsafe.Pointer, 0, len(indices))

	for _, ix := range indices {
		value := primitive.ValueAt(ix)
		if value == nil {
			continue
		}

		out = append(out, unsafe.Pointer(&value[0]))
	}

	return out
}

/*
pick selects the substrate with the lowest pressure score.
Score = inflight * emaServiceTime plus an effective transfer toll when the
frame's residency tag disagrees with the candidate slot. The toll shrinks
when the resident substrate's EMA latency dominates the candidate's so a
one-time copy can amortize against faster hardware. Periodic exploration
drops the toll entirely every exploreEvery picks.
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

	explore := backend.nextPickExploresResidency()

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
			score += backend.effectiveTransferPenalty(residentIdx, st, explore)
		}

		if score < bestScore {
			bestScore = score
			best = st
		}
	}

	return best
}

/*
nextPickExploresResidency returns true on every exploreEvery-th scheduling
decision so the balancer can ignore migration cost for one pick.
*/
func (backend *Backend) nextPickExploresResidency() bool {
	if backend.exploreEvery == 0 {
		return false
	}

	n := backend.pickSeq.Add(1)

	return n%backend.exploreEvery == 0
}

/*
effectiveTransferPenalty maps the configured additive wall into a dynamic
toll. When the last resident substrate is much slower (higher EMA) than
the candidate, the penalty is right-shifted in proportion to the integer
latency ratio so asymmetric CUDA/CPU pairs can still justify a hop.
Exploration passes suppress the toll entirely.
*/
func (backend *Backend) effectiveTransferPenalty(
	residentIdx int,
	cand *substrateState,
	explore bool,
) int64 {
	if explore {
		return 0
	}

	penalty := backend.transferPenalty

	if penalty <= 0 || residentIdx < 0 || residentIdx >= len(backend.states) {
		return 0
	}

	resident := backend.states[residentIdx]

	if resident == nil || cand == nil {
		return penalty
	}

	resEMA := resident.emaNanos.Load()
	candEMA := cand.emaNanos.Load()

	if candEMA == 0 {
		candEMA = 1
	}

	if resEMA == 0 {
		resEMA = 1
	}

	if resEMA <= candEMA {
		return penalty
	}

	ratio := resEMA / candEMA

	for step := 0; step < 24 && ratio > 1 && penalty > 0; step++ {
		penalty >>= 1
		ratio >>= 1
	}

	return penalty
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

func (backend *Backend) ensureCorrelationIDs(frames []unsafe.Pointer) {
	for _, ptr := range frames {
		kernel.EnsureFrameCorrelationSeq(&backend.correlationSeq, ptr)
	}
}

func firstFrameOpcodeAndCorrelation(frames []unsafe.Pointer) (opcode uint8, correlation uint64, valueID uint64) {
	for _, ptr := range frames {
		if ptr == nil {
			continue
		}

		return kernel.FrameProgramRawOpcode(ptr), kernel.FrameCorrelationID(ptr), kernel.FrameID(ptr)
	}

	return 0, 0, 0
}

func (backend *Backend) publishALUDispatch(st *substrateState, frames []unsafe.Pointer, elapsed time.Duration) {
	if !telemetry.DefaultBus.IsActive() {
		return
	}

	op, corr, valID := firstFrameOpcodeAndCorrelation(frames)

	telemetry.DefaultBus.Publish(telemetry.ALUDispatchEvent(
		st.substrate.Name(),
		op,
		corr,
		elapsed.Nanoseconds(),
		valID,
	))
}

func (backend *Backend) publishWireFrames(frames []unsafe.Pointer) {
	const frameBytes = 128 * 8

	for _, ptr := range frames {
		if ptr == nil {
			continue
		}

		valueID := kernel.FrameID(ptr)
		if valueID == 0 {
			continue
		}

		telemetry.PublishWireValueFrame(
			valueID,
			unsafe.Slice((*byte)(ptr), frameBytes),
		)
	}
}

/*
Dispatch is the pool handler: it receives an Executable from a worker,
picks the best substrate, compiles for the matching target, writes the
frames into the Value, executes, and then calls the Finalizer so the
orchestrator can re-evaluate firmware or route to a community.
*/
func (backend *Backend) Dispatch(executable *programmer.Executable) {
	value := executable.Value()

	if value == nil {
		return
	}

	framePtr := unsafe.Pointer(&value[0])
	ptrs := []unsafe.Pointer{framePtr}

	indices, arenaErr := primitive.IndicesFromPointers(ptrs)

	if executable.IsResidentProgram() {
		if arenaErr != nil {
			cb := backend.cpuBackend()
			if cb == nil {
				errnie.Error(errors.New("compute.Dispatch: heap Value without CPU substrate"))
				executable.Finalize()

				return
			}

			if err := cb.ExecutePointers(ptrs); err != nil {
				errnie.Error(err)
			}

			executable.Finalize()

			return
		}

		if err := backend.Execute(indices); err != nil {
			errnie.Error(err)
		}

		executable.Finalize()
		return
	}

	frames, err := executable.Compile(programmer.CPU)

	if err != nil {
		errnie.Error(err)
		return
	}

	if len(frames) == 0 {
		executable.Finalize()
		return
	}

	st := backend.pick(ptrs)

	if st.target != programmer.CPU {
		recompiled, err := executable.Compile(st.target)

		if err == nil {
			frames = recompiled
		}
	}

	st.inflight.Add(1)

	if telemetry.DefaultBus.IsActive() {
		telemetry.DefaultBus.Publish(telemetry.PoolScheduleEvent(
			st.substrate.Name(),
			int(st.inflight.Load()),
			len(backend.states),
			value.ID(),
		))
	}

	start := time.Now()

	for idx := range frames {
		frames[idx].WriteIntoProgramRegion(value)
		executable.ApplyContinuation()

		var execErr error

		if arenaErr != nil {
			cb := backend.cpuBackend()
			if cb == nil {
				errnie.Error(errors.New("compute.Dispatch: heap Value without CPU substrate"))
				break
			}

			execErr = cb.ExecutePointers(ptrs)
		} else {
			execErr = st.substrate.Execute(indices)
		}

		if execErr != nil {
			errnie.Error(execErr)
			break
		}
	}

	elapsed := time.Since(start)
	st.inflight.Add(-1)
	st.observe(elapsed)

	if telemetry.DefaultBus.IsActive() {
		telemetry.DefaultBus.Publish(telemetry.PoolCompleteEvent(
			st.substrate.Name(),
			elapsed.Nanoseconds(),
			value.ID(),
		))
	}

	backend.publishALUDispatch(st, ptrs, elapsed)
	backend.publishWireFrames(ptrs)

	executable.Finalize()
}

/*
Execute dispatches pre-compiled arena slot indices to the best available
substrate. Each Value must already contain the program bits the ALU
expects; the substrate reads the opcode from the program region.
*/
func (backend *Backend) Execute(indices []uint32) error {
	ptrs := pointerSliceFromIndices(indices)
	backend.ensureCorrelationIDs(ptrs)

	// Force CPU substrate for OpcodeRegionProgram and OpcodeCopyMaskMerge since
	// they are only implemented on the CPU path today.
	op, _, valID := firstFrameOpcodeAndCorrelation(ptrs)
	var st *substrateState
	if uint64(op) == kernel.OpcodeRegionProgram || kernel.IsCopyMaskMergeOpcode(uint64(op)) {
		for _, state := range backend.states {
			if state.substrate.Name() == "cpu" {
				st = state
				break
			}
		}
	}

	if st == nil {
		st = backend.pick(ptrs)
	}

	st.inflight.Add(1)

	if telemetry.DefaultBus.IsActive() {
		telemetry.DefaultBus.Publish(telemetry.PoolScheduleEvent(
			st.substrate.Name(),
			int(st.inflight.Load()),
			len(backend.states),
			valID,
		))
	}

	start := time.Now()

	err := st.substrate.Execute(indices)

	elapsed := time.Since(start)
	st.inflight.Add(-1)
	st.observe(elapsed)

	if telemetry.DefaultBus.IsActive() {
		telemetry.DefaultBus.Publish(telemetry.PoolCompleteEvent(
			st.substrate.Name(),
			elapsed.Nanoseconds(),
			valID,
		))
	}

	backend.publishALUDispatch(st, ptrs, elapsed)
	backend.publishWireFrames(ptrs)

	return err
}

/*
ExecutePointers maps host pointers to arena indices when possible; heap Values
run on the CPU substrate only with the same telemetry and wire hooks as Execute.
*/
func (backend *Backend) ExecutePointers(frames []unsafe.Pointer) error {
	indices, err := primitive.IndicesFromPointers(frames)
	if err != nil {
		return backend.executeHeapFramesOnCPU(frames)
	}

	return backend.Execute(indices)
}

func (backend *Backend) executeHeapFramesOnCPU(frames []unsafe.Pointer) error {
	cb := backend.cpuBackend()
	if cb == nil {
		return errors.New("compute.Backend.ExecutePointers: no CPU substrate for heap frame")
	}

	backend.ensureCorrelationIDs(frames)

	_, _, valID := firstFrameOpcodeAndCorrelation(frames)

	var st *substrateState

	for _, state := range backend.states {
		if state.substrate.Name() == "cpu" {
			st = state

			break
		}
	}

	if st == nil {
		return errors.New("compute.Backend.executeHeapFramesOnCPU: CPU substrate missing")
	}

	st.inflight.Add(1)

	if telemetry.DefaultBus.IsActive() {
		telemetry.DefaultBus.Publish(telemetry.PoolScheduleEvent(
			st.substrate.Name(),
			int(st.inflight.Load()),
			len(backend.states),
			valID,
		))
	}

	start := time.Now()

	execErr := cb.ExecutePointers(frames)

	elapsed := time.Since(start)
	st.inflight.Add(-1)
	st.observe(elapsed)

	if telemetry.DefaultBus.IsActive() {
		telemetry.DefaultBus.Publish(telemetry.PoolCompleteEvent(
			st.substrate.Name(),
			elapsed.Nanoseconds(),
			valID,
		))
	}

	backend.publishALUDispatch(st, frames, elapsed)
	backend.publishWireFrames(frames)

	return execErr
}

func (backend *Backend) Name() string {
	return "backend"
}
