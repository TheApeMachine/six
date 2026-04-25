package telemetry

import (
	"context"
	"io"
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

func TestBridge_Connect(t *testing.T) {
	t.Parallel()

	Convey("Connect returns nil when the bridge URL is empty", t, func() {
		bridge, err := NewBridge(context.Background(), "")

		So(err, ShouldBeNil)
		defer bridge.Close()

		connectErr := bridge.Connect()

		So(connectErr, ShouldBeNil)
	})
}

func TestBridge_Close(t *testing.T) {
	t.Parallel()

	Convey("Close on a nil bridge does not panic", t, func() {
		var nilBridge *Bridge

		err := nilBridge.Close()

		So(err, ShouldBeNil)
	})

	Convey("Close cancels and can be called more than once", t, func() {
		bridge, err := NewBridge(context.Background(), "")

		So(err, ShouldBeNil)
		So(bridge.Close(), ShouldBeNil)
		So(bridge.Close(), ShouldBeNil)
	})
}

func TestBridge_Read(t *testing.T) {
	t.Parallel()

	Convey("Read returns io.EOF", t, func() {
		bridge, err := NewBridge(context.Background(), "")

		So(err, ShouldBeNil)
		defer bridge.Close()

		var scratch [4]byte
		n, readErr := bridge.Read(scratch[:])

		So(n, ShouldEqual, 0)
		So(readErr, ShouldEqual, io.EOF)
	})
}

func BenchmarkBridge_Write(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bridge, err := NewBridge(ctx, "")
	if err != nil {
		b.Fatal(err)
	}
	defer bridge.Close()

	payload := []byte{0, 1, 2, 3, 4, 5, 6, 7}

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		_, _ = bridge.Write(payload)
	}
}
