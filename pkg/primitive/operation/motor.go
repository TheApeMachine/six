package operation

import (
	"math/bits"
	"unsafe"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
applyMotorToValue remaps every active bit in input through one affine operator.
*/
func applyMotorToValue(input *primitive.Value, out *primitive.Value, scale, translate uint16) {
	*out = primitive.Value{}

	for wordIndex := range primitive.Words {
		word := (*input)[wordIndex]

		if wordIndex == primitive.Words-1 {
			word &= primitive.LastMask
		}

		for word != 0 {
			bitOffset := bits.TrailingZeros64(word)
			position := uint16(wordIndex*64 + bitOffset)
			mapped := primitive.ApplyMotor(scale, translate, position)
			out.Set(int(mapped))
			word &= word - 1
		}
	}

	out.Clamp()
}

var (
	// MotorApply derives motor(A) and applies it to operand B.
	MotorApply Op = func(a, b, dst []uint64) {
		left := (*primitive.Value)(unsafe.Pointer(unsafe.SliceData(a)))
		right := (*primitive.Value)(unsafe.Pointer(unsafe.SliceData(b)))
		out := (*primitive.Value)(unsafe.Pointer(unsafe.SliceData(dst)))
		scale, translate := left.Motor()

		applyMotorToValue(right, out, scale, translate)
	}

	// MotorInvert derives inverse motor(A) and applies it to operand B.
	MotorInvert Op = func(a, b, dst []uint64) {
		left := (*primitive.Value)(unsafe.Pointer(unsafe.SliceData(a)))
		right := (*primitive.Value)(unsafe.Pointer(unsafe.SliceData(b)))
		out := (*primitive.Value)(unsafe.Pointer(unsafe.SliceData(dst)))
		scale, translate := left.Motor()
		invScale, invTranslate, inverseErr := primitive.InvertMotor(scale, translate)
		if inverseErr != nil {
			panic(inverseErr)
		}

		applyMotorToValue(right, out, invScale, invTranslate)
	}

	// MotorCompose composes motor(A) then motor(B), then applies to B.
	MotorCompose Op = func(a, b, dst []uint64) {
		left := (*primitive.Value)(unsafe.Pointer(unsafe.SliceData(a)))
		right := (*primitive.Value)(unsafe.Pointer(unsafe.SliceData(b)))
		out := (*primitive.Value)(unsafe.Pointer(unsafe.SliceData(dst)))

		scaleA, translateA := left.Motor()
		scaleB, translateB := right.Motor()
		composedScale, composedTranslate := primitive.ComposeMotor(
			scaleA,
			translateA,
			scaleB,
			translateB,
		)

		applyMotorToValue(right, out, composedScale, composedTranslate)
	}
)
