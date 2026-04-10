package markovtrie

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/primitive"
)

func coordinatorValue(tb testing.TB, text string) primitive.Value {
	tb.Helper()

	value, err := primitive.FirstSegment(primitive.NewValue([]byte(text)))

	if err != nil {
		tb.Fatal(err)
	}

	tb.Cleanup(func() {
		_ = value.Close()
	})

	return *value
}

func TestNewMultimodalCoordinator(t *testing.T) {
	setupMarkovTrieValueConfig(t)

	t.Parallel()

	Convey("nil context panics (WithCancel before Require)", t, func() {
		defer func() {
			So(recover(), ShouldNotBeNil)
		}()

		//lint:ignore SA1012 deliberate nil parent: coordinator uses WithCancel before validate (recover above).
		_, _ = NewMultimodalCoordinator(nil)
	})

	Convey("valid context wires three stores", t, func() {
		coordinator, err := NewMultimodalCoordinator(context.Background())

		So(err, ShouldBeNil)
		So(coordinator, ShouldNotBeNil)
		So(coordinator.Sensory, ShouldNotBeNil)
		So(coordinator.Action, ShouldNotBeNil)
		So(coordinator.Reward, ShouldNotBeNil)
	})
}

func TestMultimodalCoordinatorLinkKey(t *testing.T) {
	setupMarkovTrieValueConfig(t)

	t.Parallel()

	Convey("nil coordinator returns empty key", t, func() {
		var coordinator *MultimodalCoordinator

		So(coordinator.LinkKey("a", "b", "c"), ShouldEqual, "")
	})

	Convey("LinkKey joins sequences with ASCII unit separator", t, func() {
		coordinator, err := NewMultimodalCoordinator(context.Background())

		So(err, ShouldBeNil)

		So(
			coordinator.LinkKey("sense", "act", "rew"),
			ShouldEqual,
			"sense\x1fact\x1frew",
		)
	})
}

func TestMultimodalCoordinatorObserveOutcome(t *testing.T) {
	setupMarkovTrieValueConfig(t)

	t.Parallel()

	Convey("ObserveOutcome loads modalities and updates coactivation stats", t, func() {
		coordinator, err := NewMultimodalCoordinator(context.Background())

		So(err, ShouldBeNil)

		sensory := coordinatorValue(t, "sense-a")
		action := coordinatorValue(t, "move-left")
		reward := coordinatorValue(t, "reward-good")

		err = coordinator.ObserveOutcome(sensory, action, reward, 2.5, "good")

		So(err, ShouldBeNil)
		So(coordinator.Sensory.root, ShouldNotBeNil)
		So(coordinator.Action.root, ShouldNotBeNil)
		So(coordinator.Reward.root, ShouldNotBeNil)

		snapshot := coordinator.coactivation.Load()
		key := coordinator.LinkKey("sense-a", "move-left", "reward-good")

		So(snapshot, ShouldNotBeNil)
		So(snapshot.m[key].RewardSum, ShouldAlmostEqual, 2.5, 1e-9)
		So(snapshot.m[key].Count, ShouldAlmostEqual, 1, 1e-9)
	})

	Convey("ObserveOutcome records reward residuals for repeated state-action outcomes", t, func() {
		coordinator, err := NewMultimodalCoordinator(context.Background())

		So(err, ShouldBeNil)

		sensory := coordinatorValue(t, "sense-r")
		action := coordinatorValue(t, "move-r")
		good := coordinatorValue(t, "reward-good-r")
		bad := coordinatorValue(t, "reward-bad-r")
		var goodAffinity [primitive.AffinityWords]uint64
		var badAffinity [primitive.AffinityWords]uint64

		goodAffinity[0] = 0xf00
		badAffinity[0] = 0x00f

		good.SetAffinityVector(goodAffinity)
		bad.SetAffinityVector(badAffinity)

		So(coordinator.ObserveOutcome(sensory, action, good, 2), ShouldBeNil)

		tracker := coordinator.causal.Value().Signals[algo.InterventionResidual]
		before := tracker.Value()

		So(coordinator.ObserveOutcome(sensory, action, bad, -1), ShouldBeNil)

		snapshot := coordinator.coactivation.Load()
		key := coordinator.LinkKey("sense-r", "move-r", "reward-bad-r")

		So(tracker.Value(), ShouldBeGreaterThan, before)
		So(snapshot.m[key].ResidualSum, ShouldBeGreaterThan, 0)
	})
}

