package compute

import (
	"context"
	"runtime"
	"sync/atomic"

	"github.com/smallnest/ringbuffer"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/compute/kernel/metal"
	"github.com/theapemachine/six/pkg/core"
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
	rb         *ringbuffer.RingBuffer
	substrates []kernel.Substrate
	pool       *pool.Pool
	popped     atomic.Int64
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
		rb:         ringbuffer.New(core.Cfg.Value.Bytes * 64),
		substrates: make([]kernel.Substrate, 0),
		pool:       pool.NewPool(uint64(runtime.NumCPU())),
	}
	backend.rb.SetBlocking(true)

	for device := 0; device < cuda.Available(); device++ {
		backend.substrates = append(backend.substrates, cuda.NewBackend(device))
	}

	for device := 0; device < metal.Available(); device++ {
		backend.substrates = append(backend.substrates, metal.NewBackend(device))
	}

	backend.substrates = append(backend.substrates, cpu.NewBackend(ctx))

	return backend
}

/*
Read implements io.Reader and allows the Backend to plug into
the io pipeline directly.
*/
func (backend *Backend) Read(p []byte) (n int, err error) {
	errnie.Trace("compute.Backend.Read")

	select {
	case <-backend.ctx.Done():
		return 0, backend.ctx.Err()
	default:
		return backend.rb.Read(p)
	}
}

/*
Write implements io.Writer and allows the Backend to plug into
the io pipeline directly.
*/
func (backend *Backend) Write(p []byte) (n int, err error) {
	errnie.Trace("compute.Backend.Write")

	select {
	case <-backend.ctx.Done():
		return 0, backend.ctx.Err()
	default:
		value := primitive.AllocValue()

		if err := value.LoadFullFrame(p); err != nil {
			primitive.FreeValue(value)
			return 0, errnie.Error(err)
		}

		backend.pool.Schedule(func() {
			defer primitive.FreeValue(value)

			backend.substrates[len(backend.substrates)-1].UniversalBitwise(
				kernel.NewOptimizer(value, kernel.StrategyRotate),
			)

			if _, err := backend.rb.Write(value.Bytes()); err != nil {
				errnie.Error(err)
			}
		})

		return len(p), nil
	}
}

/*
Close closes the Backend.
*/
func (backend *Backend) Close() error {
	errnie.Trace("compute.Backend.Close")
	backend.cancel()
	return nil
}

/*
Error implements the error interface.
*/
func (backend *Backend) Error() string {
	return backend.err.Error()
}
