package kernel

import (
	"unsafe"
)

/*
Substrate is the unified contract that every compute backend
(CPU, CUDA, Metal) must satisfy. It combines the streaming IO
surface needed by the workflow pipeline with the vectorized kernel
dispatch. The compiler enforces that all backends implement every method.
*/
type Substrate interface {
	// UniversalBitwise is the primary dispatch method. Each element of frames
	// is one Value: in-band programs read operands from that frame’s own
	// layout (token region and register file); there is no second “partner”
	// frame in this contract. Implementations execute every non-nil pointer
	// in order and may pack non-contiguous host pointers into accelerator
	// batch buffers internally.
	//
	// Memory contract: each pointer must reference a full Value frame (1024
	// bytes / 128×uint64 by default, per core.Cfg) aligned for uintptr use.
	// Frames remain owned by the caller; the callee mutates them in place
	// and does not retain pointers after return. Do not overlap concurrent
	// UniversalBitwise calls on the same frames without external sync.
	UniversalBitwise(frames []unsafe.Pointer) error

	// BatchDistances computes Hamming distances from a single query
	// affinity vector to count contiguous candidate vectors. Each vector
	// is AffinityWords × uint64 (64 bytes). Results are written as uint32
	// distances into the caller-provided output slice.
	//
	// CPU substrates dispatch to SIMD assembly (NEON/AVX2). GPU substrates
	// launch one thread per candidate for massively parallel popcount.
	BatchDistances(
		query unsafe.Pointer,
		candidates unsafe.Pointer,
		count int,
		distances []uint32,
	) error

	// Name returns a human-readable identifier for
	// the substrate (e.g. “cpu”, “cuda”, “metal”).
	Name() string
}
