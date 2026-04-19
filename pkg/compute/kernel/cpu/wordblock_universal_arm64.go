//go:build arm64

package cpu

import "unsafe"

/*
UniversalBitwise is the ARM64 entry point: a NEON/scalar virtual machine
that walks the Value's program region (words 16..31), decodes each packed
64-bit instruction (see pkg/compute/program for the encoding), and applies
the truth-table sweep in place.

The implementation lives in wordblock_universal_arm64.s. The Go side is
purely a //go:noescape stub so the call sequence is:

	backend.ExecutePointers([]unsafe.Pointer{ptr})
	  └─ UniversalBitwise(ptr)            ; one CALL into the ASM kernel
	       └─ <decode + sweep + writeback for every instruction>

There is no per-instruction Go decoding, no scratch buffer allocation in
managed memory, and no escape analysis on the value pointer.
*/

//go:noescape
func UniversalBitwise(value unsafe.Pointer)
