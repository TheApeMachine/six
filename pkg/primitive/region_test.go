package primitive

import (
	"errors"
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewRegion(t *testing.T) {
	Convey("Given NewRegion", t, func() {
		Convey("It should return a Region with the given ID that accepts aligned writes", func() {
			r := NewRegion(99)
			defer r.Close()

			So(r.ID, ShouldEqual, 99)
			frame := make([]byte, ByteSize)
			_, err := r.Write(frame)
			So(err, ShouldBeNil)
		})
	})
}

func TestRegion_Read(t *testing.T) {
	Convey("Given a Region", t, func() {
		Convey("It should return io.ErrShortBuffer when len(p) < ByteSize", func() {
			r := NewRegion(1)
			defer r.Close()

			short := make([]byte, ByteSize-1)
			n, err := r.Read(short)

			So(n, ShouldEqual, 0)
			So(errors.Is(err, io.ErrShortBuffer), ShouldBeTrue)
		})

		Convey("It should return io.EOF when no frames are queued", func() {
			r := NewRegion(1)
			defer r.Close()

			buf := make([]byte, ByteSize)
			n, err := r.Read(buf)

			So(n, ShouldEqual, 0)
			So(errors.Is(err, io.EOF), ShouldBeTrue)
		})

		Convey("It should copy one mixer frame into p when available", func() {
			r := NewRegion(2)
			defer r.Close()

			want := make([]byte, ByteSize)
			want[0] = 0x7e
			_, werr := r.Write(want)
			So(werr, ShouldBeNil)

			got := make([]byte, ByteSize)
			nRead, rerr := r.Read(got)

			So(rerr, ShouldBeNil)
			So(nRead, ShouldEqual, ByteSize)
			So(got[0], ShouldEqual, want[0])
		})

		Convey("It should read from spill after the mixer is drained", func() {
			r := NewRegion(3)
			defer r.Close()

			mixerCap := 64
			for i := 0; i < mixerCap; i++ {
				f := make([]byte, ByteSize)
				f[0] = 1
				So(regionWriteOK(r, f), ShouldBeNil)
			}

			spillFrame := make([]byte, ByteSize)
			spillFrame[0] = 42
			So(regionWriteOK(r, spillFrame), ShouldBeNil)

			drain := make([]byte, ByteSize)
			for i := 0; i < mixerCap; i++ {
				So(regionReadCount(r, drain), ShouldEqual, ByteSize)
			}

			got := make([]byte, ByteSize)
			n, err := r.Read(got)
			So(err, ShouldBeNil)
			So(n, ShouldEqual, ByteSize)
			So(got[0], ShouldEqual, 42)
		})
	})
}

func TestRegion_Write(t *testing.T) {
	Convey("Given a Region", t, func() {
		Convey("It should accept an empty p as a no-op", func() {
			r := NewRegion(1)
			defer r.Close()

			n, err := r.Write(nil)
			So(err, ShouldBeNil)
			So(n, ShouldEqual, 0)
		})

		Convey("It should return RegionErrAlign when len(p) is not a multiple of ByteSize", func() {
			r := NewRegion(1)
			defer r.Close()

			bad := make([]byte, ByteSize-1)
			n, err := r.Write(bad)

			So(n, ShouldEqual, 0)
			var re *RegionError
			So(errors.As(err, &re), ShouldBeTrue)
		})

		Convey("It should enqueue every aligned frame in a single Write", func() {
			r := NewRegion(4)
			defer r.Close()

			two := make([]byte, ByteSize*2)
			two[0] = 1
			two[ByteSize] = 2

			n, err := r.Write(two)
			So(err, ShouldBeNil)
			So(n, ShouldEqual, ByteSize*2)

			first := make([]byte, ByteSize)
			So(regionReadCount(r, first), ShouldEqual, ByteSize)
			So(first[0], ShouldEqual, 1)

			second := make([]byte, ByteSize)
			So(regionReadCount(r, second), ShouldEqual, ByteSize)
			So(second[0], ShouldEqual, 2)
		})

		Convey("It should preserve FIFO order for back-to-back single-frame writes", func() {
			r := NewRegion(5)
			defer r.Close()

			for i := 0; i < 5; i++ {
				f := make([]byte, ByteSize)
				f[0] = byte(i + 1)
				So(regionWriteOK(r, f), ShouldBeNil)
			}
			for i := 0; i < 5; i++ {
				got := make([]byte, ByteSize)
				So(regionReadCount(r, got), ShouldEqual, ByteSize)
				So(got[0], ShouldEqual, byte(i+1))
			}
		})
	})
}

func TestRegion_Close(t *testing.T) {
	Convey("Given a Region", t, func() {
		Convey("It should succeed when the receiver is nil", func() {
			var r *Region
			So(r.Close(), ShouldBeNil)
		})

		Convey("It should be idempotent", func() {
			r := NewRegion(1)
			So(r.Close(), ShouldBeNil)
			So(r.Close(), ShouldBeNil)
		})

		Convey("It should leave the region unusable for Write (panic on send to closed channel)", func() {
			r := NewRegion(1)
			So(r.Close(), ShouldBeNil)

			frame := make([]byte, ByteSize)
			So(func() { _, _ = r.Write(frame) }, ShouldPanic)
		})
	})
}

func TestRegion_SpillStats(t *testing.T) {
	Convey("Given (*Region).SpillStats", t, func() {
		Convey("It should report queued spill depth and accept count after overflow", func() {
			r := NewRegion(6)
			defer r.Close()

			mixerCap := 64
			for i := 0; i < mixerCap; i++ {
				f := make([]byte, ByteSize)
				f[0] = 1
				So(regionWriteOK(r, f), ShouldBeNil)
			}

			extra := make([]byte, ByteSize)
			extra[0] = 42
			So(regionWriteOK(r, extra), ShouldBeNil)

			queued, acc, drop := r.SpillStats()
			So(queued, ShouldEqual, 1)
			So(acc, ShouldEqual, 1)
			So(drop, ShouldEqual, 0)
		})
	})
}

func regionWriteOK(r *Region, f []byte) error {
	_, err := r.Write(f)
	return err
}

func regionReadCount(r *Region, p []byte) int {
	n, err := r.Read(p)
	if err != nil {
		panic(err)
	}
	return n
}

func BenchmarkNewRegion(b *testing.B) {
	for i := 0; i < b.N; i++ {
		r := NewRegion(0)
		_ = r.Close()
	}
}

func BenchmarkRegion_Read(b *testing.B) {
	frame := make([]byte, ByteSize)
	readBuf := make([]byte, ByteSize)
	r := NewRegion(0)
	defer r.Close()

	_, _ = r.Write(frame)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = r.Read(readBuf)
		_, _ = r.Write(frame)
	}
}

func BenchmarkRegion_Write(b *testing.B) {
	frame := make([]byte, ByteSize)
	readBuf := make([]byte, ByteSize)
	r := NewRegion(0)
	defer r.Close()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = r.Write(frame)
		_, _ = r.Read(readBuf)
	}
}

func BenchmarkRegion_Close(b *testing.B) {
	for i := 0; i < b.N; i++ {
		r := NewRegion(0)
		_ = r.Close()
	}
}

func BenchmarkRegion_SpillStats(b *testing.B) {
	r := NewRegion(0)
	defer r.Close()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _, _ = r.SpillStats()
	}
}
