package viz

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNodeCreated(t *testing.T) {
	t.Parallel()

	Convey("NodeCreated stamps node id and label", t, func() {
		ev := NodeCreated(0xab, "lbl")

		So(ev.Kind, ShouldEqual, EventNodeCreated)
		So(ev.Source, ShouldEqual, "node_ab")
		So(ev.Label, ShouldEqual, "lbl")
	})
}

func TestNodeUpdated(t *testing.T) {
	t.Parallel()

	Convey("NodeUpdated carries metric values", t, func() {
		vals := map[string]float64{"k": 2.5}
		ev := NodeUpdated(1, vals)

		So(ev.Kind, ShouldEqual, EventNodeUpdated)
		So(ev.Values, ShouldResemble, vals)
	})
}

func TestPeerAdded(t *testing.T) {
	t.Parallel()

	Convey("PeerAdded encodes bucket metadata", t, func() {
		ev := PeerAdded(2, 3, 7)

		So(ev.Kind, ShouldEqual, EventPeerAdded)
		So(ev.Target, ShouldEqual, "node_3")
		So(ev.Values["bucket"], ShouldEqual, 7)
	})
}

func TestPeerLatency(t *testing.T) {
	t.Parallel()

	Convey("PeerLatency records latency in Values", t, func() {
		ev := PeerLatency(4, 5, 12.5)

		So(ev.Kind, ShouldEqual, EventPeerLatency)
		So(ev.Values["latency_ms"], ShouldEqual, 12.5)
	})
}

func TestValuePublished(t *testing.T) {
	t.Parallel()

	Convey("ValuePublished stores key in Meta", t, func() {
		ev := ValuePublished(9, 0xf00, "seq")

		So(ev.Kind, ShouldEqual, EventValuePublished)
		So(ev.Label, ShouldEqual, "seq")
		So(ev.Meta["key"], ShouldEqual, "f00")
	})
}

func TestValueReplicated(t *testing.T) {
	t.Parallel()

	Convey("ValueReplicated ties peers to key", t, func() {
		ev := ValueReplicated(1, 2, 0xdeadbeef)

		So(ev.Kind, ShouldEqual, EventValueReplicated)
		So(ev.Target, ShouldEqual, "node_2")
		So(ev.Meta["key"], ShouldEqual, "deadbeef")
	})
}

func TestGossipSent(t *testing.T) {
	t.Parallel()

	Convey("GossipSent records epoch", t, func() {
		ev := GossipSent(3, 99)

		So(ev.Kind, ShouldEqual, EventGossipSent)
		So(ev.Values["epoch"], ShouldEqual, 99)
	})
}

func TestGossipReceived(t *testing.T) {
	t.Parallel()

	Convey("GossipReceived inverts source/target roles", t, func() {
		ev := GossipReceived(8, 9, 42)

		So(ev.Kind, ShouldEqual, EventGossipReceived)
		So(ev.Target, ShouldEqual, "node_9")
		So(ev.Values["epoch"], ShouldEqual, 42)
	})
}

func TestFieldDigestEvent(t *testing.T) {
	t.Parallel()

	Convey("FieldDigestEvent bundles field scalars", t, func() {
		ev := FieldDigestEvent(4, 0.1, 0.2, 0.3)

		So(ev.Kind, ShouldEqual, EventFieldDigest)
		So(ev.Values["surprisal"], ShouldEqual, 0.1)
	})
}

func TestEigenmodeDetected(t *testing.T) {
	t.Parallel()

	Convey("EigenmodeDetected stores mode summary", t, func() {
		ev := EigenmodeDetected(2, 5, 0.88)

		So(ev.Kind, ShouldEqual, EventEigenmodeDetected)
		So(ev.Values["mode_count"], ShouldEqual, 5)
	})
}

func TestFieldPressureEvent(t *testing.T) {
	t.Parallel()

	Convey("FieldPressureEvent carries decay and learning knobs", t, func() {
		ev := FieldPressureEvent(1, 0.5, 1.5, 0.25)

		So(ev.Kind, ShouldEqual, EventFieldPressure)
		So(ev.Values["prune"], ShouldEqual, 0.25)
	})
}

func TestTrieInsertEvent(t *testing.T) {
	t.Parallel()

	Convey("TrieInsertEvent stores sequence meta separate from label", t, func() {
		ev := TrieInsertEvent(6, "tok", "lab")

		So(ev.Kind, ShouldEqual, EventTrieInsert)
		So(ev.Label, ShouldEqual, "lab")
		So(ev.Meta["sequence"], ShouldEqual, "tok")
	})
}

func TestTriePredictEvent(t *testing.T) {
	t.Parallel()

	Convey("TriePredictEvent records confidence", t, func() {
		ev := TriePredictEvent(7, "noun", 0.91)

		So(ev.Kind, ShouldEqual, EventTriePredict)
		So(ev.Values["confidence"], ShouldEqual, 0.91)
	})
}

func TestTrieClassifyEvent(t *testing.T) {
	t.Parallel()

	Convey("TrieClassifyEvent embeds score map", t, func() {
		scores := map[string]float64{"a": 0.4}
		ev := TrieClassifyEvent(1, scores)

		So(ev.Kind, ShouldEqual, EventTrieClassify)
		So(ev.Values, ShouldResemble, scores)
	})
}

func TestTrieExperienceEvent(t *testing.T) {
	t.Parallel()

	Convey("TrieExperienceEvent links surprisal to label", t, func() {
		ev := TrieExperienceEvent(3, 1.2, "ctx")

		So(ev.Kind, ShouldEqual, EventTrieExperience)
		So(ev.Label, ShouldEqual, "ctx")
	})
}

