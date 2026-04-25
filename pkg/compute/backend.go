package compute

import (
	"context"
	"runtime"
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

type QueueType uint

const (
	QueueTypeNormal QueueType = iota
	QueueTypePriority
)

const ttlExpiredSentinel = uint64(1) << 63

/*
Backend is a small load balancer over compute substrates (CUDA, Metal, CPU).
It picks the lowest-pressure candidate using inflight × EMA service time.
*/
type Backend struct {
	ctx        context.Context
	cancel     context.CancelFunc
	err        error
	queues     map[QueueType]*Queue
	pool       *pool.Pool
	substrates []kernel.Substrate
	popped     atomic.Int64
	nextSub    atomic.Uint64
	pending    atomic.Int64
	completed  chan []*primitive.Value
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
		substrates: make([]kernel.Substrate, 0),
		completed:  make(chan []*primitive.Value, runtime.NumCPU()*4),
	}

	for device := 0; device < cuda.Available(); device++ {
		backend.substrates = append(backend.substrates, cuda.NewBackend(device))
	}

	for device := 0; device < metal.Available(); device++ {
		backend.substrates = append(backend.substrates, metal.NewBackend(device))
	}

	backend.substrates = append(backend.substrates, cpu.NewBackend(ctx))
	backend.queues[QueueTypeNormal], _ = NewQueue(ctx)
	backend.queues[QueueTypePriority], _ = NewQueue(ctx)
	go backend.dispatch(backend.queues[QueueTypePriority])
	go backend.dispatch(backend.queues[QueueTypeNormal])

	return backend
}

/*
Schedule a new job to be executed on the backend.
*/
func (backend *Backend) Schedule(job func()) {
	backend.pool.Submit(job)
}

/*
Submit schedules in-value execution over a community.
*/
func (backend *Backend) Submit(community []*primitive.Value) bool {
	if backend == nil || len(community) == 0 {
		return false
	}

	if scheduledProgramOwner(community) < 0 {
		return false
	}

	queue := backend.queues[QueueTypeNormal]
	if queue == nil {
		return false
	}

	backend.pending.Add(1)
	if queue.Schedule(backend.ctx, func() {
		defer backend.pending.Add(-1)
		backend.runHypercubeGossip(community)
	}) {
		return true
	}

	backend.pending.Add(-1)
	return false
}

/*
DrainSpawned returns all emitted Values completed by queued workloads.
*/
func (backend *Backend) DrainSpawned() []*primitive.Value {
	if backend == nil {
		return nil
	}

	var spawned []*primitive.Value
	for {
		select {
		case values := <-backend.completed:
			spawned = append(spawned, values...)
		default:
			return spawned
		}
	}
}

/*
Sync waits for queued compute work to quiesce and then drains emitted Values.
The normal cycle path still submits asynchronously; prompt/readout callers use
this when they need an observed tick before deciding whether anything resolved.
*/
func (backend *Backend) Sync(ctx context.Context) []*primitive.Value {
	if backend == nil {
		return nil
	}

	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for backend.pending.Load() > 0 {
		select {
		case <-ctx.Done():
			return backend.DrainSpawned()
		case <-backend.ctx.Done():
			return backend.DrainSpawned()
		case <-ticker.C:
		}
	}

	return backend.DrainSpawned()
}

func (backend *Backend) dispatch(queue *Queue) {
	for {
		select {
		case <-backend.ctx.Done():
			return
		default:
			task := queue.Next()
			backend.pool.Submit(task)
		}
	}
}

func (backend *Backend) runHypercubeGossip(community []*primitive.Value) {
	substrate := backend.nextSubstrate()
	if substrate == nil {
		return
	}

	ownerIdx := scheduledProgramOwner(community)
	if ownerIdx < 0 {
		return
	}

	owner := community[ownerIdx]
	programBefore := snapshotProgram(owner)
	ttlBefore := owner.TTL()
	owner.SetStatus(primitive.BUSY)

	spawned, err := substrate.HypercubeGossip(owner, community)
	finalizeProgramOwner(owner, programBefore, ttlBefore)
	if err != nil {
		errnie.Error(err)

		return
	}
	if len(spawned) == 0 {
		return
	}

	select {
	case backend.completed <- spawned:
	case <-backend.ctx.Done():
		primitive.CloseAll(spawned)
	}
}

func (backend *Backend) nextSubstrate() kernel.Substrate {
	if backend == nil || len(backend.substrates) == 0 {
		return nil
	}

	idx := backend.nextSub.Add(1) - 1
	return backend.substrates[int(idx%uint64(len(backend.substrates)))]
}

func scheduledProgramOwner(values []*primitive.Value) int {
	for idx, value := range values {
		if value != nil && value.ReadyForALU() {
			return idx
		}
	}

	return -1
}

func finalizeProgramOwner(value *primitive.Value, programBefore [primitive.ProgramWords]uint64, ttlBefore uint64) {
	if value == nil {
		return
	}

	ttlExpired := ttlExpiredAfterTick(value, ttlBefore)

	if !ttlExpired && value.Status() == primitive.READY && programChanged(value, programBefore) && value.SchedulingNext() != 0 {
		return
	}

	value.ClearProgram()
	value.SetSchedulingNext(0)
	value.SetStatus(primitive.DONE)
}

func ttlExpiredAfterTick(value *primitive.Value, before uint64) bool {
	ttl := value.TTL()
	if ttl == ^uint64(0) {
		return false
	}
	if ttl&ttlExpiredSentinel != 0 {
		value.SetProperty(primitive.TTL, 0)
		return true
	}
	if ttl != before {
		return ttl == 0 && before != 0 && before != ^uint64(0)
	}
	if ttl == 0 {
		return false
	}

	return value.DecTTL() == 0
}

func snapshotProgram(value *primitive.Value) [primitive.ProgramWords]uint64 {
	var snapshot [primitive.ProgramWords]uint64

	if value == nil {
		return snapshot
	}

	copy(snapshot[:], value.Get(primitive.ProgramRegion))

	return snapshot
}

func programChanged(value *primitive.Value, before [primitive.ProgramWords]uint64) bool {
	if value == nil {
		return false
	}

	after := value.Get(primitive.ProgramRegion)
	if len(after) != primitive.ProgramWords {
		return true
	}

	for idx, word := range after {
		if word != before[idx] {
			return true
		}
	}

	return false
}

/*
Close closes the Backend and all its substrates.
*/
func (backend *Backend) Close() error {
	errnie.Trace("compute.Backend.Close")
	backend.cancel()
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
	return backend.err.Error()
}
