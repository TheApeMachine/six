//go:build !amd64 && !arm64

package cpu

import "unsafe"

/*
GeometricFrame dispatches the PGA lane through the scalar reference on
architectures without a dedicated assembly implementation.
*/
func GeometricFrame(value unsafe.Pointer, opcode uint64) bool {
	return geometricFrameGeneric(value, opcode)
}
