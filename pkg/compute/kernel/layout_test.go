package kernel

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestPackRegionRef(t *testing.T) {
	t.Parallel()

	Convey("PackRegionRef round-trips through UnpackRegionRef", t, func() {
		ref := PackRegionRef(10, 20)
		start, span := UnpackRegionRef(ref)

		So(start, ShouldEqual, 10)
		So(span, ShouldEqual, 20)
	})
}

func TestUnpackRegionRef(t *testing.T) {
	t.Parallel()

	Convey("UnpackRegionRef splits the low and high dwords", t, func() {
		const low = 0xdeadbeef
		const high = 0x00c0ffee

		ref := uint64(low) | uint64(high)<<32
		start, span := UnpackRegionRef(ref)

		So(uint32(start), ShouldEqual, uint32(low))
		So(uint32(span), ShouldEqual, uint32(high))
	})
}

func BenchmarkPackRegionRef(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_ = PackRegionRef(16, 32)
	}
}

func BenchmarkUnpackRegionRef(b *testing.B) {
	ref := PackRegionRef(16, 32)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_, _ = UnpackRegionRef(ref)
	}
}
