//go:build !amd64 && !arm64

package cpu

import (
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
)

/*
geometricFrame is the scalar fallback for architectures without a dedicated
PGA assembly lane. AMD64 and ARM64 use native assembly to match the existing
CPU backend's AVX2 / NEON posture.
*/
func geometricFrame(value *uint64, opcode uint64) bool {
	return kernel.ExecuteGeometricFrame(unsafe.Pointer(value), opcode)
}
