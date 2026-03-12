package telemetry

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestSink(t *testing.T) {
	Convey("Given a new telemetry Sink", t, func() {
		// Mock a UDP listener so the dialer has a destination
		addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:8258")
		So(err, ShouldBeNil)
		
		listener, err := net.ListenUDP("udp", addr)
		So(err, ShouldBeNil)
		defer listener.Close()

		sink := NewSink()

		Convey("It should connect successfully", func() {
			So(sink.conn, ShouldNotBeNil)
		})

		Convey("It should emit events without erroring", func() {
			event := Event{
				Component: "Tokenizer",
				Action:    "Test",
				Data: EventData{
					ChunkText: "hello",
				},
			}
			sink.Emit(event)

			// Wait up to 50ms for delivery
			err := listener.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
			So(err, ShouldBeNil)

			var buf [1024]byte
			n, _, err := listener.ReadFromUDP(buf[:])
			So(err, ShouldBeNil)

			var received Event
			err = json.Unmarshal(buf[:n], &received)
			So(err, ShouldBeNil)
			So(received.Component, ShouldEqual, "Tokenizer")
			So(received.Action, ShouldEqual, "Test")
			So(received.Data.ChunkText, ShouldEqual, "hello")
		})
	})
}
