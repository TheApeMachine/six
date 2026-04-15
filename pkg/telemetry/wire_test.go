package telemetry

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestMarshalWireValueFrameRoundTrip(t *testing.T) {
	Convey("Given a raw Value wire frame", t, func() {
		wantID := uint64(0xabc123)
		payload := make([]byte, 128*8)
		payload[0] = 0x41
		payload[len(payload)-1] = 0xfe

		Convey("It should round-trip through the telemetry wire decoder", func() {
			raw := MarshalWireValueFrame(wantID, payload)

			frameType, _, _, _, _, valueID, wire, err := UnmarshalWireMessage(raw)

			So(err, ShouldBeNil)
			So(frameType, ShouldEqual, byte(WireFrameValue))
			So(valueID, ShouldEqual, wantID)
			So(wire, ShouldResemble, payload)
		})
	})
}

func TestBusPublishClonesMaps(t *testing.T) {
	Convey("Given an active telemetry bus", t, func() {
		bus := NewBus()
		bus.Activate()
		channel := bus.Subscribe(1, nil)
		defer bus.Unsubscribe(channel)

		event := NewEvent(EventTokenizerEmit, "tokenizer")
		event.Values["bytes_written"] = 4
		event.Meta["value_id"] = "0000000000000004"

		Convey("It should freeze payload maps before delivery", func() {
			bus.Publish(event)
			event.Values["bytes_written"] = 99
			event.Meta["value_id"] = "ffffffffffffffff"

			got := <-channel

			So(got.Values["bytes_written"], ShouldEqual, 4)
			So(got.Meta["value_id"], ShouldEqual, "0000000000000004")
		})
	})
}

func BenchmarkMarshalWireValueFrame(b *testing.B) {
	payload := make([]byte, 128*8)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_ = MarshalWireValueFrame(0xabc123, payload)
	}
}
