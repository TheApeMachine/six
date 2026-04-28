package telemetry

import (
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
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

	Convey("Write sends a Value frame only when its bytes changed", t, func() {
		url, messages, closeServer := newBridgeTestServer(t)
		defer closeServer()

		bridge, err := NewBridge(context.Background(), url)

		So(err, ShouldBeNil)
		So(bridge, ShouldNotBeNil)
		defer bridge.Close()

		frame := make([]byte, primitive.FrameByteLength)
		binary.LittleEndian.PutUint64(frame[primitive.IDStartWord*8:], 42)

		n, writeErr := bridge.Write(frame)

		So(writeErr, ShouldBeNil)
		So(n, ShouldEqual, len(frame))
		So(readBridgeTestMessage(t, messages), ShouldEqual, primitive.FrameByteLength)

		n, writeErr = bridge.Write(frame)

		So(writeErr, ShouldBeNil)
		So(n, ShouldEqual, len(frame))
		So(noBridgeTestMessage(messages), ShouldBeTrue)

		frame[0] = 7
		n, writeErr = bridge.Write(frame)

		So(writeErr, ShouldBeNil)
		So(n, ShouldEqual, len(frame))
		So(readBridgeTestMessage(t, messages), ShouldEqual, primitive.FrameByteLength)
	})

	Convey("Write filters unchanged frames inside a batch", t, func() {
		url, messages, closeServer := newBridgeTestServer(t)
		defer closeServer()

		bridge, err := NewBridge(context.Background(), url)

		So(err, ShouldBeNil)
		So(bridge, ShouldNotBeNil)
		defer bridge.Close()

		batch := make([]byte, primitive.FrameByteLength*2)
		first := batch[:primitive.FrameByteLength]
		second := batch[primitive.FrameByteLength:]
		binary.LittleEndian.PutUint64(first[primitive.IDStartWord*8:], 101)
		binary.LittleEndian.PutUint64(second[primitive.IDStartWord*8:], 202)

		n, writeErr := bridge.Write(batch)

		So(writeErr, ShouldBeNil)
		So(n, ShouldEqual, len(batch))
		So(readBridgeTestMessage(t, messages), ShouldEqual, len(batch))

		second[0] = 9
		n, writeErr = bridge.Write(batch)

		So(writeErr, ShouldBeNil)
		So(n, ShouldEqual, len(batch))
		So(readBridgeTestMessage(t, messages), ShouldEqual, primitive.FrameByteLength)
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

func TestBridge_Enabled(t *testing.T) {
	t.Parallel()

	Convey("Enabled reflects whether a WebSocket URL is configured", t, func() {
		disabled, disabledErr := NewBridge(context.Background(), "")
		enabled, enabledErr := NewBridge(context.Background(), "ws://127.0.0.1:6600/ws")

		So(disabledErr, ShouldBeNil)
		So(enabledErr, ShouldBeNil)
		defer disabled.Close()
		defer enabled.Close()

		So(disabled.Enabled(), ShouldBeFalse)
		So(enabled.Enabled(), ShouldBeTrue)
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

func TestBridge_BeginRun(t *testing.T) {
	t.Parallel()

	Convey("BeginRun emits an ID-zero run marker frame", t, func() {
		frame := bridgeRunMarkerFrame(7)

		So(len(frame), ShouldEqual, primitive.FrameByteLength)
		So(binary.LittleEndian.Uint64(frame[0:]), ShouldEqual, bridgeRunMarkerMagic)
		So(binary.LittleEndian.Uint64(frame[8:]), ShouldEqual, uint64(7))
		So(binary.LittleEndian.Uint64(frame[primitive.IDStartWord*8:]), ShouldEqual, uint64(0))
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

func TestBridgeFingerprintCache(t *testing.T) {
	t.Parallel()

	Convey("Add evicts the oldest fingerprint once capacity is reached", t, func() {
		cache := newBridgeFingerprintCache(2)

		cache.Add(1, 10)
		cache.Add(2, 20)
		cache.Add(3, 30)

		_, ok := cache.Get(1)
		So(ok, ShouldBeFalse)

		hash, ok := cache.Get(2)
		So(ok, ShouldBeTrue)
		So(hash, ShouldEqual, 20)

		hash, ok = cache.Get(3)
		So(ok, ShouldBeTrue)
		So(hash, ShouldEqual, 30)
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

func newBridgeTestServer(t *testing.T) (string, <-chan int, func()) {
	t.Helper()

	messages := make(chan int, 8)
	upgrader := websocket.Upgrader{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			_, payload, readErr := conn.ReadMessage()
			if readErr != nil {
				return
			}

			messages <- len(payload)
		}
	}))

	url := "ws" + strings.TrimPrefix(server.URL, "http")

	return url, messages, server.Close
}

func readBridgeTestMessage(t *testing.T, messages <-chan int) int {
	t.Helper()

	select {
	case length := <-messages:
		return length
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bridge message")
		return 0
	}
}

func noBridgeTestMessage(messages <-chan int) bool {
	select {
	case <-messages:
		return false
	case <-time.After(50 * time.Millisecond):
		return true
	}
}
