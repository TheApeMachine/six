package viz

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewTimeline(t *testing.T) {
	t.Parallel()

	Convey("NewTimeline applies a positive default capacity", t, func() {
		tl := NewTimeline(0)

		So(tl.Len(), ShouldEqual, 0)
		So(tl, ShouldNotBeNil)
	})

	Convey("NewTimeline honors explicit capacity", t, func() {
		tl := NewTimeline(3)

		So(tl.Len(), ShouldEqual, 0)
	})
}

func TestTimelineRecord(t *testing.T) {
	t.Parallel()

	Convey("Record grows Len until cap", t, func() {
		tl := NewTimeline(2)

		tl.Record(NewEvent(EventNodeCreated, "a"))
		tl.Record(NewEvent(EventNodeUpdated, "b"))
		tl.Record(NewEvent(EventNodeRemoved, "c"))

		So(tl.Len(), ShouldEqual, 2)
	})
}

func TestTimelineLen(t *testing.T) {
	t.Parallel()

	Convey("Len tracks stored events", t, func() {
		tl := NewTimeline(10)

		So(tl.Len(), ShouldEqual, 0)

		tl.Record(NewEvent(EventGossipSent, "g"))

		So(tl.Len(), ShouldEqual, 1)
	})
}

func TestTimelineRange(t *testing.T) {
	t.Parallel()

	Convey("Range returns insertion-ordered slice", t, func() {
		tl := NewTimeline(5)

		tl.Record(NewEvent(EventTriePredict, "one"))
		tl.Record(NewEvent(EventTriePredict, "two"))

		slice := tl.Range(0, 2)

		So(len(slice), ShouldEqual, 2)
		So(slice[0].Source, ShouldEqual, "one")
		So(slice[1].Source, ShouldEqual, "two")
	})

	Convey("when from >= to, Range returns nil", t, func() {
		tl := NewTimeline(3)

		tl.Record(NewEvent(EventTriePredict, "x"))

		So(tl.Range(1, 0), ShouldBeNil)
	})
}

func TestTimelineSnapshot(t *testing.T) {
	t.Parallel()

	Convey("Snapshot mirrors Range(0, Len)", t, func() {
		tl := NewTimeline(4)

		tl.Record(NewEvent(EventFieldDigest, "f"))

		snap := tl.Snapshot()

		So(len(snap), ShouldEqual, 1)
		So(snap[0].Kind, ShouldEqual, EventFieldDigest)
	})
}

func TestTimelineLoad(t *testing.T) {
	t.Parallel()

	Convey("Load replays events up to capacity", t, func() {
		tl := NewTimeline(2)

		events := []Event{
			NewEvent(EventBeamBreak, "one"),
			NewEvent(EventBeamBreak, "two"),
			NewEvent(EventBeamBreak, "three"),
		}

		tl.Load(events)

		So(tl.Len(), ShouldEqual, 2)

		snap := tl.Snapshot()

		So(snap[0].Source, ShouldEqual, "one")
		So(snap[1].Source, ShouldEqual, "two")
	})
}

func BenchmarkTimelineRecord(b *testing.B) {
	tl := NewTimeline(1000)
	ev := NewEvent(EventTrieDecay, "bench")

	b.ResetTimer()

	for b.Loop() {
		tl.Record(ev)
	}
}
