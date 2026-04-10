package viz

import (
	"reflect"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestMarshalWireEventRoundTrip(t *testing.T) {
	Convey("event round-trips", t, func() {
		want := NewEvent(EventTrieInsert, "node_a8f")
		want.Label = "lbl"
		want.Target = "node_b01"
		want.Values["trie_idx"] = 2
		want.Values["x"] = 3.5
		want.Meta["sequence"] = "hello"
		want.Meta["graph"] = `{"edges":[],"vertices":[]}`

		raw := MarshalWireEvent(want)
		ft, got, _, _, _, err := UnmarshalWireMessage(raw)
		So(err, ShouldBeNil)
		So(ft, ShouldEqual, WireFrameEvent)
		So(got.Kind, ShouldEqual, want.Kind)
		So(got.Timestamp, ShouldEqual, want.Timestamp)
		So(got.Source, ShouldEqual, want.Source)
		So(got.Target, ShouldEqual, want.Target)
		So(got.Label, ShouldEqual, want.Label)
		So(reflect.DeepEqual(got.Values, want.Values), ShouldBeTrue)
		So(reflect.DeepEqual(got.Meta, want.Meta), ShouldBeTrue)
	})
}

func TestMarshalWireBootstrapScrubStats(t *testing.T) {
	Convey("bootstrap", t, func() {
		raw := MarshalWireBootstrap([]string{"node_a", "node_b"})
		ft, _, nodes, _, _, err := UnmarshalWireMessage(raw)
		So(err, ShouldBeNil)
		So(ft, ShouldEqual, WireFrameBootstrap)
		So(nodes, ShouldResemble, []string{"node_a", "node_b"})
	})

	Convey("stats", t, func() {
		raw := MarshalWireStats(4242)
		ft, _, _, d, _, err := UnmarshalWireMessage(raw)
		So(err, ShouldBeNil)
		So(ft, ShouldEqual, WireFrameStats)
		So(d, ShouldEqual, 4242)
	})

	Convey("scrub batch", t, func() {
		a := NewEvent(EventNodeCreated, "node_x")
		a.Label = "n1"
		b := NewEvent(EventPeerLatency, "node_x")
		b.Target = "node_y"
		b.Values["latency_ms"] = 1.25
		raw := MarshalWireScrub([]Event{a, b})
		ft, _, _, _, scrub, err := UnmarshalWireMessage(raw)
		So(err, ShouldBeNil)
		So(ft, ShouldEqual, WireFrameScrub)
		So(len(scrub), ShouldEqual, 2)
		So(scrub[0].Kind, ShouldEqual, EventNodeCreated)
		So(scrub[0].Label, ShouldEqual, "n1")
		So(scrub[1].Kind, ShouldEqual, EventPeerLatency)
		So(scrub[1].Values["latency_ms"], ShouldAlmostEqual, 1.25, 1e-9)
	})
}

func TestTryUnmarshalWireEvent(t *testing.T) {
	Convey("try unwrap", t, func() {
		want := NewEvent(EventValuePublished, "node_1")
		ev, ok := TryUnmarshalWireEvent(MarshalWireEvent(want))
		So(ok, ShouldBeTrue)
		So(ev.Kind, ShouldEqual, want.Kind)
		So(ev.Source, ShouldEqual, want.Source)
	})
}
