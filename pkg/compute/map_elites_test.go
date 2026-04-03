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
