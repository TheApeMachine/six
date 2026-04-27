package kernel

import (
	"unsafe"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
Substrate is the unified contract every compute backend (CPU, Metal, CUDA)
implements. The new population-vectored AST abstracts all networking,
state transitions, and computation into a single execution loop.

  - HypercubeGossip: Applies the packed 64-bit AST instructions contained
    in the resident program region of the first Value (or all Values
    concurrently) across the entire community in SIMD lockstep.
    Routing (self, next, fold, spawn), math, and predication are handled
    internally by the kernel.

  - GeometricFrame: Applies Projective Geometric Algebra (PGA) rotations
    or translations. (Kept for compatibility with the continuous math path).

Memory: every *Value is a full 128-word frame owned by the caller.
Substrates mutate in place and do not retain pointers after return.
*/
/*
StageRequest is one in-band staging directive emitted by a program's stage
instruction during a kernel sweep. OwnerID names the staging lane (the
recruiter / next-program id stamped into A.properties.reference at sweep
time); Value is the popped B that should land in that lane. The compute
backend turns the slice into StageInto calls after the sweep retires.
*/
type StageRequest struct {
	OwnerID uint64
	Value   *primitive.Value
}

type Substrate interface {
	Name() string
	HypercubeGossip(value *primitive.Value, values []*primitive.Value) (spawned []*primitive.Value, staged []StageRequest, err error)
	GeometricFrame(value unsafe.Pointer, opcode uint64) bool
	Close() error
}
