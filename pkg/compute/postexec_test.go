package compute

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

// peerLabelOffset returns the absolute host word index where a gossip-staged
// peer's LABELS slot lives in the host's asset region. It is the same offset
// runLabelPropagation derives internally; the test recomputes it from the
// canonical core config so a layout drift would surface as a test failure
// against runLabelPropagation rather than as a silent miss.
func peerLabelOffset() int {
	signalsStart := core.Cfg.Value.Region.Signals.Start
	propertiesStart := core.Cfg.Value.Region.Properties.Start
	assetStart := core.Cfg.Value.Region.Asset.Start

	return assetStart + (propertiesStart - signalsStart) + int(primitive.LABELS)
}

func TestRunLabelPropagation(t *testing.T) {
	Convey("runLabelPropagation copies a labeled peer's class into a Prompt host and stamps it as a Readout", t, func() {
		host := primitive.AllocValue()
		defer primitive.FreeValue(host)

		host.SetProperty(primitive.ROLE, uint64(primitive.ValueRolePrompt))
		host.Set(peerLabelOffset(), 3)

		runLabelPropagation(host)

		hostLabel, err := host.Property(primitive.LABELS)
		So(err, ShouldBeNil)
		So(hostLabel, ShouldEqual, 3)
		So(host.Role(), ShouldEqual, primitive.ValueRoleReadout)
		So(host.Status(), ShouldEqual, primitive.RESOLVED)
		So(host.EmitRequested(), ShouldBeTrue)
	})

	Convey("runLabelPropagation does not overwrite a host that already carries a class label", t, func() {
		host := primitive.AllocValue()
		defer primitive.FreeValue(host)

		host.SetProperty(primitive.ROLE, uint64(primitive.ValueRolePrompt))
		host.SetProperty(primitive.LABELS, 2)
		host.Set(peerLabelOffset(), 4)

		runLabelPropagation(host)

		hostLabel, err := host.Property(primitive.LABELS)
		So(err, ShouldBeNil)
		So(hostLabel, ShouldEqual, 2)
	})

	Convey("runLabelPropagation does not promote a non-Prompt host even after copying a label", t, func() {
		host := primitive.AllocValue()
		defer primitive.FreeValue(host)

		host.Set(peerLabelOffset(), 1)

		runLabelPropagation(host)

		hostLabel, err := host.Property(primitive.LABELS)
		So(err, ShouldBeNil)
		So(hostLabel, ShouldEqual, 1)
		So(host.Role(), ShouldEqual, primitive.ValueRoleNone)
		So(host.Status(), ShouldNotEqual, primitive.RESOLVED)
		So(host.EmitRequested(), ShouldBeFalse)
	})

	Convey("runLabelPropagation skips Association hosts entirely", t, func() {
		host := primitive.AllocValue()
		defer primitive.FreeValue(host)

		host.SetProperty(primitive.ROLE, uint64(primitive.ValueRoleAssociation))
		host.Set(peerLabelOffset(), 7)

		runLabelPropagation(host)

		hostLabel, err := host.Property(primitive.LABELS)
		So(err, ShouldBeNil)
		So(hostLabel, ShouldEqual, 0)
	})

	Convey("runLabelPropagation is a no-op when neither host nor peer carry a label", t, func() {
		host := primitive.AllocValue()
		defer primitive.FreeValue(host)

		host.SetProperty(primitive.ROLE, uint64(primitive.ValueRolePrompt))

		runLabelPropagation(host)

		hostLabel, err := host.Property(primitive.LABELS)
		So(err, ShouldBeNil)
		So(hostLabel, ShouldEqual, 0)
		So(host.Role(), ShouldEqual, primitive.ValueRolePrompt)
		So(host.Status(), ShouldNotEqual, primitive.RESOLVED)
	})
}
