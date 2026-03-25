package kernel

import "unsafe"

/*
Substrate is the unified contract that every compute backend
(CPU, CUDA, Metal) must satisfy. It combines the streaming IO
surface needed by the workflow pipeline with the vectorized kernel
dispatch. The compiler enforces that all backends implement every method.
*/
type Substrate interface {
	// Streaming IO — pipeline integration.
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Close() error

	// UniversalBitwise is the primary dispatch method. It reads the 4-bit
	// opcode from each Value's instruction region and executes the
	// corresponding boolean gate across the data lanes. Values carrying a
	// full 64-tick program in Region 3 execute that program instead.
	// This is the canonical in-band instruction path.
	UniversalBitwise(a, b, dst unsafe.Pointer, n uint32) error
}
