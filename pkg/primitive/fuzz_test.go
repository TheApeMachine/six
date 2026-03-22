package primitive

import (
	"encoding/binary"
	"testing"
)

/*
FuzzApplyMotorInvertRoundTrip checks that when InvertMotor succeeds, applying
the motor and its inverse restores every tested position. Run:

	go test ./pkg/primitive -fuzz=FuzzApplyMotorInvertRoundTrip -fuzztime=30s
*/
func FuzzApplyMotorInvertRoundTrip(f *testing.F) {
	f.Add([]byte{1, 0, 2, 0, 3, 0, 4, 0})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 4 {
			t.Skip()
		}

		scale := binary.LittleEndian.Uint16(data[0:2]) % CoreBits
		translate := binary.LittleEndian.Uint16(data[2:4]) % CoreBits

		invScale, invTranslate, err := InvertMotor(scale, translate)
		if err != nil {
			t.Skip()
		}

		for _, position := range []uint16{0, 1, 42, 1024, 4095, 8190} {
			forward := ApplyMotor(scale, translate, position)
			back := ApplyMotor(invScale, invTranslate, forward)

			if back != position {
				t.Fatalf(
					"round-trip position %d: got %d want %d (scale=%d translate=%d)",
					position,
					back,
					position,
					scale,
					translate,
				)
			}
		}
	})
}

/*
FuzzComposeMotorAssociative samples random affine parameters and verifies that
sequential application matches ComposeMotor for a spread of positions.
*/
func FuzzComposeMotorAssociative(f *testing.F) {
	f.Add([]byte{5, 0, 10, 0, 3, 0, 7, 0})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 8 {
			t.Skip()
		}

		s1 := binary.LittleEndian.Uint16(data[0:2]) % CoreBits
		t1 := binary.LittleEndian.Uint16(data[2:4]) % CoreBits
		s2 := binary.LittleEndian.Uint16(data[4:6]) % CoreBits
		t2 := binary.LittleEndian.Uint16(data[6:8]) % CoreBits

		sComp, tComp := ComposeMotor(s1, t1, s2, t2)

		for _, position := range []uint16{0, 1, 17, 255, 1024, 4096, 8190} {
			mid := ApplyMotor(s1, t1, position)
			seq := ApplyMotor(s2, t2, mid)
			composed := ApplyMotor(sComp, tComp, position)

			if seq != composed {
				t.Fatalf(
					"compose mismatch at position %d: sequential=%d composed=%d",
					position,
					seq,
					composed,
				)
			}
		}
	})
}

/*
FuzzValueMotorNoPanic loads arbitrary bytes into a Value and exercises Motor
and Clamp. Any panic is a fuzz failure.
*/
func FuzzValueMotorNoPanic(f *testing.F) {
	f.Add(make([]byte, ByteSize))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			t.Skip()
		}

		buf := make([]byte, ByteSize)

		for i := range buf {
			buf[i] = data[i%len(data)]
		}

		value := NewValue()

		_, err := value.Write(buf)
		if err != nil {
			t.Fatal(err)
		}

		_, _ = value.Motor()
		value.Clamp()
		_ = value.PopCount()
	})
}
