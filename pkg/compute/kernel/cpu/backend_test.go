package cpu

import (
	"errors"
	"io"
	"math/bits"
	"runtime"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestAvailable(t *testing.T) {
	Convey("Available reports logical CPU count", t, func() {
		So(Available(), ShouldEqual, runtime.NumCPU())
		So(Available(), ShouldBeGreaterThan, 0)
	})
}

func TestNewBackend(t *testing.T) {
	Convey("NewBackend returns a non-nil Backend", t, func() {
		b := NewBackend()
		So(b, ShouldNotBeNil)
	})
}

func TestRead(t *testing.T) {
	Convey("Read returns EOF when the pipe has no data", t, func() {
		b := NewBackend()
		n, err := b.Read(nil)
		So(n, ShouldEqual, 0)
		So(errors.Is(err, io.EOF), ShouldBeTrue)

		buf := make([]byte, 16)
		n, err = b.Read(buf)
		So(errors.Is(err, io.EOF), ShouldBeTrue)
		So(n, ShouldEqual, 0)
	})
}

func TestWrite(t *testing.T) {
	Convey("Write forwards non-empty payloads to the ring buffer", t, func() {
		b := NewBackend()
		n, err := b.Write(nil)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, 0)

		buf := make([]byte, 16)
		n, err = b.Write(buf)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, len(buf))
	})
}

func TestClose(t *testing.T) {
	Convey("Close always succeeds", t, func() {
		b := NewBackend()
		So(b.Close(), ShouldBeNil)
		So(b.Close(), ShouldBeNil)
	})
}

// accumulateRef mirrors Backend.Accumulate for one Value pair (tests only).
func accumulateRef(incoming, state *primitive.Value) {
	iw := (*[primitive.Words]uint64)(unsafe.Pointer(incoming))
	s, t := deriveMotor(iw)
	var mapped [primitive.Words]uint64
	sw := (*[primitive.Words]uint64)(unsafe.Pointer(state))
	applyMotor(sw, &mapped, s, t)
	for i := uint32(0); i < primitive.Words; i += 8 {
		state[i+0] = mapped[i+0] | incoming[i+0]
		state[i+1] = mapped[i+1] | incoming[i+1]
		state[i+2] = mapped[i+2] | incoming[i+2]
		state[i+3] = mapped[i+3] | incoming[i+3]
		state[i+4] = mapped[i+4] | incoming[i+4]
		state[i+5] = mapped[i+5] | incoming[i+5]
		state[i+6] = mapped[i+6] | incoming[i+6]
		state[i+7] = mapped[i+7] | incoming[i+7]
	}
	state[primitive.Words-1] &= primitive.LastMask
}

func TestAccumulate(t *testing.T) {
	Convey("Accumulate", t, func() {
		Convey("numValues 0 is a no-op and returns nil", func() {
			b := NewBackend()
			So(b.Accumulate(nil, nil, 0), ShouldBeNil)
		})

		Convey("matches reference for a single Value", func() {
			b := NewBackend()
			inc := randomValue(42)
			st := randomValue(7)
			want := *st
			accumulateRef(inc, &want)

			st2 := *st
			So(b.Accumulate(
				unsafe.Pointer(inc),
				unsafe.Pointer(&st2),
				1,
			), ShouldBeNil)
			So(st2, ShouldResemble, want)
		})

		Convey("matches reference for multiple Values", func() {
			b := NewBackend()
			const n = 8
			inc := make([]primitive.Value, n)
			st := make([]primitive.Value, n)
			want := make([]primitive.Value, n)
			for i := range n {
				inc[i] = *randomValue(uint64(100 + i))
				st[i] = *randomValue(uint64(200 + i))
				want[i] = st[i]
				accumulateRef(&inc[i], &want[i])
			}
			So(b.Accumulate(
				unsafe.Pointer(&inc[0]),
				unsafe.Pointer(&st[0]),
				n,
			), ShouldBeNil)
			for i := range n {
				So(st[i], ShouldResemble, want[i])
			}
		})

		Convey("all-zero Values stay consistent with reference", func() {
			b := NewBackend()
			inc := primitive.NewValue()
			st := primitive.NewValue()
			want := *st
			accumulateRef(inc, &want)
			st2 := *st
			So(b.Accumulate(unsafe.Pointer(inc), unsafe.Pointer(&st2), 1), ShouldBeNil)
			So(st2, ShouldResemble, want)
		})
	})
}

func randomValue(seed uint64) *primitive.Value {
	v := primitive.NewValue()
	x := seed
	for w := range primitive.Words {
		x ^= x + 0x9e3779b97f4a7c15
		v[w] = x
	}
	v[primitive.Words-1] &= primitive.LastMask
	return v
}

