package markovtrie

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

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
