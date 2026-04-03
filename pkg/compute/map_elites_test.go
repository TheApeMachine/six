package compute

import (
	"math/rand"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
)

func TestEliteArchiveStoreAndInject(t *testing.T) {

	convey.Convey("EliteArchive keeps the best fitness per bin and injects it", t, func() {
		saved := core.Cfg.System.MapElitesGridShift
		core.Cfg.System.MapElitesGridShift = 8
		defer func() {
			core.Cfg.System.MapElitesGridShift = saved
		}()

		core.Cfg.Value.Words = 128
		core.Cfg.Value.Region.Affinity.Start = 63
		core.Cfg.Value.Region.Program.Start = 76
		core.Cfg.Value.Region.Program.Bits = 3328

		archive := NewEliteArchive()

		var highFit [128]uint64
		highFit[63] = 0x100
		highFit[76] = 0xAAAAAAAAAAAAAAAA

		archive.StoreIfBetter(&highFit, 0.9)

		var lowFit [128]uint64
		lowFit[63] = 0x100
		lowFit[76] = 0xBBBBBBBBBBBBBBBB

		archive.StoreIfBetter(&lowFit, 0.1)

		var dst [128]uint64
		dst[63] = 0x100
		dst[76] = 0

		rng := rand.New(rand.NewSource(1))
		ok := archive.TryInject(&dst, rng)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(dst[76], convey.ShouldEqual, highFit[76])
	})
}

func TestEliteArchiveLookupBand(t *testing.T) {

	convey.Convey("LookupBand returns a stored band for EliteBinFromHostKey", t, func() {
		saved := core.Cfg.System.MapElitesGridShift
		core.Cfg.System.MapElitesGridShift = 8
		defer func() {
			core.Cfg.System.MapElitesGridShift = saved
		}()

		core.Cfg.Value.Words = 128
		core.Cfg.Value.Region.Affinity.Start = 63
		core.Cfg.Value.Region.Program.Start = 76
		core.Cfg.Value.Region.Program.Bits = 3328

		archive := NewEliteArchive()

		convey.Convey("returns ok false when bin has no elite", func() {
			emptyBin := EliteBinFromHostKey(0xDEADBEEFCAFEBABE)
			bandEmpty, okEmpty := archive.LookupBand(emptyBin)
			convey.So(okEmpty, convey.ShouldBeFalse)
			convey.So(bandEmpty, convey.ShouldBeNil)
		})

		var frame [128]uint64
		frame[63] = 0x9000000000000000
		frame[76] = 0x1111111111111111

		archive.StoreIfBetter(&frame, 0.5)
		bin := EliteBinFromHostKey(0x9000000000000000)
		band, ok := archive.LookupBand(bin)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(len(band), convey.ShouldBeGreaterThan, 0)
		convey.So(band[0], convey.ShouldEqual, frame[76])
	})
}
