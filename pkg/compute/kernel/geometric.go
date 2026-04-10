package kernel

import "unsafe"

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

	var out frameMultivector

	switch opcode & OpcodeGeometricMask {
	case OpcodeGeometricCompose:
		out = left.geometricProduct(right)
	case OpcodeGeometricSandwich:
		out = left.sandwich(right)
	case OpcodeGeometricReverse:
		out = left.reverse()
	}

	writeMultivectorAt(frameWords, SignalsStartWord, out)

	return true
}

type frameMultivector [8]float64

func multivectorAt(frameWords *[128]uint64, start int) frameMultivector {
	return *(*frameMultivector)(unsafe.Pointer(&frameWords[start]))
}

func writeMultivectorAt(frameWords *[128]uint64, start int, mv frameMultivector) {
	*(*frameMultivector)(unsafe.Pointer(&frameWords[start])) = mv
}

func (mv frameMultivector) geometricProduct(other frameMultivector) frameMultivector {
	return frameMultivector{
		mv[0]*other[0] - mv[4]*other[4] - mv[5]*other[5] - mv[6]*other[6],

		mv[0]*other[1] + mv[1]*other[0] - mv[2]*other[4] + mv[3]*other[5] +
			mv[4]*other[2] - mv[5]*other[3] - mv[6]*other[7] - mv[7]*other[6],

		mv[0]*other[2] + mv[1]*other[4] + mv[2]*other[0] - mv[3]*other[6] -
			mv[4]*other[1] - mv[5]*other[7] + mv[6]*other[3] - mv[7]*other[5],

		mv[0]*other[3] - mv[1]*other[5] + mv[2]*other[6] + mv[3]*other[0] -
			mv[4]*other[7] + mv[5]*other[1] - mv[6]*other[2] - mv[7]*other[4],

		mv[0]*other[4] + mv[4]*other[0] + mv[5]*other[6] - mv[6]*other[5],

		mv[0]*other[5] - mv[4]*other[6] + mv[5]*other[0] + mv[6]*other[4],

		mv[0]*other[6] + mv[4]*other[5] - mv[5]*other[4] + mv[6]*other[0],

		mv[0]*other[7] + mv[1]*other[6] + mv[2]*other[5] + mv[3]*other[4] +
			mv[4]*other[3] + mv[5]*other[2] + mv[6]*other[1] + mv[7]*other[0],
	}
}

func (mv frameMultivector) reverse() frameMultivector {
	return frameMultivector{
		mv[0],
		-mv[1],
		-mv[2],
		-mv[3],
		-mv[4],
		-mv[5],
		-mv[6],
		mv[7],
	}
}

func (mv frameMultivector) sandwich(target frameMultivector) frameMultivector {
	return mv.geometricProduct(target).geometricProduct(mv.reverse())
}
