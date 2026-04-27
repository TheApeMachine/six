package compute

import (
	"context"
	"errors"
	"iter"
	"os"
	"runtime"
	"strings"
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
	}

	if strings.EqualFold(strings.TrimSpace(os.Getenv("SIX_SUBSTRATE")), "cpu") {
		backend.cpuSubstrate = backend.addSubstrate(cpu.NewBackend(ctx))
	} else {
		for device := 0; device < cuda.Available(); device++ {
			backend.addSubstrate(cuda.NewBackend(device))
		}

		for device := 0; device < metal.Available(); device++ {
			backend.addSubstrate(metal.NewBackend(device))
		}

		backend.cpuSubstrate = backend.addSubstrate(cpu.NewBackend(ctx))
	}

	if !createAndAssignQueue(ctx, backend, QueueTypeNormal) {
		return nil
	}

	if !createAndAssignQueue(ctx, backend, QueueTypePriority) {
		return nil
	}

	go backend.dispatch(backend.queues[QueueTypePriority])
	go backend.dispatch(backend.queues[QueueTypeNormal])

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

	backend.pending.Add(1)

	if err = backend.queues[QueueTypeNormal].Schedule(backend.ctx, func() {
		defer backend.pending.Add(-1)
		backend.runHypercubeGossip(owner, community)
	}); err != nil {
		backend.pending.Add(-1)
		owner.SetStatus(primitive.READY)

		return err
	}

	return nil
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

func (backend *Backend) runHypercubeGossip(owner *primitive.Value, community []*primitive.Value) {
	substrateState := backend.nextSubstrate()
	if substrateState == nil {
		return
	}

	before := snapshotProgram(owner)

	owner.SetStatus(primitive.BUSY)

	kernelValues := make([]*primitive.Value, 0, 1+len(community))
	kernelValues = append(kernelValues, owner)

	for _, value := range community {
		if value == nil {
			continue
		}

		if value == owner {
			continue
		}

		kernelValues = append(kernelValues, value)
	}

	spawned, err := backend.executeSubstrate(substrateState, owner, kernelValues)

	if err != nil && backend.cpuSubstrate != nil && substrateState != backend.cpuSubstrate {
		errnie.Error(err)
		primitive.CloseAll(spawned)
		spawned, err = backend.executeSubstrate(backend.cpuSubstrate, owner, kernelValues)
	}

	if err != nil {
		errnie.Error(err)
		primitive.CloseAll(spawned)
		owner.SetStatus(primitive.ERROR)
		return
	}

	backend.finalizeOwner(owner, before)

	backend.cache.Range(func(key any, value any) bool {
		value.(*primitive.Value).SetStatus(primitive.DONE)
		return true
	})

	for _, value := range spawned {
		backend.cache.Store(value.ID(), value)
	}
}

func (backend *Backend) finalizeOwner(owner *primitive.Value, before [primitive.ProgramWords]uint64) {
	finalizeExecutedOwner(owner, before)
}

func finalizeExecutedOwner(owner *primitive.Value, before [primitive.ProgramWords]uint64) {
	if owner == nil {
		return
	}

	if ttl := owner.TTL(); ttl > 0 && ttl != ^uint64(0) {
		if ttl == 1 {
			owner.SetProperty(primitive.TTL, ttlExpiredSentinel)
			owner.SetSchedulingNext(0)
		} else {
			owner.SetProperty(primitive.TTL, ttl-1)
		}
	}

	// HypercubeGossip / in-kernel firmware can move the owner back to READY
	// with a non-zero continuation. That mark is the only cross-sweep control
	// contract; the current sweep never changes its own PC.
	if owner.Status() == primitive.READY && owner.SchedulingNext() != 0 {
		return
	}

	owner.ClearProgram()
	owner.SetSchedulingNext(0)
	owner.SetStatus(primitive.DONE)
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

	spawned, err := state.HypercubeGossip(owner, community)

	state.inflight.Add(-1)
	state.observe(time.Since(start))

	return spawned, err
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

func (backend *Backend) nextSubstrate() *substrateState {
	if backend == nil || len(backend.substrates) == 0 {
		return nil
	}

	start := backend.nextSub.Add(1) - 1
	var best *substrateState
	bestScore := ^uint64(0)

	for offset := range backend.substrates {
		idx := int((start + uint64(offset)) % uint64(len(backend.substrates)))
		candidate := backend.substrates[idx]
		score := candidate.pressure()

		if score < bestScore {
			best = candidate
			bestScore = score
		}
	}

	return best
}

func (state *substrateState) pressure() uint64 {
	if state == nil {
		return ^uint64(0)
	}

	inflight := state.inflight.Load()
	if inflight < 0 {
		inflight = 0
	}

	service := state.serviceNanos.Load()
	if service < 1 {
		service = 1
	}

	return uint64(inflight+1) * uint64(service)
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
