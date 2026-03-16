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
		addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
		So(err, ShouldBeNil)
		
		listener, err := net.ListenUDP("udp", addr)
		So(err, ShouldBeNil)
		defer listener.Close()

		sink := NewSink(WithAddress(listener.LocalAddr().String()))

		Convey("It should connect successfully", func() {
			// Instead of So(sink.conn, ShouldNotBeNil), we test observable behaviour
			// below by ensuring Emit executes and data is received by the listener.
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

func BenchmarkSinkEmit(b *testing.B) {
	addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	listener, err := net.ListenUDP("udp", addr)
	if err != nil {
		b.Fatal(err)
	}
	defer listener.Close()

	sink := NewSink(WithAddress(listener.LocalAddr().String()))
	event := Event{
		Component: "Benchmark",
		Action:    "Emit",
		Data:      EventData{ChunkText: "small"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink.Emit(event)
	}
}

func BenchmarkSinkEmitLargePayload(b *testing.B) {
	addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	listener, err := net.ListenUDP("udp", addr)
	if err != nil {
		b.Fatal(err)
	}
	defer listener.Close()

	sink := NewSink(WithAddress(listener.LocalAddr().String()))
	event := Event{
		Component: "Benchmark",
		Action:    "EmitLarge",
		Data: EventData{
			ChunkText:  "large payload text",
			ActiveBits: []int{1, 5, 10, 20, 30, 100, 200, 300},
			MatchBits:  []int{1, 5, 10},
			Density:    0.15,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink.Emit(event)
	}
}
