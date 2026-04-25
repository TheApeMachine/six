//go:build amd64 || arm64

package cpu

import "unsafe"

func geometricFrame(value unsafe.Pointer, opcode uint64) bool

/*
GeometricFrame dispatches the active PGA lane to the architecture-specific
assembly kernel. The generic implementation remains the reference for
non-SIMD architectures and for parity tests.
*/
func GeometricFrame(value unsafe.Pointer, opcode uint64) bool {
	return geometricFrame(value, opcode)
}