func TestMultimodalCoordinatorActionScores(t *testing.T) {
	setupMarkovTrieValueConfig(t)

	t.Parallel()

	Convey("ActionScores ranks actions by expected reinforcement", t, func() {
		coordinator, err := NewMultimodalCoordinator(context.Background())

		So(err, ShouldBeNil)

		sensory := coordinatorValue(t, "state")
		left := coordinatorValue(t, "left")
		right := coordinatorValue(t, "right")
		good := coordinatorValue(t, "good")
		bad := coordinatorValue(t, "bad")

		So(coordinator.ObserveOutcome(sensory, left, good, 3), ShouldBeNil)
		So(coordinator.ObserveOutcome(sensory, left, good, 1), ShouldBeNil)
		So(coordinator.ObserveOutcome(sensory, right, bad, -2), ShouldBeNil)

		scores, scoreErr := coordinator.ActionScores(sensory)

		So(scoreErr, ShouldBeNil)
		So(len(scores), ShouldEqual, 2)
		So(scores[0].Action, ShouldEqual, "left")
		So(scores[0].Score, ShouldBeGreaterThan, scores[1].Score)
	})

	Convey("ActionScores boosts causally stable actions when rewards tie", t, func() {
		coordinator, err := NewMultimodalCoordinator(context.Background())

		So(err, ShouldBeNil)

		sensory := coordinatorValue(t, "state")
		left := coordinatorValue(t, "left")
		right := coordinatorValue(t, "right")
		reward := coordinatorValue(t, "reward")

		So(
			coordinator.ObserveOutcomeInRegimes(
				sensory,
				left,
				reward,
				2,
				[]string{"bright"},
				"good",
			),
			ShouldBeNil,
		)
		So(
			coordinator.ObserveOutcomeInRegimes(
				sensory,
				left,
				reward,
				2,
				[]string{"dim"},
				"excellent",
			),
			ShouldBeNil,
		)
		So(
			coordinator.ObserveOutcomeInRegimes(
				sensory,
				right,
				reward,
				2,
				[]string{"bright"},
				"good",
			),
			ShouldBeNil,
		)
		So(
			coordinator.ObserveOutcomeInRegimes(
				sensory,
				right,
				reward,
				2,
				[]string{"bright"},
				"good",
			),
			ShouldBeNil,
		)

		So(
			coordinator.causal.EdgeInvariance(sensory.ID(), left.ID()),
			ShouldBeGreaterThan,
			0,
		)
		So(
			coordinator.causal.EdgeInvariance(left.ID(), reward.ID()),
			ShouldBeGreaterThan,
			0,
		)
		So(
			coordinator.causal.EdgeInvariance(sensory.ID(), right.ID()),
			ShouldEqual,
			0,
		)
		So(
			coordinator.causal.EdgeInvariance(right.ID(), reward.ID()),
			ShouldEqual,
			0,
		)

		scores, scoreErr := coordinator.ActionScores(sensory)

		So(scoreErr, ShouldBeNil)
		So(len(scores), ShouldEqual, 2)
		So(scores[0].Action, ShouldEqual, "left")
		So(scores[0].Score, ShouldBeGreaterThan, scores[1].Score)
	})
}

