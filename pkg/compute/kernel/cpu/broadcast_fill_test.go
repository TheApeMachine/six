package cpu

import (
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

func TestBroadcastFillU64(t *testing.T) {
	convey.Convey("Given broadcastFillU64", t, func() {
		convey.Convey("It should fill empty slice harmlessly", func() {
			var empty []uint64
			broadcastFillU64(empty, 42)
			convey.So(len(empty), convey.ShouldEqual, 0)
		})

		convey.Convey("It should broadcast to arbitrary length", func() {
			buf := make([]uint64, 37)
			broadcastFillU64(buf, 0xDEADBEEFCAFEBABE)
			for _, word := range buf {
				convey.So(word, convey.ShouldEqual, uint64(0xDEADBEEFCAFEBABE))
			}
		})
	})
}

func BenchmarkBroadcastFillU64(b *testing.B) {
	buf := make([]uint64, 124)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		broadcastFillU64(buf, 0x55)
	}
}
