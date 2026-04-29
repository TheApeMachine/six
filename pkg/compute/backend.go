package compute

import (
	"context"
	"errors"
	"iter"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/compute/kernel/metal"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Backend is a small load balancer over compute substrates (CUDA, Metal, CPU).
It picks the lowest-pressure candidate using inflight × EMA service time.
*/
type Backend struct {
	ctx        context.Context
	cancel     context.CancelFunc
	err        error
	pool       *pool.Pool
	substrates []*substrateState
	nextSub    atomic.Uint64
	pending    atomic.Int64
	community  sync.Map
	syncMu     sync.Mutex
}

/*
substrateState carries the live scheduling pressure for one substrate.
Service time is a nanosecond EMA so selection does not blindly round-robin a
tiny CPU job onto a cold accelerator with worse transfer/setup cost.
*/
type substrateState struct {
	kernel.Substrate
	inflight     atomic.Int64
	serviceNanos atomic.Int64
}

/*
NewBackend creates a Backend with every available substrate registered. CPU is
always added last so accelerators get first crack at low-pressure work.
*/
func NewBackend(ctx context.Context) *Backend {
	ctx, cancel := context.WithCancel(ctx)

	backend := &Backend{
		ctx:    ctx,
		cancel: cancel,
		pool:   pool.NewPool(uint64(runtime.NumCPU())),
	}

	for device := 0; device < cuda.Available(); device++ {
		backend.addSubstrate(cuda.NewBackend(device))
	}

	for device := 0; device < metal.Available(); device++ {
		backend.addSubstrate(metal.NewBackend(device))
	}

	backend.addSubstrate(cpu.NewBackend(ctx))

	return backend
}

/*
Submit registers value in the single community store. Status drives what
happens to it next: PENDING segments sit until a query tags them, READY
programs get picked up by Sync, RESOLVED values get yielded back out.
Submit itself does not touch status — every transition is owned by either
the kernel (in-frame writes) or the program (set ... <- STATUS).
*/
func (backend *Backend) Submit(owner *primitive.Value) error {
	if backend == nil || owner == nil {
		return errors.New("backend or owner is nil")
	}

	backend.community.Store(owner.ID(), owner)

	return nil
}

/*
Range visits the resident store without exposing its sync.Map machinery.
Tests and readout code use this to inspect in-band state while keeping
storage ownership inside Backend.
*/
func (backend *Backend) Range(visitor func(*primitive.Value) bool) {
	if backend == nil || visitor == nil {
		return
	}

	for _, resident := range backend.snapshotCommunity() {
		if !visitor(resident) {
			return
		}
	}
}

func (backend *Backend) getCommunity(owner *primitive.Value) []*primitive.Value {
	return backend.communityFor(owner, backend.snapshotCommunity())
}

func (backend *Backend) communityFor(owner *primitive.Value, residents []*primitive.Value) []*primitive.Value {
	if backend == nil || owner == nil {
		return nil
	}

	all := make([]*primitive.Value, 0, len(residents))
	community := make([]*primitive.Value, 0)
	lane := owner.ID()

	if reference, err := owner.Property(primitive.REFERENCE); err == nil && reference != 0 {
		lane = reference
	}

	for _, resident := range residents {
		reference, err := resident.Property(primitive.REFERENCE)
		if err != nil {
			continue
		}

		all = append(all, resident)

		if reference == lane && resident.Status() == primitive.SELECTED {
			community = append(community, resident)
		}
	}

	// Many resident programs intentionally run over the whole live store.
	// The SELECTED/reference lane is a narrowing optimization, not a
	// prerequisite for execution.
	if len(community) == 0 {
		return all
	}

	return community
}

/*
snapshotCommunity returns the live resident store in canonical ValueID order.
sync.Map intentionally has no stable iteration order, and the ALU pop/reducer
paths make first-seen ties observable. Sorting the snapshot keeps recruitment
and readout reproducible while preserving the lock-free submit store.
*/
func (backend *Backend) snapshotCommunity() []*primitive.Value {
	if backend == nil {
		return nil
	}

	residents := make([]*primitive.Value, 0)

	backend.community.Range(func(key, value any) bool {
		resident, ok := value.(*primitive.Value)
		if !ok || resident == nil {
			return true
		}

		residents = append(residents, resident)

		return true
	})

	sort.Slice(residents, func(left, right int) bool {
		return residents[left].ID() < residents[right].ID()
	})

	return residents
}

/*
execute walks substrates in pressure order and returns on the first success.
A failed substrate is bit-marked so the same submission does not retry it.
*/
func (backend *Backend) execute(
	owner *primitive.Value,
	community []*primitive.Value,
) ([]*primitive.Value, error) {
	if len(backend.substrates) == 0 {
		return nil, errors.New("no compute substrates available")
	}

	var last error
	var attempted uint64
	offset := int(backend.nextSub.Add(1) % uint64(len(backend.substrates)))

	for attempts := 0; attempts < len(backend.substrates); attempts++ {
		state, bit := backend.nextSubstrate(offset, attempted)
		if state == nil || bit == 0 {
			break
		}

		attempted |= bit

		results, err := backend.executeSubstrate(state, owner, community)

		if err == nil {
			return results, nil
		}

		last = err
	}

	if last != nil {
		return nil, last
	}

	return nil, errors.New("no compute substrates available")
}

/*
nextSubstrate finds the lowest-pressure substrate not yet attempted in this
submission. Ties use a rotating offset so cold substrates do not starve each
other, while the hot path stays allocation-free.
*/
func (backend *Backend) nextSubstrate(offset int, attempted uint64) (*substrateState, uint64) {
	if len(backend.substrates) == 0 {
		return nil, 0
	}

	bestIdx := -1
	bestRank := 0
	bestPressure := int64(1<<63 - 1)
	count := len(backend.substrates)

	for idx, state := range backend.substrates {
		if idx >= 64 {
			break
		}

		bit := uint64(1) << uint(idx)

		if attempted&bit != 0 {
			continue
		}

		if state == nil {
			continue
		}

		pressure := state.pressure()
		rank := (idx - offset + count) % count

		if bestIdx >= 0 && (pressure > bestPressure || pressure == bestPressure && rank >= bestRank) {
			continue
		}

		bestIdx = idx
		bestRank = rank
		bestPressure = pressure
	}

	if bestIdx < 0 {
		return nil, 0
	}

	return backend.substrates[bestIdx], uint64(1) << uint(bestIdx)
}

/*
SyncResult is the per-sweep readout from Backend.Sync. Resolved contains
Values that reached RESOLVED during the sweep. Ready contains every resident
still marked READY afterwards, including freshly spawned children and values
restored after a substrate failure, so drain loops can continue until the
runtime quiesces.
*/
type SyncResult struct {
	Resolved []*primitive.Value
	Ready    []*primitive.Value
}

/*
Sync walks the community store once: every READY value is dispatched on
the lowest-pressure substrate against its SELECTED lane, every RESOLVED
value is yielded to the caller. Spawned children land back in the same
store so the next tick picks them up — no second cache, no drain queue.
*/
func (backend *Backend) Sync(
	ctx context.Context,
) iter.Seq[*SyncResult] {
	if backend == nil {
		return nil
	}

	return func(yield func(*SyncResult) bool) {
		result := func() *SyncResult {
			backend.syncMu.Lock()
			defer backend.syncMu.Unlock()

			residents := backend.snapshotCommunity()

			for _, owner := range residents {
				if owner.Status() == primitive.READY {
					owner.SetStatus(primitive.BUSY)

					spawned, err := backend.execute(
						owner, backend.communityFor(owner, residents),
					)

					if err != nil {
						owner.SetStatus(primitive.READY)
						errnie.Error(err)
					}

					for _, child := range spawned {
						backend.community.Store(child.ID(), child)
					}

					if owner.Status() == primitive.BUSY {
						owner.SetSchedulingNext(0)
						owner.SetStatus(primitive.DONE)
					}
				}
			}

			result := &SyncResult{
				Resolved: make([]*primitive.Value, 0),
				Ready:    make([]*primitive.Value, 0),
			}

			for _, owner := range backend.snapshotCommunity() {
				if owner.Status() == primitive.RESOLVED {
					result.Resolved = append(result.Resolved, owner)
				}

				if owner.Status() == primitive.READY {
					result.Ready = append(result.Ready, owner)
				}
			}

			return result
		}()

		yield(result)
	}
}

func (backend *Backend) addSubstrate(substrate kernel.Substrate) *substrateState {
	if substrate == nil {
		return nil
	}

	state := &substrateState{Substrate: substrate}
	backend.substrates = append(backend.substrates, state)

	return state
}

func (backend *Backend) executeSubstrate(
	state *substrateState,
	owner *primitive.Value,
	community []*primitive.Value,
) ([]*primitive.Value, error) {
	if state == nil {
		return nil, nil
	}

	if state.Name() != "cpu" && valueUsesGeometric(owner) {
		return nil, errors.New("resident geometric slots require cpu substrate")
	}

	if state.Name() != "cpu" && valueTargetsChild(owner) {
		return nil, errors.New("child-target emit requires cpu substrate")
	}

	state.inflight.Add(1)
	defer state.inflight.Add(-1)
	start := time.Now()

	results, err := state.HypercubeGossip(owner, community)

	if err != nil {
		return nil, err
	}

	state.observe(time.Since(start))

	return results, nil
}

const (
	targetTagShift = 53
	targetTagMask  = 0x3
	targetTagChild = 0x2
)

/*
valueTargetsChild reports whether any packed program word writes to the
emitted child target. targetTagShift is the bit offset of the 2-bit target
tag, and targetTagChild is the tag value that denotes the child frame. The
backend uses this scan to keep child materialization on substrates that
currently implement target-C writes.
*/
func valueTargetsChild(value *primitive.Value) bool {
	if value == nil {
		return false
	}

	for _, word := range value.Get(primitive.ProgramRegion) {
		if word == 0 {
			continue
		}

		if (word>>targetTagShift)&targetTagMask == targetTagChild {
			return true
		}
	}

	return false
}

func valueUsesGeometric(value *primitive.Value) bool {
	if value == nil {
		return false
	}

	for _, word := range value.Get(primitive.ProgramRegion) {
		switch word {
		case 0x10, 0x20, 0x30:
			return true
		}
	}

	return false
}

func (state *substrateState) pressure() int64 {
	if state == nil {
		return 1<<63 - 1
	}

	serviceNanos := state.serviceNanos.Load()
	if serviceNanos < 1 {
		serviceNanos = 1
	}

	return (state.inflight.Load() + 1) * serviceNanos
}

func (state *substrateState) observe(elapsed time.Duration) {
	if state == nil {
		return
	}

	nanos := elapsed.Nanoseconds()
	if nanos < 1 {
		nanos = 1
	}

	for {
		previous := state.serviceNanos.Load()
		next := nanos

		if previous > 0 {
			next = ((previous * 7) + nanos) / 8
		}

		if state.serviceNanos.CompareAndSwap(previous, next) {
			return
		}
	}
}

/*
Close closes the Backend and all its substrates.
*/
func (backend *Backend) Close() error {
	errnie.Trace("compute.Backend.Close")

	if backend == nil {
		return nil
	}

	if backend.cancel != nil {
		backend.cancel()
	}

	if backend.pool != nil {
		_ = backend.pool.Close()
	}

	for _, sub := range backend.substrates {
		if err := sub.Close(); err != nil {
			errnie.Error(err)
		}
	}

	return nil
}

/*
Error implements the error interface.
*/
func (backend *Backend) Error() string {
	if backend == nil || backend.err == nil {
		return ""
	}

	return backend.err.Error()
}
