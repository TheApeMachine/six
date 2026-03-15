package macro

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
)

func TestAnchorRegistry(t *testing.T) {
	gc.Convey("Given a MacroIndex acting as an anchor registry", t, func() {
		idx := NewMacroIndexServer()

		record := idx.RecordAnchor("cat", numeric.Phase(155), "text", "image")

		gc.Convey("RecordAnchor should persist the shared phase and modalities", func() {
			gc.So(record, gc.ShouldNotBeNil)
			gc.So(record.Phase, gc.ShouldEqual, numeric.Phase(155))
			gc.So(record.Modalities["text"], gc.ShouldBeTrue)
			gc.So(record.Modalities["image"], gc.ShouldBeTrue)
		})

		gc.Convey("FindAnchorByName should recover the stored anchor", func() {
			found, ok := idx.FindAnchorByName("cat")
			gc.So(ok, gc.ShouldBeTrue)
			gc.So(found.Phase, gc.ShouldEqual, numeric.Phase(155))
		})

		gc.Convey("Repeated use should harden the anchor", func() {
			for range 3 {
				idx.RecordAnchor("cat", numeric.Phase(155), "text")
			}

			found, ok := idx.FindAnchorByPhase(numeric.Phase(155))
			gc.So(ok, gc.ShouldBeTrue)
			gc.So(found.Hardened, gc.ShouldBeTrue)
			gc.So(found.UseCount, gc.ShouldEqual, uint64(4))
		})
	})
}
