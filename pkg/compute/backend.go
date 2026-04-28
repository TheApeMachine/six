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
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
)

type QueueType uint

const (
	QueueTypeNormal QueueType = iota
	QueueTypePriority
)

const ttlExpiredSentinel = uint64(1) << 63

func TTLExpiredSentinel() uint64 {
	return ttlExpiredSentinel
}

/*
Backend is a small load balancer over compute substrates (CUDA, Metal, CPU).
It picks the lowest-pressure candidate using inflight × EMA service time.
*/
type Backend struct {
	ctx          context.Context
	cancel       context.CancelFunc
	err          error
	queues       map[QueueType]*Queue
	pool         *pool.Pool
	substrates   []*substrateState
	cpuSubstrate *substrateState
	popped       atomic.Int64
	nextSub      atomic.Uint64
	pending      atomic.Int64
	cache        sync.Map
	staging      sync.Map
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
NewBackend creates a new Backend instance.
*/
func NewBackend(ctx context.Context) *Backend {
	ctx, cancel := context.WithCancel(ctx)

	backend := &Backend{
		ctx:        ctx,
		cancel:     cancel,
		err:        nil,
		queues:     make(map[QueueType]*Queue),
		pool:       pool.NewPool(uint64(runtime.NumCPU())),
		substrates: make([]*substrateState, 0),
		cache:      sync.Map{},
		staging:    sync.Map{},
	}

	for device := 0; device < cuda.Available(); device++ {
		backend.addSubstrate(cuda.NewBackend(device))
	}

	for device := 0; device < metal.Available(); device++ {
		backend.addSubstrate(metal.NewBackend(device))
	}

	backend.cpuSubstrate = backend.addSubstrate(cpu.NewBackend(ctx))

	if !createAndAssignQueue(ctx, backend, QueueTypeNormal) {
		return nil
	}

	if !createAndAssignQueue(ctx, backend, QueueTypePriority) {
		return nil
	}

	return backend
}

/*
createAndAssignQueue builds one scheduler ring and binds it to backend.queues.
Centralizes the shared NewQueue + errnie.Error path used for normal and priority lanes.
*/
func createAndAssignQueue(ctx context.Context, backend *Backend, queueType QueueType) bool {
	queue, err := NewQueue(ctx)

	if err != nil {
		errnie.Error(err)

		return false
	}

	backend.queues[queueType] = queue

	return true
}

/*
Schedule a new job to be executed on the backend.
*/
func (backend *Backend) Schedule(job func()) {
	backend.pool.Submit(job)
}

/*
Submit schedules in-value execution over a community.

The community values are parked under the owner's ValueID inside backend.staging
as the B-pool the kernel sweep will pop from. Programs are linear sweeps over
the program region (select A, select B, apply truth table, write to DST); the
only way to branch or loop is for the value to set the spawn/reschedule word
and fall back into the ALU on the priority queue for another sweep.
*/
func (backend *Backend) Submit(owner *primitive.Value, community []*primitive.Value) (err error) {
	if backend == nil || len(community) == 0 {
		return errors.New("backend is nil or community is empty")
	}

	if owner == nil {
		return errors.New("owner is nil")
	}

	if owner.Status() != primitive.READY {
		return errors.New("owner is not ready")
	}

	owner.SetStatus(primitive.WAITING)

	backend.stage(owner, community)
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

		spawned = backend.stageResidual(owner, spawned, community)
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
stageResidual hands recruiter continuations the still-unclaimed input lane.
The kernel already wrote every accepted candidate's community word in-band; the
finalizer only preserves the remaining B frames as the next recruiter's workset.
If the recruiter did not saturate enough to emit on its own, residual work still
needs a fresh seed, so the finalizer mints one carrying the same firmware.
*/
func (backend *Backend) stageResidual(owner *primitive.Value, spawned, community []*primitive.Value) []*primitive.Value {
	if backend == nil || len(community) == 0 {
		return spawned
	}

	var residual []*primitive.Value
	for _, value := range community {
		if value == nil {
			continue
		}

		communityID, err := value.Property(primitive.COMMUNITY)
		if err != nil || communityID != 0 {
			continue
		}

		residual = append(residual, value)
	}

	if len(residual) == 0 {
		return spawned
	}

	if len(residual) == len(community) {
		return spawned
	}

	staged := false
	for _, child := range spawned {
		if child == nil || !child.ReadyForALU() {
			continue
		}

		backend.stage(child, residual)
		staged = true
	}

	if staged || !isFirmware(owner, core.RECRUIT_COMMUNITY) {
		return spawned
	}

	child := primitive.Emit(primitive.WithFirmware(core.RECRUIT_COMMUNITY))
	if child == nil || !child.ReadyForALU() {
		return spawned
	}

	backend.stage(child, residual)

	return append(spawned, child)
}

func isFirmware(value *primitive.Value, firmware core.FirmwareType) bool {
	if value == nil || core.Cfg == nil {
		return false
	}

	entry, ok := core.Cfg.Programs[firmware]
	if !ok {
		return false
	}

	words := value.Get(primitive.ProgramRegion)
	compiled := entry.Compiled()
	if len(words) == 0 || len(compiled) == 0 {
		return false
	}

	for idx, word := range compiled {
		if idx >= len(words) {
			return false
		}

		if words[idx] != word {
			return false
		}
	}

	return true
}

func (backend *Backend) execute(
	owner *primitive.Value,
	community []*primitive.Value,
) ([]*primitive.Value, error) {
	if backend == nil || len(backend.substrates) == 0 {
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
	if backend == nil || len(backend.substrates) == 0 {
		return nil, 0
	}

	bestIdx := -1
	bestRank := 0
	bestPressure := int64(1<<63 - 1)
	n := len(backend.substrates)

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
		rank := (idx - offset + n) % n

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
stagingLane is the per-owner B-pool that programs pop from during a sweep.
PopB advances the read cursor with a CAS so kernels share the pool without
a mutex on the consume side. The producer side (StageInto) protects append
with a small mutex because Go slice growth is not safe under concurrent
appends; lanes are partitioned by owner ID so contention is per-owner, not
global, and the only contenders are sweeps that explicitly target the same
owner — rare in practice.
*/
type stagingLane struct {
	mu     sync.Mutex
	values []*primitive.Value
	cursor atomic.Uint64
}

/*
stage parks the community under the owner's ValueID. Re-staging the same owner
overwrites the prior lane (a fresh sweep starts from a fresh cursor).
*/
func (backend *Backend) stage(owner *primitive.Value, community []*primitive.Value) {
	if backend == nil || owner == nil {
		return
	}

	lane := &stagingLane{values: community}
	backend.staging.Store(owner.ID(), lane)
}

/*
Lane returns a snapshot of the staging slice for the given owner. The host
scheduler uses this to decide whether a Value has work to do: empty lane,
nothing to dispatch. Programs are the only thing that fills lanes (via the
kernel's stage instruction), so the host stays out of selection logic.
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
go where, the program does. Contention is per-owner; the per-lane mutex only
serializes the rare case where two sweeps target the same owner at once.
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

/*
PopB returns the next community Value parked under the owner's ValueID, or nil
when the lane is exhausted. This is the host-side counterpart to the kernel's
pop(B) topology — every program is a linear sweep that consumes its B span by
calling here.
*/
func (backend *Backend) PopB(owner *primitive.Value) *primitive.Value {
	if backend == nil || owner == nil {
		return nil
	}

	entry, ok := backend.staging.Load(owner.ID())
	if !ok {
		return nil
	}

	lane := entry.(*stagingLane)
	idx := lane.cursor.Add(1) - 1

	if idx >= uint64(len(lane.values)) {
		return nil
	}

	return lane.values[idx]
}

/*
clearStaging drops the owner's lane. Called when a submission fails before the
sweep runs and after the sweep retires so the map does not retain dead frames.
*/
func (backend *Backend) clearStaging(owner *primitive.Value) {
	if backend == nil || owner == nil {
		return
	}

	backend.staging.Delete(owner.ID())
}

/*
finishOwner enforces single-use firmware. A program may keep itself alive only
by explicitly restoring READY and leaving a continuation word; every other pass
settles to DONE with an empty program slab.
*/
func (backend *Backend) finishOwner(owner *primitive.Value) {
	if owner == nil {
		return
	}

	if owner.Status() == primitive.READY && owner.SchedulingNext() != 0 && owner.HasProgram() {
		return
	}

	owner.ClearProgram()
	owner.SetSchedulingNext(0)

	switch owner.Status() {
	case primitive.PENDING, primitive.READY, primitive.BUSY, primitive.WAITING:
		owner.SetStatus(primitive.DONE)
	}
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

	return spawned, err
}

func (backend *Backend) failOwner(owner *primitive.Value) {
	if owner == nil {
		return
	}

	owner.ClearProgram()
	owner.SetSchedulingNext(0)
	owner.SetStatus(primitive.ERROR)
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

	for _, queue := range backend.queues {
		if queue != nil {
			_ = queue.Close()
		}
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
