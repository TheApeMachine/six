package compute

import (
	"context"
	"errors"
	"iter"
	"runtime"
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
	cache      sync.Map
	staging    sync.Map
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
Submit runs owner's program over its staged community on the lowest-pressure
substrate. Spawned children land in the cache for Sync to drain; in-band stage
requests emitted by the kernel are dispatched into the matching owner's lane
via StageInto. The owner is single-use unless its program rewrote itself READY
with a non-zero continuation before the sweep returned.
*/
func (backend *Backend) Submit(owner *primitive.Value) error {
	if backend == nil || owner == nil {
		return errors.New("backend or owner is nil")
	}

	if owner.Status() != primitive.READY {
		return errors.New("owner is not ready")
	}

	community := backend.Lane(owner)
	
	if len(community) == 0 {
		return errors.New("owner has no staged community")
	}

	owner.SetStatus(primitive.WAITING)
	backend.pending.Add(1)

	backend.pool.Submit(func() {
		defer backend.pending.Add(-1)
		defer backend.clearStaging(owner)

		spawned, err := backend.execute(owner, community)
		if err != nil {
			errnie.Error(err)
			backend.failOwner(owner)

			return
		}

		backend.finishOwner(owner)

		for _, child := range spawned {
			if child != nil {
				backend.cache.Store(child.ID(), child)
			}
		}
	})

	return nil
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

		spawned, err := backend.executeSubstrate(state, owner, community)
		if err == nil {
			return spawned, nil
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
stagingLane is the per-owner B-pool that programs sweep over. Append is mutex-
protected because Go slice growth is not safe under concurrent appends; lanes
are partitioned by owner ID so contention is per-owner, not global.
*/
type stagingLane struct {
	mu     sync.Mutex
	values []*primitive.Value
}

/*
Lane returns a snapshot of the staging slice for the given owner. Cycle uses
this to fetch the community a READY owner should sweep over; tests use it for
inspection.
*/
func (backend *Backend) Lane(owner *primitive.Value) []*primitive.Value {
	if backend == nil || owner == nil {
		return nil
	}

	entry, ok := backend.staging.Load(owner.ID())
	if !ok {
		return nil
	}

	lane := entry.(*stagingLane)

	lane.mu.Lock()
	out := make([]*primitive.Value, len(lane.values))
	copy(out, lane.values)
	lane.mu.Unlock()

	return out
}

/*
StageInto pushes a Value into the staging lane keyed by ownerID. The kernel's
stage instruction calls this from inside a program sweep, so reference-style
selection happens entirely inside Value-space — no Go side decides which Bs
go where, the program does.
*/
func (backend *Backend) StageInto(ownerID uint64, value *primitive.Value) {
	if backend == nil || value == nil {
		return
	}

	entry, _ := backend.staging.LoadOrStore(ownerID, &stagingLane{})
	lane := entry.(*stagingLane)

	lane.mu.Lock()
	lane.values = append(lane.values, value)
	lane.mu.Unlock()
}

func (backend *Backend) clearStaging(owner *primitive.Value) {
	if owner == nil {
		return
	}

	backend.staging.Delete(owner.ID())
}

/*
finishOwner enforces single-use firmware with two opt-in escape hatches.

  - Self-continuation (status=READY, continuation=self.id, program intact):
    the firmware wants another sweep on this same owner; leave everything.
  - Wake-target continuation (status=DONE, continuation=other_id != 0):
    the firmware is finished here but is signalling that another value
    should run next. Drop the program slab so this owner does not get
    scheduled again, but PRESERVE the continuation word so the runtime
    can read it as a wake target and flip the matching WAITING value to
    READY (machine.wakeWaiting).

Anything else falls through to the standard retire: clear program, clear
continuation, force DONE.
*/
func (backend *Backend) finishOwner(owner *primitive.Value) {
	if owner == nil {
		return
	}

	if owner.Status() == primitive.READY && owner.SchedulingNext() != 0 && owner.HasProgram() {
		return
	}

	if owner.Status() == primitive.DONE && owner.SchedulingNext() != 0 && owner.SchedulingNext() != owner.ID() {
		owner.ClearProgram()

		return
	}

	owner.ClearProgram()
	owner.SetSchedulingNext(0)

	switch owner.Status() {
	case primitive.PENDING, primitive.READY, primitive.BUSY, primitive.WAITING:
		owner.SetStatus(primitive.DONE)
	}
}

func (backend *Backend) failOwner(owner *primitive.Value) {
	if owner == nil {
		return
	}

	owner.ClearProgram()
	owner.SetSchedulingNext(0)
	owner.SetStatus(primitive.ERROR)
}

/*
Sync waits for queued compute work to quiesce and then drains emitted Values.
The normal cycle path still submits asynchronously; prompt/readout callers use
this when they need an observed tick before deciding whether anything resolved.
*/
func (backend *Backend) Sync(ctx context.Context) iter.Seq[*primitive.Value] {
	if backend == nil {
		return nil
	}

	return func(yield func(*primitive.Value) bool) {
		for backend.pending.Load() > 0 {
			select {
			case <-ctx.Done():
				return
			default:
				runtime.Gosched()
			}
		}

		backend.cache.Range(func(key any, value any) bool {
			if value.(*primitive.Value).Status() != primitive.DONE {
				if !yield(value.(*primitive.Value)) {
					return false
				}

				backend.cache.Delete(key)
			}

			return true
		})
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

	state.inflight.Add(1)
	start := time.Now()

	spawned, staged, err := state.HypercubeGossip(owner, community)

	state.inflight.Add(-1)
	state.observe(time.Since(start))

	if err != nil {
		return spawned, err
	}

	for _, req := range staged {
		backend.StageInto(req.OwnerID, req.Value)
	}

	return spawned, nil
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
