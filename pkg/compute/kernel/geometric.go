package kernel

import "unsafe"

/*
ExecuteGeometricFrame applies one PGA operation in-place on a Value frame.
AMD64/ARM64 cpu backends use assembly for this path; generic builds and
fallbacks use this implementation when present.

When this returns false, callers should fall back to the unified bitwise kernel.
The full PGA lane is still being consolidated around fixed opcodes on Values;
non-amd64/arm64 stubs may return false until the scalar reference is wired.
*/
func ExecuteGeometricFrame(frame unsafe.Pointer, opcode uint64) bool {
	if frame == nil || !IsGeometricOpcode(opcode) {
		return false
	}

	_ = (*[128]uint64)(frame)

	return false
}
