package operation

import (
	"io"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestBitwiseSliceOps(t *testing.T) {
	gc.Convey("Given two Values with known bit patterns", t, func() {
		a := primitive.NewValue()
		a.Set(0)
		a.Set(2)
		a.Set(4)

		b := primitive.NewValue()
		b.Set(0)
		b.Set(1)
		b.Set(2)

		dst := primitive.NewValue()

		gc.Convey("AND should produce GCD (shared primes only)", func() {
			AND(a[:], b[:], dst[:])
			dst.Clamp()
			gc.So(dst.Has(0), gc.ShouldBeTrue)
			gc.So(dst.Has(2), gc.ShouldBeTrue)
			gc.So(dst.Has(1), gc.ShouldBeFalse)
			gc.So(dst.Has(4), gc.ShouldBeFalse)
			gc.So(dst.PopCount(), gc.ShouldEqual, 2)
		})

		gc.Convey("OR should produce LCM (union of primes)", func() {
			OR(a[:], b[:], dst[:])
			dst.Clamp()
			gc.So(dst.Has(0), gc.ShouldBeTrue)
			gc.So(dst.Has(1), gc.ShouldBeTrue)
			gc.So(dst.Has(2), gc.ShouldBeTrue)
			gc.So(dst.Has(4), gc.ShouldBeTrue)
			gc.So(dst.PopCount(), gc.ShouldEqual, 4)
		})

		gc.Convey("XOR should produce symmetric factorization difference", func() {
			XOR(a[:], b[:], dst[:])
			dst.Clamp()
			gc.So(dst.Has(0), gc.ShouldBeFalse)
			gc.So(dst.Has(2), gc.ShouldBeFalse)
			gc.So(dst.Has(1), gc.ShouldBeTrue)
			gc.So(dst.Has(4), gc.ShouldBeTrue)
			gc.So(dst.PopCount(), gc.ShouldEqual, 2)
		})

		gc.Convey("AndNot should produce unique factor residue", func() {
			AndNot(a[:], b[:], dst[:])
			dst.Clamp()
			gc.So(dst.Has(4), gc.ShouldBeTrue)
			gc.So(dst.Has(0), gc.ShouldBeFalse)
			gc.So(dst.Has(1), gc.ShouldBeFalse)
			gc.So(dst.Has(2), gc.ShouldBeFalse)
			gc.So(dst.PopCount(), gc.ShouldEqual, 1)
		})

		gc.Convey("NOT should complement the field", func() {
			z := primitive.NewValue()
			z.Set(0)
			NOT(z[:], nil, dst[:])
			dst.Clamp()
			gc.So(dst.Has(0), gc.ShouldBeFalse)
			gc.So(dst.Has(1), gc.ShouldBeTrue)
			gc.So(dst.PopCount(), gc.ShouldEqual, primitive.CoreBits-1)
		})

		gc.Convey("NAND should complement AND", func() {
			NAND(a[:], b[:], dst[:])
			dst.Clamp()
			gc.So(dst.Has(0), gc.ShouldBeFalse)
			gc.So(dst.Has(2), gc.ShouldBeFalse)
			gc.So(dst.Has(1), gc.ShouldBeTrue)
			gc.So(dst.Has(4), gc.ShouldBeTrue)
			gc.So(dst.PopCount(), gc.ShouldEqual, primitive.CoreBits-2)
		})

		gc.Convey("NOR should complement OR", func() {
			NOR(a[:], b[:], dst[:])
			dst.Clamp()
			gc.So(dst.Has(0), gc.ShouldBeFalse)
			gc.So(dst.Has(1), gc.ShouldBeFalse)
			gc.So(dst.Has(2), gc.ShouldBeFalse)
			gc.So(dst.Has(4), gc.ShouldBeFalse)
			gc.So(dst.Has(5), gc.ShouldBeTrue)
			gc.So(dst.PopCount(), gc.ShouldEqual, primitive.CoreBits-4)
		})

		gc.Convey("XNOR should complement XOR", func() {
			XNOR(a[:], b[:], dst[:])
			dst.Clamp()
			gc.So(dst.Has(0), gc.ShouldBeTrue)
			gc.So(dst.Has(2), gc.ShouldBeTrue)
			gc.So(dst.Has(1), gc.ShouldBeFalse)
			gc.So(dst.Has(4), gc.ShouldBeFalse)
			gc.So(dst.PopCount(), gc.ShouldEqual, primitive.CoreBits-2)
		})

		gc.Convey("ConverseNonimplication should produce B & ~A", func() {
			ConverseNonimplication(a[:], b[:], dst[:])
			dst.Clamp()
			gc.So(dst.Has(1), gc.ShouldBeTrue)
			gc.So(dst.Has(0), gc.ShouldBeFalse)
			gc.So(dst.Has(4), gc.ShouldBeFalse)
			gc.So(dst.PopCount(), gc.ShouldEqual, 1)
		})
	})
}

func TestSliceSubRange(t *testing.T) {
	gc.Convey("Given two Values, operations on sub-ranges should only affect those words", t, func() {
		a := primitive.NewValue()
		b := primitive.NewValue()
		dst := primitive.NewValue()

		a.Set(0)
		a.Set(64)
		b.Set(0)
		b.Set(64)

		gc.Convey("AND on first word only should miss bit 64", func() {
			AND(a[0:1], b[0:1], dst[0:1])
			gc.So(dst.Has(0), gc.ShouldBeTrue)
			gc.So(dst.Has(64), gc.ShouldBeFalse)
		})
	})
}

func TestRollLeft(t *testing.T) {
	gc.Convey("Given a Value with one bit set", t, func() {
		src := primitive.NewValue()
		src.Set(0)
		dst := primitive.NewValue()

		gc.Convey("RollLeft by 1 should move bit 0 to bit 1", func() {
			RollLeft(src, dst, 1)
			gc.So(dst.Has(0), gc.ShouldBeFalse)
			gc.So(dst.Has(1), gc.ShouldBeTrue)
			gc.So(dst.PopCount(), gc.ShouldEqual, 1)
		})

		gc.Convey("RollLeft by CoreBits should be identity", func() {
			RollLeft(src, dst, primitive.CoreBits)
			gc.So(src.Equal(dst), gc.ShouldBeTrue)
		})

		gc.Convey("RollLeft by 0 should be identity", func() {
			RollLeft(src, dst, 0)
			gc.So(src.Equal(dst), gc.ShouldBeTrue)
		})

		gc.Convey("RollLeft should wrap bit CoreBits-1 back to bit 0", func() {
			wrap := primitive.NewValue()
			wrap.Set(primitive.CoreBits - 1)
			RollLeft(wrap, dst, 1)
			gc.So(dst.Has(0), gc.ShouldBeTrue)
			gc.So(dst.PopCount(), gc.ShouldEqual, 1)
		})

		gc.Convey("RollLeft should handle cross-word boundary shifts", func() {
			cross := primitive.NewValue()
			cross.Set(63)
			RollLeft(cross, dst, 1)
			gc.So(dst.Has(63), gc.ShouldBeFalse)
			gc.So(dst.Has(64), gc.ShouldBeTrue)
			gc.So(dst.PopCount(), gc.ShouldEqual, 1)
		})

		gc.Convey("RollLeft with negative shift should work as reverse rotation", func() {
			RollLeft(src, dst, -1)
			gc.So(dst.Has(primitive.CoreBits-1), gc.ShouldBeTrue)
			gc.So(dst.PopCount(), gc.ShouldEqual, 1)
		})
	})
}

func TestBitwisePipeline(t *testing.T) {
	gc.Convey("Given a Bitwise pipeline stage", t, func() {
		stage := NewBitwise(OR)

		instrOR := primitive.NewValue()
		instrOR.Set(14)

		a := primitive.NewValue()
		a.Set(0)
		a.Set(2)

		b := primitive.NewValue()
		b.Set(1)
		b.Set(3)

		bufInstr := make([]byte, primitive.ByteSize)
		bufA := make([]byte, primitive.ByteSize)
		bufB := make([]byte, primitive.ByteSize)
		instrOR.Read(bufInstr)
		a.Read(bufA)
		b.Read(bufB)

		gc.Convey("Three-write sequence (instr + 2 operands) should produce operation result", func() {
			_, err := stage.Write(bufInstr)
			gc.So(err, gc.ShouldBeNil)

			_, err = stage.Write(bufA)
			gc.So(err, gc.ShouldBeNil)

			_, err = stage.Write(bufB)
			gc.So(err, gc.ShouldBeNil)

			result := make([]byte, primitive.ByteSize)
			n, readErr := stage.Read(result)
			gc.So(n, gc.ShouldEqual, primitive.ByteSize)
			gc.So(readErr, gc.ShouldBeNil)

			out := primitive.NewValue()
			out.Write(result)

			gc.So(out.Has(0), gc.ShouldBeTrue)
			gc.So(out.Has(1), gc.ShouldBeTrue)
			gc.So(out.Has(2), gc.ShouldBeTrue)
			gc.So(out.Has(3), gc.ShouldBeTrue)
			gc.So(out.PopCount(), gc.ShouldEqual, 4)
		})

		gc.Convey("Read before 3 writes should return io.EOF", func() {
			n, err := stage.Read(make([]byte, primitive.ByteSize))
			gc.So(n, gc.ShouldEqual, 0)
			gc.So(err, gc.ShouldEqual, io.EOF)
		})

		gc.Convey("Read with undersized buffer should return ErrShortValue", func() {
			stage.Write(bufInstr)
			stage.Write(bufA)
			stage.Write(bufB)
			n, err := stage.Read(make([]byte, primitive.ByteSize-1))
			gc.So(n, gc.ShouldEqual, 0)
			gc.So(err, gc.ShouldEqual, primitive.ErrShortValue)
		})

		gc.Convey("Write with undersized buffer should return ErrShortValue", func() {
			n, err := stage.Write(make([]byte, primitive.ByteSize-1))
			gc.So(n, gc.ShouldEqual, 0)
			gc.So(err, gc.ShouldEqual, primitive.ErrShortValue)
		})

		gc.Convey("Close should reset the state", func() {
			stage.Write(bufInstr)
			stage.Write(bufA)
			err := stage.Close()
			gc.So(err, gc.ShouldBeNil)

			n, readErr := stage.Read(make([]byte, primitive.ByteSize))
			gc.So(n, gc.ShouldEqual, 0)
			gc.So(readErr, gc.ShouldEqual, io.EOF)
		})
	})
}

func TestBitwiseAccumulation(t *testing.T) {
	gc.Convey("Given a Bitwise stage selecting AND via instruction write", t, func() {
		stage := NewBitwise(OR)

		instrAND := primitive.NewValue()
		instrAND.Set(8)

		val1 := primitive.NewValue()
		val1.Set(10)
		val1.Set(20)
		val1.Set(30)

		val2 := primitive.NewValue()
		val2.Set(10)
		val2.Set(20)

		bufInstr := make([]byte, primitive.ByteSize)
		buf1 := make([]byte, primitive.ByteSize)
		buf2 := make([]byte, primitive.ByteSize)
		instrAND.Read(bufInstr)
		val1.Read(buf1)
		val2.Read(buf2)

		stage.Write(bufInstr)
		stage.Write(buf1)
		stage.Write(buf2)

		result := make([]byte, primitive.ByteSize)
		n, err := stage.Read(result)
		gc.So(n, gc.ShouldEqual, primitive.ByteSize)
		gc.So(err, gc.ShouldBeNil)

		out := primitive.NewValue()
		out.Write(result)

		gc.So(out.Has(10), gc.ShouldBeTrue)
		gc.So(out.Has(20), gc.ShouldBeTrue)
		gc.So(out.Has(30), gc.ShouldBeFalse)
		gc.So(out.PopCount(), gc.ShouldEqual, 2)
	})
}

func TestBitwiseOpAccessor(t *testing.T) {
	gc.Convey("Given a Bitwise stage after the instruction write", t, func() {
		stage := NewBitwise(AND)

		instrOR := primitive.NewValue()
		instrOR.Set(14)

		bufInstr := make([]byte, primitive.ByteSize)
		_, readErr := instrOR.Read(bufInstr)
		gc.So(readErr, gc.ShouldEqual, io.EOF)

		_, writeErr := stage.Write(bufInstr)
		gc.So(writeErr, gc.ShouldEqual, nil)
		gc.So(stage.Op(), gc.ShouldEqual, OR)
	})
}

func TestOpShortSliceSlowPaths(t *testing.T) {
	gc.Convey("Given uint64 slices shorter than primitive.Words", t, func() {
		a := []uint64{0xAAAAAAAAAAAAAAAA, 0x5555555555555555}
		b := []uint64{0x5555555555555555, 0xAAAAAAAAAAAAAAAA}
		dst := make([]uint64, 2)

		gc.Convey("OR should use the scalar loop path", func() {
			OR(a[:1], b[:1], dst[:1])
			gc.So(dst[0], gc.ShouldEqual, a[0]|b[0])
		})

		gc.Convey("AND should use the scalar loop path", func() {
			dst[0] = 0
			AND(a[:1], b[:1], dst[:1])
			gc.So(dst[0], gc.ShouldEqual, a[0]&b[0])
		})

		gc.Convey("XOR should use the scalar loop path", func() {
			XOR(a[:1], b[:1], dst[:1])
			gc.So(dst[0], gc.ShouldEqual, a[0]^b[0])
		})

		gc.Convey("AndNot should use the scalar loop path", func() {
			AndNot(a[:1], b[:1], dst[:1])
			gc.So(dst[0], gc.ShouldEqual, a[0]&^b[0])
		})

		gc.Convey("NOT should use the scalar loop path", func() {
			NOT(a[:1], nil, dst[:1])
			gc.So(dst[0], gc.ShouldEqual, ^a[0])
		})

		gc.Convey("NAND should use the scalar loop path", func() {
			NAND(a[:1], b[:1], dst[:1])
			gc.So(dst[0], gc.ShouldEqual, ^(a[0] & b[0]))
		})

		gc.Convey("NOR should use the scalar loop path", func() {
			NOR(a[:1], b[:1], dst[:1])
			gc.So(dst[0], gc.ShouldEqual, ^(a[0] | b[0]))
		})

		gc.Convey("XNOR should use the scalar loop path", func() {
			XNOR(a[:1], b[:1], dst[:1])
			gc.So(dst[0], gc.ShouldEqual, ^(a[0] ^ b[0]))
		})

		gc.Convey("ConverseNonimplication should use the scalar loop path", func() {
			ConverseNonimplication(a[:1], b[:1], dst[:1])
			gc.So(dst[0], gc.ShouldEqual, b[0]&^a[0])
		})

		gc.Convey("zero-length dst should early-return for binary ops", func() {
			OR(a[:0], b[:0], dst[:0])
			AND(a[:0], b[:0], dst[:0])
			XOR(a[:0], b[:0], dst[:0])
			AndNot(a[:0], b[:0], dst[:0])
			NAND(a[:0], b[:0], dst[:0])
			NOR(a[:0], b[:0], dst[:0])
			XNOR(a[:0], b[:0], dst[:0])
			ConverseNonimplication(a[:0], b[:0], dst[:0])
			NOT(a[:0], nil, dst[:0])
		})
	})
}

func TestRollLeftBranchCoverage(t *testing.T) {
	gc.Convey("Given RollLeft edge shifts", t, func() {
		gc.Convey("shift 0 with src == dst should skip the copy", func() {
			v := primitive.NewValue()
			v.Set(42)
			RollLeft(v, v, 0)
			gc.So(v.Has(42), gc.ShouldBeTrue)
			gc.So(v.PopCount(), gc.ShouldEqual, 1)
		})

		gc.Convey("shift multiple of 64 should use bitShiftL == 0 left path", func() {
			src := primitive.NewValue()
			src.Set(0)
			dst := primitive.NewValue()
			RollLeft(src, dst, 64)
			gc.So(dst.Has(64), gc.ShouldBeTrue)
			gc.So(dst.PopCount(), gc.ShouldEqual, 1)
		})

		gc.Convey("shift with r divisible by 64 should use bitShiftR == 0 right-merge path", func() {
			src := primitive.NewValue()
			src.Set(0)
			dst := primitive.NewValue()
			RollLeft(src, dst, 63)
			gc.So(dst.Has(63), gc.ShouldBeTrue)
			gc.So(dst.PopCount(), gc.ShouldEqual, 1)
		})
	})
}

func TestSelectOpInstructionBranches(t *testing.T) {
	gc.Convey("Given Bitwise first-write instruction selection", t, func() {
		a := primitive.NewValue()
		a.Set(5)
		a.Set(11)

		b := primitive.NewValue()
		b.Set(11)

		bufA := make([]byte, primitive.ByteSize)
		bufB := make([]byte, primitive.ByteSize)
		result := make([]byte, primitive.ByteSize)

		_, err := a.Read(bufA)
		gc.So(err, gc.ShouldEqual, io.EOF)
		_, err = b.Read(bufB)
		gc.So(err, gc.ShouldEqual, io.EOF)

		gc.Convey("NOT instruction (bit 3) should ignore the second operand", func() {
			stage := NewBitwise(OR)
			instr := primitive.NewValue()
			instr.Set(3)

			bufInstr := make([]byte, primitive.ByteSize)
			_, err := instr.Read(bufInstr)
			gc.So(err, gc.ShouldEqual, io.EOF)

			stage.Write(bufInstr)
			stage.Write(bufA)
			stage.Write(bufB)

			n, readErr := stage.Read(result)
			gc.So(readErr, gc.ShouldEqual, nil)
			gc.So(n, gc.ShouldEqual, primitive.ByteSize)

			out := primitive.NewValue()
			out.Write(result)

			operand := primitive.NewValue()
			operand.Set(5)
			operand.Set(11)

			expect := primitive.NewValue()
			NOT(operand[:], nil, expect[:])
			expect.Clamp()

			gc.So(out.Equal(expect), gc.ShouldBeTrue)
		})

		gc.Convey("Identity instruction (bit 12) should return the first operand", func() {
			stage := NewBitwise(OR)
			instr := primitive.NewValue()
			instr.Set(12)

			bufInstr := make([]byte, primitive.ByteSize)
			_, err := instr.Read(bufInstr)
			gc.So(err, gc.ShouldEqual, io.EOF)

			stage.Write(bufInstr)
			stage.Write(bufA)
			stage.Write(bufB)

			n, readErr := stage.Read(result)
			gc.So(readErr, gc.ShouldBeNil)
			gc.So(n, gc.ShouldEqual, primitive.ByteSize)

			out := primitive.NewValue()
			out.Write(result)

			gc.So(out.Equal(a), gc.ShouldBeTrue)
		})

		gc.Convey("Unrecognized instruction should default to AND", func() {
			stage := NewBitwise(OR)
			bufInstr := make([]byte, primitive.ByteSize)

			stage.Write(bufInstr)
			stage.Write(bufA)
			stage.Write(bufB)

			n, readErr := stage.Read(result)
			gc.So(readErr, gc.ShouldBeNil)
			gc.So(n, gc.ShouldEqual, primitive.ByteSize)

			out := primitive.NewValue()
			out.Write(result)

			expect := primitive.NewValue()
			AND(a[:], b[:], expect[:])
			expect.Clamp()

			gc.So(out.Equal(expect), gc.ShouldBeTrue)
		})
	})
}

func TestBitwiseInstructionSelection(t *testing.T) {
	gc.Convey("Given different instruction Values, the operation should be selected correctly", t, func() {
		stage := NewBitwise(OR)

		a := primitive.NewValue()
		a.Set(4)
		a.Set(9)

		b := primitive.NewValue()
		b.Set(9)

		bufA := make([]byte, primitive.ByteSize)
		bufB := make([]byte, primitive.ByteSize)
		result := make([]byte, primitive.ByteSize)

		a.Read(bufA)
		b.Read(bufB)

		gc.Convey("Instruction for XOR should compute symmetric difference", func() {
			instrXOR := primitive.NewValue()
			instrXOR.Set(6)
			bufInstr := make([]byte, primitive.ByteSize)
			instrXOR.Read(bufInstr)

			stage.Write(bufInstr)
			stage.Write(bufA)
			stage.Write(bufB)

			n, err := stage.Read(result)
			gc.So(n, gc.ShouldEqual, primitive.ByteSize)
			gc.So(err, gc.ShouldBeNil)

			out := primitive.NewValue()
			out.Write(result)
			gc.So(out.Has(4), gc.ShouldBeTrue)
			gc.So(out.Has(9), gc.ShouldBeFalse)
			gc.So(out.PopCount(), gc.ShouldEqual, 1)
		})

		gc.Convey("Instruction for AND should compute GCD", func() {
			stage.Close()

			instrAND := primitive.NewValue()
			instrAND.Set(8)
			bufInstr := make([]byte, primitive.ByteSize)
			instrAND.Read(bufInstr)

			stage.Write(bufInstr)
			stage.Write(bufA)
			stage.Write(bufB)

			n, err := stage.Read(result)
			gc.So(n, gc.ShouldEqual, primitive.ByteSize)
			gc.So(err, gc.ShouldBeNil)

			out := primitive.NewValue()
			out.Write(result)
			gc.So(out.Has(9), gc.ShouldBeTrue)
			gc.So(out.Has(4), gc.ShouldBeFalse)
			gc.So(out.PopCount(), gc.ShouldEqual, 1)
		})

		gc.Convey("Instruction for AndNot should compute material nonimplication", func() {
			stage.Close()

			instrAndNot := primitive.NewValue()
			instrAndNot.Set(4)
			bufInstr := make([]byte, primitive.ByteSize)
			instrAndNot.Read(bufInstr)

			stage.Write(bufInstr)
			stage.Write(bufA)
			stage.Write(bufB)

			n, err := stage.Read(result)
			gc.So(n, gc.ShouldEqual, primitive.ByteSize)
			gc.So(err, gc.ShouldBeNil)

			out := primitive.NewValue()
			out.Write(result)
			gc.So(out.Has(4), gc.ShouldBeTrue)
			gc.So(out.Has(9), gc.ShouldBeFalse)
			gc.So(out.PopCount(), gc.ShouldEqual, 1)
		})
	})
}

func BenchmarkAND(b *testing.B) {
	a := primitive.NewValue()
	other := primitive.NewValue()
	dst := primitive.NewValue()

	for i := 0; i < 50; i++ {
		a.Set(i * 37 % primitive.CoreBits)
		other.Set(i * 53 % primitive.CoreBits)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		AND(a[:], other[:], dst[:])
	}
}

func BenchmarkOR(b *testing.B) {
	a := primitive.NewValue()
	other := primitive.NewValue()
	dst := primitive.NewValue()

	for i := 0; i < 50; i++ {
		a.Set(i * 37 % primitive.CoreBits)
		other.Set(i * 53 % primitive.CoreBits)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		OR(a[:], other[:], dst[:])
	}
}

func BenchmarkXOR(b *testing.B) {
	a := primitive.NewValue()
	other := primitive.NewValue()
	dst := primitive.NewValue()

	for i := 0; i < 50; i++ {
		a.Set(i * 37 % primitive.CoreBits)
		other.Set(i * 53 % primitive.CoreBits)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		XOR(a[:], other[:], dst[:])
	}
}

func BenchmarkNOT(b *testing.B) {
	a := primitive.NewValue()
	dst := primitive.NewValue()

	for i := 0; i < 50; i++ {
		a.Set(i * 37 % primitive.CoreBits)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		NOT(a[:], nil, dst[:])
	}
}

func BenchmarkRollLeft(b *testing.B) {
	src := primitive.NewValue()
	dst := primitive.NewValue()

	for i := 0; i < 50; i++ {
		src.Set(i * 37 % primitive.CoreBits)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		RollLeft(src, dst, 7)
	}
}

func BenchmarkBitwisePipeline(b *testing.B) {
	instrOR := primitive.NewValue()
	instrOR.Set(14)

	va := primitive.NewValue()
	vb := primitive.NewValue()
	stage := NewBitwise(OR)

	for i := 0; i < 50; i++ {
		va.Set(i * 37 % primitive.CoreBits)
		vb.Set(i * 53 % primitive.CoreBits)
	}

	bufInstr := make([]byte, primitive.ByteSize)
	bufA := make([]byte, primitive.ByteSize)
	bufB := make([]byte, primitive.ByteSize)
	instrOR.Read(bufInstr)
	va.Read(bufA)
	vb.Read(bufB)

	result := make([]byte, primitive.ByteSize)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		stage.Close()
		stage.Write(bufInstr)
		stage.Write(bufA)
		stage.Write(bufB)
		stage.Read(result)
	}
}
