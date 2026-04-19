//go:build amd64

package cpu

import "unsafe"

/*
UniversalBitwise is the AMD64 entry point: a scalar+POPCNTQ virtual machine
that walks the Value's program region (words 16..31), decodes each packed
64-bit instruction (see pkg/compute/program for the encoding), and applies
the truth-table sweep in place.

The implementation lives in wordblock_universal_amd64.s. The Go side is
purely a //go:noescape stub.
*/

//go:noescape
func UniversalBitwise(value unsafe.Pointer)
