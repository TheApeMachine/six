package telemetry

import (
	"net"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/errnie"
)

func TestSink(t *testing.T) {
	Convey("Given a new telemetry Sink", t, func() {
		addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
		So(err, ShouldBeNil)

		listener, err := net.ListenUDP("udp", addr)
		So(err, ShouldBeNil)
		defer listener.Close()

		sink := NewSink(WithAddress(listener.LocalAddr().String()))

		Convey("It should emit events that round-trip through binary encoding", func() {
			event := Event{
				Component: "Tokenizer",
				Action:    "Test",
				Data: EventData{
					ChunkText: "hello",
					Step:      3,
					MaxSteps:  10,
					Density:   0.42,
					Advanced:  true,
				},
			}
			sink.Emit(event)

			err := listener.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
			So(err, ShouldBeNil)

			var buf [4096]byte
			n, _, err := listener.ReadFromUDP(buf[:])
			So(err, ShouldBeNil)

			received := DecodeBinary(buf[:n])
			So(received.Component, ShouldEqual, "Tokenizer")
			So(received.Action, ShouldEqual, "Test")
			So(received.Data.ChunkText, ShouldEqual, "hello")
			So(received.Data.Step, ShouldEqual, 3)
			So(received.Data.MaxSteps, ShouldEqual, 10)
			So(received.Data.Density, ShouldAlmostEqual, 0.42)
			So(received.Data.Advanced, ShouldBeTrue)
			So(received.Data.Stable, ShouldBeFalse)
		})

		Convey("It should round-trip int slices correctly", func() {
			event := Event{
				Component: "Graph",
				Action:    "Insert",
				Data: EventData{
					ActiveBits: []int{1, 5, 10, 200},
					MatchBits:  []int{3, 7},
					CancelBits: []int{},
				},
			}
			sink.Emit(event)

			err := listener.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
			So(err, ShouldBeNil)

			var buf [4096]byte
			n, _, err := listener.ReadFromUDP(buf[:])
			So(err, ShouldBeNil)

			received := DecodeBinary(buf[:n])
			So(received.Data.ActiveBits, ShouldResemble, []int{1, 5, 10, 200})
			So(received.Data.MatchBits, ShouldResemble, []int{3, 7})
			So(received.Data.CancelBits, ShouldResemble, []int{})
		})
	})
}

func TestWireRoundTrip(t *testing.T) {
	Convey("Given a fully populated Event", t, func() {
		event := Event{
			Component: "Program",
			Action:    "Execute",
			Data: EventData{
				ValueID:         42,
				Bin:             7,
				State:           "active",
				ActiveBits:      []int{0, 3, 15, 255},
				Density:         0.125,
				ChunkText:       "boundary",
				Residue:         12,
				MatchBits:       []int{1, 2},
				CancelBits:      []int{9},
				Left:            100,
				Right:           200,
				Pos:             50,
				Paths:           4,
				Chunks:          8,
				Edges:           16,
				Level:           2,
				Theta:           3.14159,
				ParentBin:       5,
				ChildCount:      3,
				Stage:           "step",
				Message:         "converged",
				EdgeCount:       32,
				PathCount:       64,
				ResultText:      "answer",
				WavefrontEnergy: 999,
				EntryCount:      128,
				Step:            7,
				MaxSteps:        20,
				CandidateCount:  5,
				BestIndex:       2,
				PreResidue:      10,
				PostResidue:     3,
				Advanced:        true,
				Stable:          false,
				Outcome:         "improved",
				SpanSize:        17,
			},
		}

		Convey("AppendBinary/DecodeBinary should reproduce every field", func() {
			wire := event.AppendBinary(nil)
			got := DecodeBinary(wire)

			So(got.Component, ShouldEqual, event.Component)
			So(got.Action, ShouldEqual, event.Action)
			So(got.Data, ShouldResemble, event.Data)
		})
	})
}

func BenchmarkSinkEmit(b *testing.B) {
	addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:0")

	listener := errnie.Guard(errnie.NewState("visualizer/benchmark"), func() (*net.UDPConn, error) {
		return net.ListenUDP("udp", addr)
	})
	defer listener.Close()

	sink := NewSink(WithAddress(listener.LocalAddr().String()))
	event := Event{
		Component: "Benchmark",
		Action:    "Emit",
		Data:      EventData{ChunkText: "small"},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		sink.Emit(event)
	}
}

func BenchmarkSinkEmitLargePayload(b *testing.B) {
	addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:0")

	listener := errnie.Guard(errnie.NewState("visualizer/benchmark"), func() (*net.UDPConn, error) {
		return net.ListenUDP("udp", addr)
	})
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
	b.ReportAllocs()

	for b.Loop() {
		sink.Emit(event)
	}
}

func BenchmarkAppendBinary(b *testing.B) {
	event := Event{
		Component: "Program",
		Action:    "Execute",
		Data: EventData{
			Stage:          "step",
			Step:           5,
			MaxSteps:       20,
			PreResidue:     10,
			PostResidue:    3,
			BestIndex:      2,
			CandidateCount: 5,
			Advanced:       true,
		},
	}

	buf := make([]byte, 0, 512)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		buf = event.AppendBinary(buf[:0])
	}
}
