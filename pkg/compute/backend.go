package compute

import (
	"context"
	"runtime"
	"sync/atomic"

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

	queue := backend.queues[QueueTypeNormal]
	if queue == nil {
		return false
	}

	return queue.Schedule(backend.ctx, func() {
		backend.runHypercubeGossip(community)
	})
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

	spawned := substrate.HypercubeGossip(programOwner(community), community)
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

	for _, substrate := range backend.substrates {
		if substrate.Name() == "cpu" {
			return substrate
		}
	}

	idx := backend.nextSub.Add(1) - 1
	return backend.substrates[int(idx%uint64(len(backend.substrates)))]
}

func programOwner(values []*primitive.Value) *primitive.Value {
	for _, value := range values {
		if value != nil && value.HasProgram() {
			return value
		}
	}

	return nil
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
