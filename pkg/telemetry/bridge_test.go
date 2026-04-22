package telemetry

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestBridge_Write(t *testing.T) {
	t.Parallel()

	Convey("Write succeeds when the bridge URL is empty (no uplink)", t, func() {
		bridge, err := NewBridge(context.Background(), "")

		So(err, ShouldBeNil)
		So(bridge, ShouldNotBeNil)
		defer bridge.Close()

		payload := []byte{1, 2, 3, 4}
		n, writeErr := bridge.Write(payload)

		So(writeErr, ShouldBeNil)
		So(n, ShouldEqual, len(payload))
	})
}
