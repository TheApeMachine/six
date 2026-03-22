package internaltest

import (
	"unsafe"

	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/primitive/operation"
)

/*
SampleBatch returns deterministic operand batches for backend correctness tests.
*/
func SampleBatch(numValues int) ([]primitive.Value, []primitive.Value) {
	left := make([]primitive.Value, numValues)
	right := make([]primitive.Value, numValues)

	for index := range numValues {
		primitive.BaseValueInto(&left[index], byte((index*37+11)%255+1))
		primitive.BaseValueInto(&right[index], byte((index*53+29)%255+1))

		left[index].Set((index*97 + 7) % primitive.CoreBits)
		right[index].Set((index*193 + 19) % primitive.CoreBits)
	}

	return left, right
}

/*
ValuesPointer returns the first-element pointer for a non-empty Value batch.
*/
func ValuesPointer(values []primitive.Value) unsafe.Pointer {
	return unsafe.Pointer(&values[0])
}

/*
ExpectedBinary applies a binary primitive operation across a Value batch.
*/
func ExpectedBinary(left, right []primitive.Value, op operation.Op) []primitive.Value {
	out := make([]primitive.Value, len(left))

	for index := range left {
		op(left[index][:], right[index][:], out[index][:])
		out[index].Clamp()
	}

	return out
}

/*
ExpectedUnary applies a unary primitive operation across a Value batch.
*/
func ExpectedUnary(values []primitive.Value, op operation.Op) []primitive.Value {
	out := make([]primitive.Value, len(values))

	for index := range values {
		op(values[index][:], nil, out[index][:])
		out[index].Clamp()
	}

	return out
}

/*
ExpectedMotorApply applies the primitive motor transform across a Value batch.
*/
func ExpectedMotorApply(left, right []primitive.Value) []primitive.Value {
	out := make([]primitive.Value, len(left))

	for index := range left {
		operation.MotorApply(left[index][:], right[index][:], out[index][:])
		out[index].Clamp()
	}

	return out
}

/*
ExpectedMotorInvert applies the primitive inverse motor transform across a Value batch.
*/
func ExpectedMotorInvert(left, right []primitive.Value) []primitive.Value {
	out := make([]primitive.Value, len(left))

	for index := range left {
		operation.MotorInvert(left[index][:], right[index][:], out[index][:])
		out[index].Clamp()
	}

	return out
}

/*
ExpectedMotorCompose applies the primitive composed motor transform across a Value batch.
*/
func ExpectedMotorCompose(left, right []primitive.Value) []primitive.Value {
	out := make([]primitive.Value, len(left))

	for index := range left {
		operation.MotorCompose(left[index][:], right[index][:], out[index][:])
		out[index].Clamp()
	}

	return out
}

/*
ExpectedRollLeft applies structural rotation across a Value batch.
*/
func ExpectedRollLeft(values []primitive.Value, shift int) []primitive.Value {
	out := make([]primitive.Value, len(values))

	for index := range values {
		operation.RollLeft(&values[index], &out[index], shift)
		out[index].Clamp()
	}

	return out
}
