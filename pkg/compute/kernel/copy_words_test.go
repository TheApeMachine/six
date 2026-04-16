package kernel

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestCopyWordsInFrame(t *testing.T) {
	Convey("Given a frame with distinct regions", t, func() {
		Convey("It should copy forward without overlap", func() {
			var frame [128]uint64

			frame[10] = 7
			frame[11] = 8

			CopyWordsInFrame(&frame, 10, 20, 2)

			So(frame[20], ShouldEqual, 7)
			So(frame[21], ShouldEqual, 8)
		})

		Convey("It should behave like memmove when ranges overlap", func() {
			var frame [128]uint64

			frame[5] = 1
			frame[6] = 2
			frame[7] = 3

			CopyWordsInFrame(&frame, 5, 6, 3)

			So(frame[6], ShouldEqual, 1)
			So(frame[7], ShouldEqual, 2)
			So(frame[8], ShouldEqual, 3)
		})
	})
}

func TestCopyWordsBetween(t *testing.T) {
	Convey("Given two distinct frames", t, func() {
		Convey("It should copy words from src into dst", func() {
			var dst, src [128]uint64

			src[3] = 99
			src[4] = 100

			CopyWordsBetween(&dst, &src, 10, 3, 2)

			So(dst[10], ShouldEqual, 99)
			So(dst[11], ShouldEqual, 100)
		})

		Convey("It should route same-pointer frames through CopyWordsInFrame", func() {
			var frame [128]uint64

			frame[2] = 42
			frame[3] = 43

			CopyWordsBetween(&frame, &frame, 8, 2, 2)

			So(frame[8], ShouldEqual, 42)
			So(frame[9], ShouldEqual, 43)
		})
	})
}

func BenchmarkCopyWordsBetween(b *testing.B) {
	var dst, src [128]uint64

	src[0] = 1

	b.ResetTimer()

	for range b.N {
		CopyWordsBetween(&dst, &src, 64, 0, 32)
	}
}
