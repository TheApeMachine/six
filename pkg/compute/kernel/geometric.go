package kernel

import (
	"unsafe"

	"github.com/theapemachine/six/pkg/core/numeric/geometry"
)

const (
	OpcodeBooleanMask   uint64 = 0x0F
	OpcodeGeometricMask uint64 = 0xF0

	OpcodeGeometricCompose  uint64 = 0x10
	OpcodeGeometricSandwich uint64 = 0x20
	OpcodeGeometricReverse  uint64 = 0x30

	ContextStartWord  = 32
	GradientStartWord = 40
)

/*
FrameProgramRawOpcode returns the low byte of the first program word.
Boolean kernels still consume the low nibble; the geometric lane uses the
high nibble so opcodes like 0x20 cannot be collapsed to Boolean FALSE.
*/
func FrameProgramRawOpcode(frame unsafe.Pointer) uint8 {
	if frame == nil {
		return 0
	}

	frameWords := (*[128]uint64)(frame)

	return uint8(frameWords[ProgramStartWord] & 0xFF)
}

/*
IsGeometricOpcode reports whether an opcode should route to the PGA ALU lane.
Only the high nibble is considered so future low-nibble flags can ride along
without changing the dispatch branch.
*/
func IsGeometricOpcode(opcode uint64) bool {
	switch opcode & OpcodeGeometricMask {
	case OpcodeGeometricCompose, OpcodeGeometricSandwich, OpcodeGeometricReverse:
		return true
	}

	return false
}

/*
ExecuteGeometricFrame applies one PGA operation in-place on a Value frame.
Context holds the left operand or motor, Gradient holds the right operand or
target, and Signals receives the resulting 8-float64 multivector. Returning
false means the opcode does not belong to this lane.
*/
func ExecuteGeometricFrame(frame unsafe.Pointer, opcode uint64) bool {
	if frame == nil || !IsGeometricOpcode(opcode) {
		return false
	}

	frameWords := (*[128]uint64)(frame)
	left := multivectorAt(frameWords, ContextStartWord)
	right := multivectorAt(frameWords, GradientStartWord)

	var out geometry.Multivector

	switch opcode & OpcodeGeometricMask {
	case OpcodeGeometricCompose:
		out = left.GeometricProduct(right)
	case OpcodeGeometricSandwich:
		out = left.Sandwich(right)
	case OpcodeGeometricReverse:
		out = left.Reverse()
	}

	writeMultivectorAt(frameWords, SignalsStartWord, out)

	return true
}

func multivectorAt(frameWords *[128]uint64, start int) geometry.Multivector {
	return *(*geometry.Multivector)(unsafe.Pointer(&frameWords[start]))
}

func writeMultivectorAt(frameWords *[128]uint64, start int, mv geometry.Multivector) {
	*(*geometry.Multivector)(unsafe.Pointer(&frameWords[start])) = mv
}
