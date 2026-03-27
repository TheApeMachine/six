package kernel

import (
	"context"
	"unsafe"
)

/*
Substrate is the unified contract that every compute backend
(CPU, CUDA, Metal) must satisfy. It combines the streaming IO
surface needed by the workflow pipeline with the vectorized kernel
dispatch. The compiler enforces that all backends implement every method.
*/
type Substrate interface {
	// UniversalBitwise is the primary dispatch method. It reads the 4-bit
	// opcode from each Value's instruction region and executes the
	// corresponding boolean gate across the data lanes. Values carrying a
	// full 64-tick program in Region 3 execute that program instead.
	// This is the canonical in-band instruction path.
	UniversalBitwise(a, b, dst unsafe.Pointer, n uint32) error

	// Schedule pushes abstract functional execution payloads onto the underlying worker pool.
	Schedule(job func(ctx context.Context) error)
}
