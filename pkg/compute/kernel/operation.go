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
	//
	// Memory contract: a and b must each point to a valid Value frame
	// (1024 bytes, 128×uint64 words in little-endian order per word) suitable for the host
	// and sufficiently aligned for uintptr(unsafe.Pointer) use (typical
	// Go heap allocation is word-aligned; do not pass arbitrary byte slices
	// without ensuring alignment and size). Both frames remain owned by the
	// caller; the callee reads and writes them in place during the call and
	// does not retain the pointers after it returns. Callers must not free
	// or reuse the backing storage until UniversalBitwise returns.
	// Do not invoke concurrently on the same *Value instances unless you
	// externally synchronize those frames; distinct Value pointers may be
	// used from different goroutines per backend.
	UniversalBitwise(a, b unsafe.Pointer, count int) error

	// Schedule runs job on the backend worker path (or synchronously if
	// there is no pool). When a pool is used, the returned error reflects
	// enqueue / context cancellation only; the job itself may still fail
	// asynchronously inside the pool. Without a pool, the error is the job's.
	Schedule(job func(ctx context.Context) error) error
}
