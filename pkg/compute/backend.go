package compute

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/compute/kernel/metal"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
substrateState tracks per-substrate pressure so the load balancer can
make informed dispatch decisions without locks.
*/
type substrateState struct {
	idx       int
	substrate interface {
		Execute(indices []uint32) error
		Name() string
	}
	inflight atomic.Int64
	emaNanos atomic.Int64
}

// frameMetaResidencyWord is the asset metadata slot that records which substrate
// last executed the frame (see pkg/core/config.go ValueRegionConfig asset band).
// Zero means unset; otherwise it holds the substrateState.idx value.
const frameMetaResidencyWord = 119

/*
Backend is a small load balancer over compute substrates (CUDA, Metal, CPU).
It picks the lowest-pressure candidate using inflight × EMA service time, with
an optional migration toll when a frame carries a residency tag pointing at
another substrate.
*/
type Backend struct {
	ctx             context.Context
	cancel          context.CancelFunc
	err             error
	states          []*substrateState
	transferPenalty int64
	exploreEvery    uint64
	pickSeq         atomic.Uint64
}

/*
BackendOption configures the multi-substrate router.
*/
type BackendOption func(*Backend)

/*
WithTransferPenalty sets the additive cost (same units as inflight×ema) for
running a frame on a substrate other than its residency tag.
*/
func WithTransferPenalty(nanosProduct int64) BackendOption {
	return func(backend *Backend) {
		backend.transferPenalty = nanosProduct
	}
}

/*
WithExploreEvery, when non-zero, clears the migration toll every n-th pick so
the router can re-sample a faster substrate.
*/
func WithExploreEvery(n uint64) BackendOption {
	return func(backend *Backend) {
		backend.exploreEvery = n
	}
}

/*
NewBackend registers every available substrate (CUDA devices, Metal devices,
then CPU) with independent pressure trackers.
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

func (backend *Backend) Close() error {
	if backend == nil {
		return nil
	}

	return backend.err
}

func (backend *Backend) Error() error {
	return backend.err
}

/*
Inflight sums substrate inflight counters so callers can wait for compute
to finish without adding locks to the dispatch path.
*/
func (backend *Backend) Inflight() int64 {
	if backend == nil {
		return 0
	}

	var total int64

	for _, st := range backend.states {
		if st == nil {
			continue
		}

		total += st.inflight.Load()
	}

	return total
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

func pointerSliceFromIndices(indices []uint32) (out []unsafe.Pointer, err error) {
	out = make([]unsafe.Pointer, 0, len(indices))

	for _, ix := range indices {
		value := primitive.ValueAt(ix)
		if value == nil {
			return nil, fmt.Errorf(
				"compute.pointerSliceFromIndices: primitive.ValueAt(%d) returned nil (invalid index or arena)",
				ix,
			)
		}

		out = append(out, unsafe.Pointer(&value[0]))
	}

	return out, nil
}

func residencySubstrateIndex(ptr unsafe.Pointer) int {
	if ptr == nil {
		return -1
	}

	w := (*[128]uint64)(ptr)[frameMetaResidencyWord]
	if w == 0 {
		return -1
	}

	return int(w)
}

/*
pick selects the substrate with the lowest pressure score.
Score = inflight * emaServiceTime plus an effective transfer toll when the
frame's residency tag disagrees with the candidate slot.
*/
func (backend *Backend) pick(frames []unsafe.Pointer) *substrateState {
	residentIdx := -1

	for _, ptr := range frames {
		if ptr == nil {
			continue
		}

		if idx := residencySubstrateIndex(ptr); idx >= 0 {
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

func (backend *Backend) nextPickExploresResidency() bool {
	if backend.exploreEvery == 0 {
		return false
	}

	n := backend.pickSeq.Add(1)

	return n%backend.exploreEvery == 0
}

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

func (st *substrateState) observe(elapsed time.Duration) {
	nanos := elapsed.Nanoseconds()
	old := st.emaNanos.Load()

	if old == 0 {
		st.emaNanos.Store(nanos)

		return
	}

	st.emaNanos.Store(old + (nanos-old)>>3)
}

func (backend *Backend) dispatchExecute(value *primitive.Value) {
	if value == nil {
		return
	}

	framePtr := unsafe.Pointer(&value[0])
	ptrs := []unsafe.Pointer{framePtr}

	if err := backend.ExecutePointers(ptrs); err != nil {
		errnie.Error(err)
	}
}

/*
Dispatch runs one pass on the Value's current program words (arena or heap).
*/
func (backend *Backend) Dispatch(value *primitive.Value) {
	if value == nil {
		return
	}

	backend.dispatchExecute(value)
}

/*
Execute runs arena-backed slot indices on whichever substrate pick selects.
All substrates implement the same Execute(indices) contract.
*/
func (backend *Backend) Execute(indices []uint32) error {
	ptrs, err := pointerSliceFromIndices(indices)
	if err != nil {
		return err
	}

	st := backend.pick(ptrs)
	if st == nil {
		return errors.New("compute.Backend.Execute: no substrate")
	}

	st.inflight.Add(1)
	start := time.Now()

	execErr := st.substrate.Execute(indices)

	elapsed := time.Since(start)
	st.inflight.Add(-1)
	st.observe(elapsed)

	return execErr
}

/*
ExecutePointers uses arena indices when the pointers lie in the arena slab
(so GPU/Metal can run); otherwise it runs on the CPU substrate (only path
for heap-allocated frames).
*/
func (backend *Backend) ExecutePointers(frames []unsafe.Pointer) error {
	indices, err := primitive.IndicesFromPointers(frames)
	if err != nil {
		return backend.executeNonArenaOnCPU(frames)
	}

	return backend.Execute(indices)
}

func (backend *Backend) executeNonArenaOnCPU(frames []unsafe.Pointer) error {
	cb := backend.cpuBackend()
	if cb == nil {
		return errors.New("compute.Backend: no CPU substrate for non-arena frames")
	}

	var st *substrateState

	for _, state := range backend.states {
		if state.substrate.Name() == "cpu" {
			st = state

			break
		}
	}

	if st == nil {
		return errors.New("compute.Backend: CPU substrate missing")
	}

	st.inflight.Add(1)
	start := time.Now()

	execErr := cb.ExecutePointers(frames)

	elapsed := time.Since(start)
	st.inflight.Add(-1)
	st.observe(elapsed)

	return execErr
}

func (backend *Backend) Name() string {
	return "backend"
}