func TestAdaptiveUpdateEvent(t *testing.T) {
	t.Parallel()

	Convey("AdaptiveUpdateEvent passes adaptive metrics", t, func() {
		vals := map[string]float64{"x": 3}
		ev := AdaptiveUpdateEvent(0xcafe, vals)

		So(ev.Kind, ShouldEqual, EventAdaptiveUpdate)
		So(ev.Values, ShouldResemble, vals)
	})
}

func TestTrieCouplingEvent(t *testing.T) {
	t.Parallel()

	Convey("TrieCouplingEvent names trie pair", t, func() {
		ev := TrieCouplingEvent(2, 1, 3, 0.77)

		So(ev.Kind, ShouldEqual, EventTrieCoupling)
		So(ev.Values["coupling"], ShouldEqual, 0.77)
	})
}

func TestTrieModeEvent(t *testing.T) {
	t.Parallel()

	Convey("TrieModeEvent encodes alignment as 0/1", t, func() {
		ev := TrieModeEvent(5, 0, 2, true, 0.4)

		So(ev.Kind, ShouldEqual, EventTrieMode)
		So(ev.Values["aligned"], ShouldEqual, 1)
	})
}

func TestTriePressureEvent(t *testing.T) {
	t.Parallel()

	Convey("TriePressureEvent carries asymmetric pressure", t, func() {
		ev := TriePressureEvent(1, 2, 0.1, 0.2, 0.3, 0.4)

		So(ev.Kind, ShouldEqual, EventTriePressure)
		So(ev.Values["learn_mul"], ShouldEqual, 0.4)
	})
}

func TestTrieSignalEvent(t *testing.T) {
	t.Parallel()

	Convey("TrieSignalEvent snapshots per-trie signals", t, func() {
		ev := TrieSignalEvent(8, 3, 1, 2, 3)

		So(ev.Kind, ShouldEqual, EventTrieSignal)
		So(ev.Values["growth"], ShouldEqual, 3)
	})
}

func TestBeamCollectEvent(t *testing.T) {
	t.Parallel()

	Convey("BeamCollectEvent counts trie fan-in", t, func() {
		ev := BeamCollectEvent(4, 3, 120)

		So(ev.Kind, ShouldEqual, EventBeamCollect)
		So(ev.Values["continuation_count"], ShouldEqual, 120)
	})
}

func TestBeamComposeEvent(t *testing.T) {
	t.Parallel()

	Convey("BeamComposeEvent records selection stats", t, func() {
		ev := BeamComposeEvent(2, 5, 2, 0.95)

		So(ev.Kind, ShouldEqual, EventBeamCompose)
		So(ev.Values["best_score"], ShouldEqual, 0.95)
	})
}

func TestBeamBreakEvent(t *testing.T) {
	t.Parallel()

	Convey("BeamBreakEvent identifies trie id", t, func() {
		ev := BeamBreakEvent(9, 0xfeed)

		So(ev.Kind, ShouldEqual, EventBeamBreak)
		So(ev.Target, ShouldEqual, "trie_feed")
		So(ev.Values["trie_id"], ShouldEqual, float64(0xfeed))
	})
}

func TestBeamConvergeEvent(t *testing.T) {
	t.Parallel()

	Convey("BeamConvergeEvent returns winning sequence", t, func() {
		ev := BeamConvergeEvent(1, "out", 0.8)

		So(ev.Kind, ShouldEqual, EventBeamConverge)
		So(ev.Label, ShouldEqual, "out")
	})
}

func TestPoolScheduleEvent(t *testing.T) {
	t.Parallel()

	Convey("PoolScheduleEvent describes queue pressure", t, func() {
		ev := PoolScheduleEvent("run", 10, 4)

		So(ev.Kind, ShouldEqual, EventPoolSchedule)
		So(ev.Source, ShouldEqual, "pool")
		So(ev.Label, ShouldEqual, "run")
		So(ev.Values["workers"], ShouldEqual, 4)
	})
}

func TestPoolCompleteEvent(t *testing.T) {
	t.Parallel()

	Convey("PoolCompleteEvent records duration", t, func() {
		ev := PoolCompleteEvent("run", 33)

		So(ev.Kind, ShouldEqual, EventPoolComplete)
		So(ev.Source, ShouldEqual, "pool")
		So(ev.Label, ShouldEqual, "run")
		So(ev.Values["duration_ms"], ShouldEqual, 33)
	})
}

func TestPromptEvent(t *testing.T) {
	t.Parallel()

	Convey("PromptEvent wraps user text", t, func() {
		ev := PromptEvent("hello")

		So(ev.Kind, ShouldEqual, EventPrompt)
		So(ev.Source, ShouldEqual, "user")
		So(ev.Meta["prompt"], ShouldEqual, "hello")
	})
}

func TestPromptResultEvent(t *testing.T) {
	t.Parallel()

	Convey("PromptResultEvent stores generation meta", t, func() {
		scores := map[string]float64{"ok": 1}
		ev := PromptResultEvent("gen", scores)

		So(ev.Kind, ShouldEqual, EventPromptResult)
		So(ev.Source, ShouldEqual, "system")
		So(ev.Meta["generation"], ShouldEqual, "gen")
		So(ev.Values, ShouldResemble, scores)
	})
}

func BenchmarkNodeCreated(b *testing.B) {
	b.ResetTimer()

	for b.Loop() {
		_ = NodeCreated(0x55, "x")
	}
}
