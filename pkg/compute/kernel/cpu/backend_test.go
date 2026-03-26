package cpu

import (
	"errors"
	"io"
	"math/bits"
	"runtime"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
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
		value[core.Cfg.StateIndex] = 1

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

func TestWriteBatchTriggersEmission(t *testing.T) {
	Convey("When a batch is full, Write pushes mutated Values out the ring buffer", t, func() {
		b := NewBackend(BackendWithBatchCap(2))

		v1 := primitive.NewValue()
		v1.SetValueID(10)
		v1[core.Cfg.StateIndex] = 1

		v2 := primitive.NewValue()
		v2.SetValueID(20)
		v2[core.Cfg.StateIndex] = 1

		frame1 := make([]byte, primitive.ByteSize)
		frame2 := make([]byte, primitive.ByteSize)
		So(primitive.ValueToBytes(v1, frame1), ShouldBeNil)
		So(primitive.ValueToBytes(v2, frame2), ShouldBeNil)

		n, err := b.Write(frame1)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, primitive.ByteSize)

		n, err = b.Write(frame2)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, primitive.ByteSize)

		// They should both be emitted
		out1 := make([]byte, primitive.ByteSize)
		n, err = io.ReadFull(b, out1)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, primitive.ByteSize)

		out2 := make([]byte, primitive.ByteSize)
		n, err = io.ReadFull(b, out2)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, primitive.ByteSize)
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
