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
StageRequest describes a legacy stage(B) pairing for docs and future plumbing.
Active recruitment uses in-frame SELECTED/reference tags; HypercubeGossip
returns spawned children only.
*/
type StageRequest struct {
	OwnerID uint64
	Value   *primitive.Value
}

type Substrate interface {
	Name() string
	HypercubeGossip(value *primitive.Value, values []*primitive.Value) (results []*primitive.Value, err error)
	GeometricFrame(value unsafe.Pointer, opcode uint64) bool
	Close() error
}
