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

func TestWriteWaitsForFullBatch(t *testing.T) {
	Convey("Write does not emit until the resident batch is full", t, func() {
		b := NewBackend(BackendWithBatchCap(2))
		value := primitive.NewValue()
		value.SetTokenID(0, primitive.Tokenize('a', 0))
		value.SetValueID(1)
		value[primitive.StateSlotIndex] = 1

		frame := make([]byte, primitive.ByteSize)
		So(primitive.ValueToBytes(value, frame), ShouldBeNil)

		n, err := b.Write(frame)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, primitive.ByteSize)

		out := make([]byte, primitive.ByteSize)
		n, err = b.Read(out)
		So(n, ShouldEqual, 0)
		So(errors.Is(err, io.EOF), ShouldBeTrue)
	})
}

func TestWriteEmitsStrongestCancellationOnly(t *testing.T) {
	Convey("When a batch is full, Write splits into linked shared-label + remainders", t, func() {
		b := NewBackend(BackendWithBatchCap(2))

		left := primitive.NewValue()
		left.SetTokenID(0, primitive.Tokenize('L', 0))
		left.SetTokenID(1, primitive.Tokenize('x', 1))
		left.SetTokenID(2, primitive.Tokenize('y', 2))
		left.SetTokenID(3, primitive.Tokenize('R', 3))
		left.SetValueID(10)
		left[primitive.StateSlotIndex] = 1

		right := primitive.NewValue()
		right.SetTokenID(0, primitive.Tokenize('Q', 0))
		right.SetTokenID(1, primitive.Tokenize('x', 1))
		right.SetTokenID(2, primitive.Tokenize('y', 2))
		right.SetTokenID(3, primitive.Tokenize('Z', 3))
		right.SetValueID(20)
		right[primitive.StateSlotIndex] = 1

		leftFrame := make([]byte, primitive.ByteSize)
		rightFrame := make([]byte, primitive.ByteSize)
		So(primitive.ValueToBytes(left, leftFrame), ShouldBeNil)
		So(primitive.ValueToBytes(right, rightFrame), ShouldBeNil)

		n, err := b.Write(leftFrame)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, primitive.ByteSize)

		n, err = b.Write(rightFrame)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, primitive.ByteSize)

		// Frame 1: shared label [x, y]
		sharedFrame := make([]byte, primitive.ByteSize)
		n, err = io.ReadFull(b, sharedFrame)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, primitive.ByteSize)
		shared := primitive.BytesToValue(sharedFrame)
		So(shared.TokenID(0), ShouldEqual, primitive.Tokenize('x', 1))
		So(shared.TokenID(1), ShouldEqual, primitive.Tokenize('y', 2))
		So(shared.TokenID(2), ShouldEqual, uint64(0))
		sharedID := shared.ValueID()

		// Frame 2: left remainder [L, R] → points to shared
		leftRemFrame := make([]byte, primitive.ByteSize)
		n, err = io.ReadFull(b, leftRemFrame)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, primitive.ByteSize)
		leftRem := primitive.BytesToValue(leftRemFrame)
		So(leftRem.TokenID(0), ShouldEqual, primitive.Tokenize('L', 0))
		So(leftRem.TokenID(1), ShouldEqual, primitive.Tokenize('R', 3))
		So(leftRem.NextValueID(), ShouldEqual, sharedID)
		So(leftRem.PrevValueID(), ShouldEqual, uint64(10))

		// Frame 3: right remainder [Q, Z] → points to shared
		rightRemFrame := make([]byte, primitive.ByteSize)
		n, err = io.ReadFull(b, rightRemFrame)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, primitive.ByteSize)
		rightRem := primitive.BytesToValue(rightRemFrame)
		So(rightRem.TokenID(0), ShouldEqual, primitive.Tokenize('Q', 0))
		So(rightRem.TokenID(1), ShouldEqual, primitive.Tokenize('Z', 3))
		So(rightRem.NextValueID(), ShouldEqual, sharedID)
		So(rightRem.PrevValueID(), ShouldEqual, uint64(20))

		n, err = b.Read(make([]byte, primitive.ByteSize))
		So(n, ShouldEqual, 0)
		So(errors.Is(err, io.EOF), ShouldBeTrue)
	})
}