// truth4 is the boolean result for one bit position, matching the 4-bit
// instruction layout used by UniversalBitwise (indices: 00→bit0, 01→bit1, 10→bit2, 11→bit3).
func truth4(instr, a, b uint8) uint64 {
	m0 := (instr >> 0) & 1
	m1 := (instr >> 1) & 1
	m2 := (instr >> 2) & 1
	m3 := (instr >> 3) & 1
	av := a & 1
	bv := b & 1
	r := (m0 & (1 - av) & (1 - bv)) |
		(m1 & (1 - av) & bv) |
		(m2 & av & (1 - bv)) |
		(m3 & av & bv)
	return uint64(r)
}

func TestUniversalBitwise(t *testing.T) {
	Convey("UniversalBitwise", t, func() {
		Convey("numValues 0 returns nil", func() {
			b := NewBackend()
			So(b.UniversalBitwise(0, nil, nil, nil, 0), ShouldBeNil)
		})

		Convey("all 16 truth tables on the first lane (bit index 0)", func() {
			b := NewBackend()
			const (
				r0w = 0
				r2w = primitive.OperandStart >> 6
				r2s = primitive.OperandStart & 63
				r3w = 0
				r3s = 0
			)
			for instr := uint8(0); instr < 16; instr++ {
				for ab := range 4 {
					aBit := uint8(ab >> 1)
					bBit := uint8(ab & 1)
					var A, B, dst primitive.Value
					if aBit != 0 {
						A[r0w] |= 1
					}
					if bBit != 0 {
						B[r2w] |= 1 << r2s
					}
					// Poison state vector region to ensure kernel clears then writes.
					for i := range 5 {
						dst[r3w+i] = ^uint64(0)
					}
					So(b.UniversalBitwise(
						instr,
						unsafe.Pointer(&A),
						unsafe.Pointer(&B),
						unsafe.Pointer(&dst),
						1,
					), ShouldBeNil)
					want := truth4(instr, aBit, bBit)
					got := (dst[r3w] >> r3s) & 1
					So(got, ShouldEqual, want)
				}
			}
		})

		Convey("known operations on first bit", func() {
			b := NewBackend()
			cases := []struct {
				instr      uint8
				aSet, bSet bool
				want       uint64
			}{
				{0b1000, true, true, 1},
				{0b1000, true, false, 0},
				{0b1110, false, false, 0},
				{0b1110, true, false, 1},
				{0b0110, true, true, 0},
				{0b0110, true, false, 1},
				{0b0001, false, false, 1},
				{0, true, true, 0},
				{0b1111, false, false, 1},
			}
			for _, tc := range cases {
				tc := tc
				var A, B, dst primitive.Value
				if tc.aSet {
					A[0] |= 1
				}
				if tc.bSet {
					B[primitive.OperandStart>>6] |= 1 << (primitive.OperandStart & 63)
				}
				So(b.UniversalBitwise(
					tc.instr,
					unsafe.Pointer(&A),
					unsafe.Pointer(&B),
					unsafe.Pointer(&dst),
					1,
				), ShouldBeNil)
				got := (dst[0] >> 0) & 1
				So(got, ShouldEqual, tc.want)
			}
		})

		Convey("last bit of the 257-bit regions (lane i=4)", func() {
			b := NewBackend()
			// j=256: A in word 4 bit 0; B at bit 261+256=517 = word 8 bit 5; dst accum bit 518+256=774 = word 12 bit 6.
			const j = 256
			aWord := j / 64
			aBit := j % 64
			bPos := primitive.OperandStart + j
			bWord := bPos / 64
			bBit := bPos % 64
			dPos := j
			dWord := dPos / 64
			dBit := dPos % 64

			for instr := uint8(0); instr < 16; instr++ {
				for ab := range 4 {
					aBitVal := uint8(ab >> 1)
					bBitVal := uint8(ab & 1)
					var A, B, dst primitive.Value
					if aBitVal != 0 {
						A[aWord] |= 1 << aBit
					}
					if bBitVal != 0 {
						B[bWord] |= 1 << bBit
					}
					for i := range 5 {
						dst[i] = ^uint64(0)
					}
					So(b.UniversalBitwise(
						instr,
						unsafe.Pointer(&A),
						unsafe.Pointer(&B),
						unsafe.Pointer(&dst),
						1,
					), ShouldBeNil)
					want := truth4(instr, aBitVal, bBitVal)
					got := (dst[dWord] >> dBit) & 1
					So(got, ShouldEqual, want)
				}
			}
		})

		Convey("multiple Values are independent", func() {
			b := NewBackend()
			const n = 4
			a := make([]primitive.Value, n)
			bv := make([]primitive.Value, n)
			dst := make([]primitive.Value, n)
			for i := range n {
				if i%2 == 0 {
					a[i][0] |= 1
				}
				bv[i][primitive.OperandStart>>6] |= 1 << (primitive.OperandStart & 63)
			}
			So(b.UniversalBitwise(
				0b1000, // AND
				unsafe.Pointer(&a[0]),
				unsafe.Pointer(&bv[0]),
				unsafe.Pointer(&dst[0]),
				n,
			), ShouldBeNil)
			for i := range n {
				bit := (dst[i][0] >> 0) & 1
				if i%2 == 0 {
					So(bit, ShouldEqual, 1)
				} else {
					So(bit, ShouldEqual, 0)
				}
			}
		})

		Convey("non-data words in dst are preserved when clearing region 0", func() {
			b := NewBackend()
			var A, B, dst primitive.Value
			A[0] = 1
			B[primitive.OperandStart>>6] |= 1 << (primitive.OperandStart & 63)
			// Set words outside the 5-word data window to a pattern.
			dst[5] = 0xdeadbeefcafebabe
			dst[primitive.StateStart>>6+5] = 0x1122334455667788
			So(b.UniversalBitwise(
				0b1110, // OR
				unsafe.Pointer(&A),
				unsafe.Pointer(&B),
				unsafe.Pointer(&dst),
				1,
			), ShouldBeNil)
			So(dst[5], ShouldEqual, uint64(0xdeadbeefcafebabe))
			So(dst[primitive.StateStart>>6+5], ShouldEqual, uint64(0x1122334455667788))
		})
	})
}

