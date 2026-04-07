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
/*
Substrate is the contract every compute backend (CPU, CUDA, Metal)
satisfies. One method dispatches all operations. The programmer
compiles intent into the Value’s layout; the kernel reads the
opcode from the program region and dispatches internally to the
right hardware path (truth table ALU, CSA, batch popcount, etc.).

Memory contract: each pointer must reference a full Value frame
(1024 bytes / 128×uint64 by default) aligned for uintptr use.
Frames remain owned by the caller; the substrate mutates them in
place and does not retain pointers after return.
*/
type Substrate interface {
	Execute(frames []unsafe.Pointer) error
	Name() string
}