func TestMultimodalCoordinatorPredictAction(t *testing.T) {
	setupMarkovTrieValueConfig(t)

	t.Parallel()

	Convey("PredictAction projects ranked actions into continuations", t, func() {
		coordinator, err := NewMultimodalCoordinator(context.Background())

		So(err, ShouldBeNil)

		sensory := coordinatorValue(t, "state")
		action := coordinatorValue(t, "forward")
		reward := coordinatorValue(t, "reward")

		So(coordinator.ObserveOutcome(sensory, action, reward, 4), ShouldBeNil)

		prediction, predictErr := coordinator.PredictAction(sensory)

		So(predictErr, ShouldBeNil)
		So(prediction, ShouldNotBeNil)
		So(len(prediction.Continuations), ShouldEqual, 1)
		So(string(prediction.Continuations[0].Sequence), ShouldEqual, "forward")
	})

	Convey("PredictAction carries causal signals with policy continuations", t, func() {
		coordinator, err := NewMultimodalCoordinator(context.Background())

		So(err, ShouldBeNil)

		sensory := coordinatorValue(t, "state")
		action := coordinatorValue(t, "forward")
		reward := coordinatorValue(t, "reward")

		So(
			coordinator.ObserveOutcome(
				sensory,
				action,
				reward,
				4,
				"good",
			),
			ShouldBeNil,
		)
		So(
			coordinator.ObserveOutcome(
				sensory,
				action,
				reward,
				4,
				"excellent",
			),
			ShouldBeNil,
		)

		prediction, predictErr := coordinator.PredictAction(sensory)

		So(predictErr, ShouldBeNil)
		So(prediction, ShouldNotBeNil)
		So(len(prediction.Continuations), ShouldEqual, 1)
		So(prediction.Signals[algo.CausalStrength], ShouldNotBeNil)
		So(
			prediction.Signals[algo.CausalStrength].Value(),
			ShouldBeGreaterThan,
			0,
		)
	})
}

func BenchmarkMultimodalCoordinatorLinkKey(b *testing.B) {
	setupMarkovTrieValueConfig(b)

	coordinator, err := NewMultimodalCoordinator(context.Background())

	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for b.Loop() {
		_ = coordinator.LinkKey("sense", "action", "reward")
	}
}

func BenchmarkMultimodalCoordinatorActionScores(b *testing.B) {
	setupMarkovTrieValueConfig(b)

	coordinator, err := NewMultimodalCoordinator(context.Background())

	if err != nil {
		b.Fatal(err)
	}

	sensory := coordinatorValue(b, "state")
	action := coordinatorValue(b, "forward")
	reward := coordinatorValue(b, "good")

	if err := coordinator.ObserveOutcome(sensory, action, reward, 3); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for b.Loop() {
		_, err := coordinator.ActionScores(sensory)

		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMultimodalCoordinatorObserveOutcome(b *testing.B) {
	setupMarkovTrieValueConfig(b)

	coordinator, err := NewMultimodalCoordinator(context.Background())

	if err != nil {
		b.Fatal(err)
	}

	sensory := coordinatorValue(b, "state")
	action := coordinatorValue(b, "forward")
	reward := coordinatorValue(b, "good")

	b.ResetTimer()

	for b.Loop() {
		err := coordinator.ObserveOutcome(
			sensory,
			action,
			reward,
			3,
			"good",
		)

		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMultimodalCoordinatorObserveOutcomeInRegimes(b *testing.B) {
	setupMarkovTrieValueConfig(b)

	coordinator, err := NewMultimodalCoordinator(context.Background())

	if err != nil {
		b.Fatal(err)
	}

	sensory := coordinatorValue(b, "state")
	action := coordinatorValue(b, "forward")
	reward := coordinatorValue(b, "good")
	regimes := []string{"phase:3"}

	b.ResetTimer()

	for b.Loop() {
		err := coordinator.ObserveOutcomeInRegimes(
			sensory,
			action,
			reward,
			3,
			regimes,
			"good",
		)

		if err != nil {
			b.Fatal(err)
		}
	}
}