// buildPressureFrame sets data bit 0, instruction, and operand bit 0 (structural pressure).
func buildPressureFrame(instr uint8, aBit, bBit uint8) ([]byte, error) {
	buf := make([]byte, primitive.ByteSize)
	var v primitive.Value
	if aBit != 0 {
		v[0] |= 1
	}
	WriteRegion(&v, RegionInstruction, uint64(instr))
	if bBit != 0 {
		v[primitive.OperandStart>>6] |= 1 << (primitive.OperandStart & 63)
	}
	return buf, primitive.ValueToBytes(&v, buf)
}

// writeRef mirrors Backend.Write kernel steps (Accumulate + optional UniversalBitwise)
// without touching the ring buffer; used to build expected frames in tests.
func writeRef(b *Backend, p []byte) error {
	if len(p) < primitive.ByteSize {
		return nil
	}
	incoming := primitive.BytesToValue(p)
	frameInPlace := uintptr(unsafe.Pointer(incoming)) == uintptr(unsafe.Pointer(&p[0]))
	if err := b.Accumulate(unsafe.Pointer(incoming), unsafe.Pointer(incoming), 1); err != nil {
		return err
	}
	if incoming[primitive.OperandStart>>6] != 0 {
		instr := uint8(ReadRegion(incoming, RegionInstruction) & 0xF)
		if err := b.UniversalBitwise(
			instr,
			unsafe.Pointer(incoming),
			unsafe.Pointer(incoming),
			unsafe.Pointer(incoming),
			1,
		); err != nil {
			return err
		}
		if err := b.UpdateStateVector(
			unsafe.Pointer(incoming),
			1,
		); err != nil {
			return err
		}
		if err := b.ClearOperand(
			unsafe.Pointer(incoming),
			1,
		); err != nil {
			return err
		}
	}
	if !frameInPlace {
		return primitive.ValueToBytes(incoming, p)
	}
	return nil
}

func TestBackendWriteStructuralPressure(t *testing.T) {
	Convey("Write applies Accumulate then UniversalBitwise when operand word is non-zero after spin", t, func() {
		for instr := uint8(0); instr < 16; instr++ {
			for ab := range 4 {
				aBit := uint8(ab >> 1)
				bBit := uint8(ab & 1)
				buf, err := buildPressureFrame(instr, aBit, bBit)
				So(err, ShouldBeNil)

				want := append([]byte(nil), buf...)
				ref := NewBackend()
				So(writeRef(ref, want), ShouldBeNil)

				b := NewBackend()
				n, err := b.Write(buf)
				So(err, ShouldBeNil)
				So(n, ShouldEqual, len(buf))
				So(buf, ShouldResemble, want)
			}
		}
	})
}

func TestBackendWriteNoPressurePassthrough(t *testing.T) {
	Convey("Write matches kernel reference (spin then optional ALU)", t, func() {
		b := NewBackend()
		buf := make([]byte, primitive.ByteSize)
		buf[0] = 0x42
		buf[127] = 0xee
		want := append([]byte(nil), buf...)

		So(writeRef(b, want), ShouldBeNil)

		b2 := NewBackend()
		n, err := b2.Write(buf)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, len(buf))
		So(buf, ShouldResemble, want)
	})
}

