package cpu

import (
	"context"
	"runtime"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/primitive"
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
Execute resolves each pool index to its Value pointer and hands the raw
1024-byte frame to UniversalBitwise. The kernel decodes the resident
program region (a stream of packed 64-bit instructions) and applies it in
place — there is no Go-side decoding, no per-frame argument extraction.
*/
func (backend *Backend) Execute(indices []uint32) error {
	if len(indices) == 0 {
		return nil
	}

	ptrs := make([]unsafe.Pointer, 0, len(indices))

	for _, idx := range indices {
		value := primitive.ValueAt(idx)
		if value == nil {
			return NewCPUKernelError(kernel.KernelErrNilPointer, nil, "Execute")
		}

		ptrs = append(ptrs, unsafe.Pointer(&value[0]))
	}

	return backend.ExecutePointers(ptrs)
}

/*
ExecutePointers runs the CPU ALU on host pointers (heap Values and tests).
*/
func (backend *Backend) ExecutePointers(frames []unsafe.Pointer) error {
	if len(frames) == 0 {
		return nil
	}

	for _, ptr := range frames {
		if ptr == nil {
			return NewCPUKernelError(kernel.KernelErrNilPointer, nil, "ExecutePointers")
		}

		UniversalBitwise(ptr)
	}

	return nil
}