func TestWriteRetainsNonWinningValuesForLaterRounds(t *testing.T) {
	Convey("Non-winning values remain available for future batches", t, func() {
		b := NewBackend(BackendWithBatchCap(2))

		a := primitive.NewValue()
		a.SetTokenID(0, primitive.Tokenize('L', 0))
		a.SetTokenID(1, primitive.Tokenize('x', 1))
		a.SetTokenID(2, primitive.Tokenize('y', 2))
		a.SetTokenID(3, primitive.Tokenize('R', 3))
		a.SetValueID(100)
		a[primitive.StateSlotIndex] = 1

		batchMate := primitive.NewValue()
		batchMate.SetTokenID(0, primitive.Tokenize('n', 0))
		batchMate.SetTokenID(1, primitive.Tokenize('o', 1))
		batchMate.SetValueID(200)
		batchMate[primitive.StateSlotIndex] = 1

		c := primitive.NewValue()
		c.SetTokenID(0, primitive.Tokenize('Q', 0))
		c.SetTokenID(1, primitive.Tokenize('x', 1))
		c.SetTokenID(2, primitive.Tokenize('y', 2))
		c.SetTokenID(3, primitive.Tokenize('Z', 3))
		c.SetValueID(300)
		c[primitive.StateSlotIndex] = 1

		filler := primitive.NewValue()
		filler.SetTokenID(0, primitive.Tokenize('f', 0))
		filler.SetValueID(400)
		filler[primitive.StateSlotIndex] = 1

		frameA := make([]byte, primitive.ByteSize)
		frameB := make([]byte, primitive.ByteSize)
		frameC := make([]byte, primitive.ByteSize)
		frameD := make([]byte, primitive.ByteSize)
		So(primitive.ValueToBytes(a, frameA), ShouldBeNil)
		So(primitive.ValueToBytes(batchMate, frameB), ShouldBeNil)
		So(primitive.ValueToBytes(c, frameC), ShouldBeNil)
		So(primitive.ValueToBytes(filler, frameD), ShouldBeNil)

		_, err := b.Write(frameA)
		So(err, ShouldBeNil)
		_, err = b.Write(frameB)
		So(err, ShouldBeNil)
		_, err = b.Write(frameC)
		So(err, ShouldBeNil)
		_, err = b.Write(frameD)
		So(err, ShouldBeNil)

		// First emitted frame: shared label [x, y]
		sharedFrame := make([]byte, primitive.ByteSize)
		n, err := io.ReadFull(b, sharedFrame)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, primitive.ByteSize)
		shared := primitive.BytesToValue(sharedFrame)
		So(shared.TokenID(0), ShouldEqual, primitive.Tokenize('x', 1))
		So(shared.TokenID(1), ShouldEqual, primitive.Tokenize('y', 2))
		sharedID := shared.ValueID()
		So(shared.PrevValueID(), ShouldEqual, uint64(100))

		// Second: left remainder [L, R] → shared
		leftRemFrame := make([]byte, primitive.ByteSize)
		n, err = io.ReadFull(b, leftRemFrame)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, primitive.ByteSize)
		leftRem := primitive.BytesToValue(leftRemFrame)
		So(leftRem.NextValueID(), ShouldEqual, sharedID)
		So(leftRem.PrevValueID(), ShouldEqual, uint64(100))

		// Third: right remainder [Q, Z] → shared
		rightRemFrame := make([]byte, primitive.ByteSize)
		n, err = io.ReadFull(b, rightRemFrame)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, primitive.ByteSize)
		rightRem := primitive.BytesToValue(rightRemFrame)
		So(rightRem.NextValueID(), ShouldEqual, sharedID)
		So(rightRem.PrevValueID(), ShouldEqual, uint64(300))
	})
}

func TestClose(t *testing.T) {
	Convey("Close always succeeds", t, func() {
		b := NewBackend()
		So(b.Close(), ShouldBeNil)
		So(b.Close(), ShouldBeNil)
	})
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
		_ = be.UniversalBitwise(unsafe.Pointer(&a[0]), unsafe.Pointer(&bv[0]), unsafe.Pointer(&dst[0]), uint32(n))
	}
}
