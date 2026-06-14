package compute

import (
	"context"
	"errors"
	"iter"
	"runtime"
	"sync"

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
	substrates []kernel.Substrate
	community  sync.Map
}

/*
NewBackend creates a Backend with every available substrate registered. CPU is
always added last so accelerators get first crack at low-pressure work.
*/
func NewBackend(ctx context.Context) *Backend {
	ctx, cancel := context.WithCancel(ctx)

	backend := &Backend{
		ctx:       ctx,
		cancel:    cancel,
		pool:      pool.NewPool(uint64(runtime.NumCPU())),
		community: sync.Map{},
	}

	for device := 0; device < cuda.Available(); device++ {
		backend.substrates = append(
			backend.substrates,
			cuda.NewBackend(device),
		)
	}

	for device := 0; device < metal.Available(); device++ {
		backend.substrates = append(
			backend.substrates,
			metal.NewBackend(device),
		)
	}

	backend.substrates = append(
		backend.substrates,
		cpu.NewBackend(ctx),
	)

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
func (backend *Backend) Sync(ctx context.Context) iter.Seq[*SyncResult] {
	return func(yield func(*SyncResult) bool) {
		backend.community.Range(func(key, value any) bool {
			owner := value.(*primitive.Value)

			results, err := backend.substrates[len(
				backend.substrates,
			)-1].HypercubeGossip(owner, nil)

			if err != nil {
				owner.SetStatus(primitive.ERROR)
				errnie.Error(err)

				return true
			}

			ready := make([]*primitive.Value, 0)
			resolved := make([]*primitive.Value, 0)

			for _, result := range results {
				if result.Status() == primitive.RESOLVED {
					resolved = append(resolved, result)
				}

				if result.Status() == primitive.READY {
					ready = append(ready, result)
				}
			}

			if !yield(&SyncResult{
				Resolved: resolved,
				Ready:    ready,
			}) {
				return false
			}

			return true
		})
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
