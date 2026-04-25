package cpu

import (
	"context"
	"runtime"
	"unsafe"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
Backend is the CPU substrate. It implements the unified HypercubeGossip
kernel for population-vectored AST execution.
*/
type Backend struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func NewBackend(ctx context.Context) *Backend {
	ctx, cancel := context.WithCancel(ctx)
	return &Backend{
		ctx:    ctx,
		cancel: cancel,
	}
}

func Available() int { return runtime.NumCPU() }

func (backend *Backend) Name() string { return "cpu" }

func (backend *Backend) Close() error {
	backend.cancel()
	return nil
}

/*
HypercubeGossip diffuses the values across the community using a hypercube.
This unifies the execution, and the networking across values, effectively
allowing data exchange as a first-class citizen in the programming model.
*/
func (backend *Backend) HypercubeGossip(
	value *primitive.Value, values []*primitive.Value,
) []*primitive.Value {
	return HypercubeGossip(value, values)
}

// GeometricFrame applies PGA rotations/translations to the continuous
// representation of a Value.
func (backend *Backend) GeometricFrame(value unsafe.Pointer, opcode uint64) bool {
	return GeometricFrame(value, opcode)
}
