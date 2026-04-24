package cpu

import (
	"context"
	"runtime"
	"sync/atomic"
	"unsafe"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
Backend is the CPU substrate. It implements the unified ExecuteCommunity
kernel for population-vectored AST execution.
*/
type Backend struct {
	ctx      context.Context
	cancel   context.CancelFunc
	err      error
	inflight atomic.Int64
	emaNs    atomic.Uint64
}

func NewBackend(ctx context.Context) *Backend {
	ctx, cancel := context.WithCancel(ctx)
	return &Backend{
		ctx:    ctx,
		cancel: cancel,
	}
}

func Available() int                   { return runtime.NumCPU() }
func (backend *Backend) Shutdown()     { backend.cancel() }
func (backend *Backend) Name() string  { return "cpu" }
func (backend *Backend) Error() string { return backend.err.Error() }

// ExecuteCommunity applies the resident program of the first Value
// (which acts as the SIMD program) across all Values in the community.
func (backend *Backend) ExecuteCommunity(community []*primitive.Value) []*primitive.Value {
	return ExecuteCommunity(community)
}

// GeometricFrame applies PGA rotations/translations to the continuous
// representation of a Value.
func (backend *Backend) GeometricFrame(value unsafe.Pointer, opcode uint64) bool {
	return GeometricFrame(value, opcode)
}
