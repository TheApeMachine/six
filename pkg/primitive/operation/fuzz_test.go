package operation

import (
	"encoding/binary"
	"testing"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
fillValueRepeat writes 1024 bytes derived from data (xor salt per byte index)
into value. Empty data is skipped by callers.
*/
func fillValueRepeat(t *testing.T, value *primitive.Value, data []byte, salt byte) {
	t.Helper()

	buf := make([]byte, primitive.ByteSize)

	for i := range buf {
		buf[i] = data[i%len(data)] ^ salt ^ byte(i)
	}

	_, err := value.Write(buf)
	if err != nil {
		t.Fatal(err)
	}
}

/*
FuzzBitwiseBinaryOps exercises every fixed-width binary Op on random fields.

	go test ./pkg/primitive/operation -fuzz=FuzzBitwiseBinaryOps -fuzztime=30s
*/
func FuzzBitwiseBinaryOps(f *testing.F) {
	f.Add([]byte{0x01, 0x02, 0x03, 0x04})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			t.Skip()
		}

		left := primitive.NewValue()
		right := primitive.NewValue()
		fillValueRepeat(t, left, data, 0)
		fillValueRepeat(t, right, data, 0xA5)

		dst := primitive.NewValue()

		AND(left[:], right[:], dst[:])
		dst.Clamp()

		OR(left[:], right[:], dst[:])
		dst.Clamp()

		XOR(left[:], right[:], dst[:])
		dst.Clamp()

		AndNot(left[:], right[:], dst[:])
		dst.Clamp()

		NAND(left[:], right[:], dst[:])
		dst.Clamp()

		NOR(left[:], right[:], dst[:])
		dst.Clamp()

		XNOR(left[:], right[:], dst[:])
		dst.Clamp()

		ConverseNonimplication(left[:], right[:], dst[:])
		dst.Clamp()

		NOT(left[:], nil, dst[:])
		dst.Clamp()
	})
}

/*
FuzzMotorApply exercises MotorApply on random motor sources and payloads.
*/
func FuzzMotorApply(f *testing.F) {
	f.Add([]byte{0x55, 0xAA})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			t.Skip()
		}

		motorSource := primitive.NewValue()
		payload := primitive.NewValue()
		fillValueRepeat(t, motorSource, data, 1)
		fillValueRepeat(t, payload, data, 2)

		dst := primitive.NewValue()
		MotorApply(motorSource[:], payload[:], dst[:])
		dst.Clamp()
	})
}

/*
FuzzMotorInvertInvertibleOnly calls MotorInvert only when InvertMotor would
succeed for the derived left motor. Inputs that are non-invertible are skipped
so the fuzzer does not treat the operation-layer panic as a regression.

	go test ./pkg/primitive/operation -fuzz=FuzzMotorInvertInvertibleOnly -fuzztime=30s
*/
func FuzzMotorInvertInvertibleOnly(f *testing.F) {
	f.Add([]byte{0x03, 0x07})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			t.Skip()
		}

		motorSource := primitive.NewValue()
		payload := primitive.NewValue()
		fillValueRepeat(t, motorSource, data, 3)
		fillValueRepeat(t, payload, data, 4)

		scale, translate := motorSource.Motor()
		_, _, err := primitive.InvertMotor(scale, translate)
		if err != nil {
			t.Skip()
		}

		dst := primitive.NewValue()
		MotorInvert(motorSource[:], payload[:], dst[:])
		dst.Clamp()
	})
}

/*
FuzzMotorComposeOp exercises MotorCompose on random left/right Values.
*/
func FuzzMotorComposeOp(f *testing.F) {
	f.Add([]byte{0x0F, 0xF0})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			t.Skip()
		}

		left := primitive.NewValue()
		right := primitive.NewValue()
		fillValueRepeat(t, left, data, 5)
		fillValueRepeat(t, right, data, 6)

		dst := primitive.NewValue()
		MotorCompose(left[:], right[:], dst[:])
		dst.Clamp()
	})
}

/*
FuzzRollLeft exercises circular shift for arbitrary shift magnitudes.
*/
func FuzzRollLeft(f *testing.F) {
	f.Add([]byte{0x01, 0x02, 0x03, 0x04, 0x05})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			t.Skip()
		}

		src := primitive.NewValue()
		fillValueRepeat(t, src, data, 7)

		dst := primitive.NewValue()

		var word [4]byte
		copy(word[:], data)
		shift := int(binary.LittleEndian.Uint32(word[:]))

		RollLeft(src, dst, shift)
		dst.Clamp()
	})
}

/*
FuzzXORAssociativityThreeWay checks (A^B)^C == A^(B^C) on the core field.
*/
func FuzzXORAssociativityThreeWay(f *testing.F) {
	f.Add([]byte{0xDE, 0xAD})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			t.Skip()
		}

		a := primitive.NewValue()
		b := primitive.NewValue()
		c := primitive.NewValue()
		fillValueRepeat(t, a, data, 8)
		fillValueRepeat(t, b, data, 9)
		fillValueRepeat(t, c, data, 10)

		ab := primitive.NewValue()
		bc := primitive.NewValue()
		left := primitive.NewValue()
		right := primitive.NewValue()

		XOR(a[:], b[:], ab[:])
		ab.Clamp()
		XOR(ab[:], c[:], left[:])
		left.Clamp()

		XOR(b[:], c[:], bc[:])
		bc.Clamp()
		XOR(a[:], bc[:], right[:])
		right.Clamp()

		if !left.Equal(right) {
			t.Fatal("XOR associativity failed on core field")
		}
	})
}
