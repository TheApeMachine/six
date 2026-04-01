package primitive_test

import (
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
TestInstallQueryFirmware mirrors (*primitive.Value).InstallQueryFirmware with a
real Backend: must live in primitive_test so pkg/primitive tests do not import
compute (import cycle with backend).
*/
func TestInstallQueryFirmware(t *testing.T) {
	Convey("InstallQueryFirmware installs programs.query; UniversalBitwise copies Prev into state.index", t, func() {
		self, err := primitive.NewValue(nil)
		So(err, ShouldBeNil)
		defer self.Close()

		partner, err := primitive.NewValue(nil)
		So(err, ShouldBeNil)
		defer partner.Close()

		wantPrev := uint64(0xdeadbeefcafebabe)
		self[core.Cfg.Value.Region.Prev.Start] = wantPrev
		self[core.Cfg.Value.Region.State.Index] = 0

		self.InstallQueryFirmware()

		backend := compute.NewBackgroundBackend()
		defer backend.Close()

		var workSelf, workPartner primitive.Value

		primitive.CopyFrame(&workSelf, self)
		primitive.CopyFrame(&workPartner, partner)

		So(backend.UniversalBitwise(unsafe.Pointer(&workSelf), unsafe.Pointer(&workPartner)), ShouldBeNil)
		So(workSelf[core.Cfg.Value.Region.State.Index], ShouldEqual, wantPrev)
		So(workSelf[core.Cfg.Value.Region.Prev.Start], ShouldEqual, wantPrev)
	})
}

/*
BenchmarkInstallQueryFirmware mirrors TestInstallQueryFirmware hot path.
*/
func BenchmarkInstallQueryFirmware(b *testing.B) {
	proto, err := primitive.NewValue(nil)

	if err != nil {
		b.Fatal(err)
	}

	defer proto.Close()

	partnerProto, err := primitive.NewValue(nil)

	if err != nil {
		b.Fatal(err)
	}

	defer partnerProto.Close()

	proto[core.Cfg.Value.Region.Prev.Start] = 42
	proto.InstallQueryFirmware()

	backend := compute.NewBackgroundBackend()
	defer backend.Close()

	var workSelf, workPartner primitive.Value

	b.ResetTimer()

	for b.Loop() {
		primitive.CopyFrame(&workSelf, proto)
		primitive.CopyFrame(&workPartner, partnerProto)

		if err := backend.UniversalBitwise(unsafe.Pointer(&workSelf), unsafe.Pointer(&workPartner)); err != nil {
			b.Fatal(err)
		}
	}
}