func TestBackendWriteReadRoundTrip(t *testing.T) {
	Convey("Write pushes the post-spin frame to the ring; Read returns that same frame", t, func() {
		b := NewBackend()
		buf := make([]byte, primitive.ByteSize)
		for i := range 32 {
			buf[i] = byte(i * 17)
		}

		n, err := b.Write(buf)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, len(buf))

		out := make([]byte, primitive.ByteSize)
		m, err := io.ReadFull(b, out)
		So(err, ShouldBeNil)
		So(m, ShouldEqual, len(buf))
		So(out, ShouldResemble, buf)
	})
}

func TestBackendWriteReadRoundTripWithPressure(t *testing.T) {
	Convey("Write mutates the frame then Read returns the mutated bytes", t, func() {
		buf, err := buildPressureFrame(0b1110, 1, 0) // OR: 1 OR 0 -> 1
		So(err, ShouldBeNil)
		orig := append([]byte(nil), buf...)

		want := append([]byte(nil), buf...)
		ref := NewBackend()
		So(writeRef(ref, want), ShouldBeNil)

		b := NewBackend()
		_, err = b.Write(buf)
		So(err, ShouldBeNil)
		So(buf, ShouldResemble, want)
		So(buf, ShouldNotResemble, orig)

		out := make([]byte, primitive.ByteSize)
		_, err = io.ReadFull(b, out)
		So(err, ShouldBeNil)
		So(out, ShouldResemble, buf)
	})
}

func TestBackendWriteUnalignedFrame(t *testing.T) {
	Convey("unaligned backing slice: kernel result is synced with ValueToBytes before Write", t, func() {
		base := make([]byte, primitive.ByteSize+8)
		var p []byte
		for i := range 8 {
			if uintptr(unsafe.Pointer(&base[i]))&7 != 0 {
				p = base[i : i+primitive.ByteSize]
				break
			}
		}
		if p == nil {
			t.Fatal("expected an unaligned sub-slice")
		}

		buf, err := buildPressureFrame(0b0110, 1, 0) // XOR-ish lane
		So(err, ShouldBeNil)
		copy(p, buf)

		want := append([]byte(nil), p...)
		ref := NewBackend()
		So(writeRef(ref, want), ShouldBeNil)

		b := NewBackend()
		_, err = b.Write(p)
		So(err, ShouldBeNil)

		So(p, ShouldResemble, want)
	})
}

func BenchmarkAvailable(b *testing.B) {
	for b.Loop() {
		_ = Available()
	}
}

func BenchmarkBackend_Read(b *testing.B) {
	be := NewBackend()
	buf := make([]byte, 1024)
	b.ResetTimer()
	for b.Loop() {
		_, _ = be.Read(buf)
	}
}

func BenchmarkBackend_Write(b *testing.B) {
	be := NewBackend()
	buf := make([]byte, primitive.ByteSize)
	drain := make([]byte, primitive.ByteSize)
	b.ResetTimer()
	for b.Loop() {
		_, _ = be.Write(buf)
		// Ring capacity matches one frame; without a read the next Write blocks forever.
		_, _ = io.ReadFull(be, drain)
	}
}

func BenchmarkBackend_Close(b *testing.B) {
	be := NewBackend()
	b.ResetTimer()
	for b.Loop() {
		_ = be.Close()
	}
}

func BenchmarkBackend_Accumulate(b *testing.B) {
	be := NewBackend()
	const n = 64
	inc := make([]primitive.Value, n)
	st := make([]primitive.Value, n)
	for i := range n {
		inc[i] = *randomValue(uint64(i * 9973))
		st[i] = *randomValue(uint64(i*8191 + 1))
	}
	b.ResetTimer()
	for b.Loop() {
		_ = be.Accumulate(unsafe.Pointer(&inc[0]), unsafe.Pointer(&st[0]), n)
	}
}

func BenchmarkBackend_UniversalBitwise(b *testing.B) {
	be := NewBackend()
	const n = 64
	a := make([]primitive.Value, n)
	bv := make([]primitive.Value, n)
	dst := make([]primitive.Value, n)
	for i := range n {
		a[i][0] = uint64(i + 1)
		bv[i][4] = uint64(bits.Reverse64(uint64(i)))
	}
	b.ResetTimer()
	for b.Loop() {
		_ = be.UniversalBitwise(0b1110, unsafe.Pointer(&a[0]), unsafe.Pointer(&bv[0]), unsafe.Pointer(&dst[0]), n)
	}
}
