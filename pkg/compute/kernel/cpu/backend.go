package cpu

import (
	"context"
	"runtime"

	"github.com/theapemachine/six/pkg/compute/kernel"
)

type Backend struct {
	ctx    context.Context
	cancel context.CancelFunc
}

type backendOption func(*Backend)

func NewBackend(ctx context.Context, opts ...backendOption) *Backend {
	ctx, cancel := context.WithCancel(ctx)

	backend := &Backend{
		ctx:    ctx,
		cancel: cancel,
	}

	for _, opt := range opts {
		opt(backend)
	}

	return backend
}

func (backend *Backend) Shutdown() {
	if backend == nil || backend.cancel == nil {
		return
	}

	backend.cancel()
}

func Available() int                  { return runtime.NumCPU() }
func (backend *Backend) Name() string { return "cpu" }

/*
UniversalBitwise runs the SIMD ALU pass on the given Optimizer frame.
*/
func (backend *Backend) UniversalBitwise(frame *kernel.Optimizer) {
	if frame == nil {
		return
	}

	for idx, op := range frame.OP {
		aSlice := frame.A[idx]
		bSlice := frame.B[idx]
		dstSlice := frame.DST[idx]

		// If this instruction slot is empty, we're done
		if len(aSlice) == 0 || len(bSlice) == 0 || len(dstSlice) == 0 {
			break
		}

		for i := range dstSlice {
			a := aSlice[idx%len(aSlice)]
			b := bSlice[idx%len(bSlice)]

			frame.RETURN[idx][i] ^= (a & b & (uint64(0) - (op & 1))) |
				(a & ^b & (uint64(0) - ((op >> 1) & 1))) |
				(^a & b & (uint64(0) - ((op >> 2) & 1))) |
				(^a & ^b & (uint64(0) - ((op >> 3) & 1)))
		}
	}
}
