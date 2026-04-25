//go:build !darwin || !cgo

package metal

import (
	"errors"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/primitive"
)

var ErrUnavailable = errors.New("metal: backend unavailable")

/*
Backend is the stub for non-darwin builds. It satisfies the full
kernel.Substrate contract by delegating every call to the cross-
substrate CPU helpers — the binary still links and runs, just without
GPU acceleration.
*/
type Backend struct {
	idx int
}

type backendOption func(*Backend)

/*
NewBackend returns a stub Backend on non-darwin.
*/
func NewBackend(idx int, opts ...backendOption) *Backend {
	backend := &Backend{
		idx: idx,
	}
	for _, opt := range opts {
		opt(backend)
	}
	return backend
}

func (backend *Backend) Close() error {
	return nil
}

/*
Available always returns zero on non-darwin.
*/
func Available() int { return 0 }

func (backend *Backend) Name() string { return "metal" }

func (backend *Backend) HypercubeGossip(value *primitive.Value, community []*primitive.Value) []*primitive.Value {
	if len(community) == 0 {
		return nil
	}

	// Just fallback to CPU for now
	return cpu.HypercubeGossip(value, community)
}

func (backend *Backend) GeometricFrame(value unsafe.Pointer, opcode uint64) bool {
	return cpu.GeometricFrame(value, opcode)
}
