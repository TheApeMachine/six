package geometry

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewField(t *testing.T) {
	t.Parallel()

	Convey("NewField allocates p lanes for GF(p) tier primes", t, func() {
		So(len(NewField(Mod257).Fields), ShouldEqual, int(Mod257))
		So(len(NewField(Mod8191).Fields), ShouldEqual, int(Mod8191))
		So(len(NewField(Mod65537).Fields), ShouldEqual, int(Mod65537))
	})
}

func TestLaneCountForModulus(t *testing.T) {
	t.Parallel()

	Convey("LaneCountForModulus matches the prime order", t, func() {
		So(LaneCountForModulus(Mod257), ShouldEqual, 257)
		So(LaneCountForModulus(0), ShouldEqual, 0)
	})
}

func BenchmarkNewField(b *testing.B) {
	for b.Loop() {
		_ = NewField(Mod257)
	}
}
