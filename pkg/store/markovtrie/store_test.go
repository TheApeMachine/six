package markovtrie

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/numeric/gf"
	"github.com/theapemachine/six/pkg/primitive"
)

type stubAlgorithm struct {
	prediction *algo.Prediction
	updates    int
}

func (stub *stubAlgorithm) Value() *algo.Prediction {
	if stub == nil {
		return nil
	}

	return stub.prediction
}

func (stub *stubAlgorithm) Update(
	prediction *algo.Prediction,
) (*algo.Prediction, error) {
	if stub == nil {
		return nil, nil
	}

	stub.updates++

	return stub.prediction, nil
}

func TestNewStore(t *testing.T) {
	setupMarkovTrieValueConfig(t)

	t.Parallel()

	Convey("nil context panics (WithCancel before Require)", t, func() {
		defer func() {
			So(recover(), ShouldNotBeNil)
		}()

		//lint:ignore SA1012 deliberate nil parent: NewStore calls WithCancel before validate (see recover above).
		_, _ = NewStore(nil, primitive.Affinity{})
	})

	Convey("valid ctx returns Store with algorithm stack", t, func() {
		store, err := NewStore(context.Background(), primitive.Affinity{})

		So(err, ShouldBeNil)
		So(store, ShouldNotBeNil)
		So(len(store.Algorithms()), ShouldBeGreaterThan, 2)
	})

	Convey("WithAlgorithms replaces the default stack", t, func() {
		stub := &stubAlgorithm{
			prediction: algo.NewPrediction(),
		}

		store, err := NewStore(
			context.Background(),
			primitive.Affinity{},
			WithAlgorithms(stub),
		)

		So(err, ShouldBeNil)
		So(len(store.Algorithms()), ShouldEqual, 1)
		So(store.Algorithms()[0], ShouldEqual, stub)
	})
}

func TestStoreLoad(t *testing.T) {
	setupMarkovTrieValueConfig(t)

	t.Parallel()

	Convey("Load nil store is a no-op", t, func() {
		var store *Store

		So(store.Load(primitive.Value{}), ShouldBeNil)
	})

	Convey("sequential Load shares edges under lastLeaf threading", t, func() {
		store, err := NewStore(context.Background(), primitive.Affinity{})

		So(err, ShouldBeNil)

		first, vErr := primitive.FirstSegment(primitive.NewValue([]byte("aa")))

		So(vErr, ShouldBeNil)

		defer first.Close()

		second, vErr := primitive.FirstSegment(primitive.NewValue([]byte("bb")))

		So(vErr, ShouldBeNil)

		defer second.Close()

		So(store.Load(*first), ShouldBeNil)
		So(store.Load(*second), ShouldBeNil)

		So(store.root.Child(trieEdgeKey(*first)), ShouldNotBeNil)
		So(store.root.Child(trieEdgeKey(*first)).Child(trieEdgeKey(*second)), ShouldNotBeNil)
	})

	Convey("Load with labels resets lastLeaf", t, func() {
		store, err := NewStore(context.Background(), primitive.Affinity{})

		So(err, ShouldBeNil)

		tok, vErr := primitive.FirstSegment(primitive.NewValue([]byte("tagged")))

		So(vErr, ShouldBeNil)

		defer tok.Close()

		So(store.Load(*tok, "lbl"), ShouldBeNil)

		So(store.lastLeaf.Load(), ShouldBeNil)
	})

	Convey("Load updates the trie-local phase vector", t, func() {
		store, err := NewStore(context.Background(), primitive.Affinity{})

		So(err, ShouldBeNil)

		tokenValue, valueErr := primitive.FirstSegment(primitive.NewValue([]byte("phase-tok")))

		So(valueErr, ShouldBeNil)

		defer tokenValue.Close()

		So(store.Load(*tokenValue), ShouldBeNil)
		So(store.LocalPhase().Dominant().Amplitude, ShouldBeGreaterThan, 0)
	})
}

func TestStorePredict(t *testing.T) {
	setupMarkovTrieValueConfig(t)

	t.Parallel()

	Convey("Predict on nil store returns empty prediction", t, func() {
		var store *Store

		pred, err := store.Predict(primitive.Value{})

		So(err, ShouldBeNil)
		So(pred, ShouldNotBeNil)
	})

	Convey("Predict walks loaded path", t, func() {
		store, err := NewStore(context.Background(), primitive.Affinity{})

		So(err, ShouldBeNil)

		tok, vErr := primitive.FirstSegment(primitive.NewValue([]byte("walk-me")))

		So(vErr, ShouldBeNil)

		defer tok.Close()

		So(store.Load(*tok), ShouldBeNil)

		pred, err := store.Predict(*tok)

		So(err, ShouldBeNil)
		So(pred, ShouldNotBeNil)
	})
}

func TestStoreSignals(t *testing.T) {
	setupMarkovTrieValueConfig(t)

	t.Parallel()

	Convey("Signals flattens algorithm map keys", t, func() {
		store, err := NewStore(context.Background(), primitive.Affinity{})

		So(err, ShouldBeNil)

		signals := store.Signals()

		So(signals, ShouldNotBeNil)
	})
}

func TestStoreSignal(t *testing.T) {
	setupMarkovTrieValueConfig(t)

	t.Parallel()

	Convey("Signal returns 0 for unknown keys on fresh store", t, func() {
		store, err := NewStore(context.Background(), primitive.Affinity{})

		So(err, ShouldBeNil)

		So(store.Signal(algo.SignalType(0xbadf00d)), ShouldEqual, 0)
	})

	Convey("Signal reads live algorithm channels after labeled Load", t, func() {
		store, err := NewStore(context.Background(), primitive.Affinity{})

		So(err, ShouldBeNil)

		tok, vErr := primitive.FirstSegment(primitive.NewValue([]byte("signal-tok")))

		So(vErr, ShouldBeNil)

		defer tok.Close()

		So(store.Load(*tok, "alpha"), ShouldBeNil)

		seen := false

		for _, algorithm := range store.Algorithms() {
			pred := algorithm.Value()

			if pred == nil || pred.Signals == nil {
				continue
			}

			for signalKey := range pred.Signals {
				if store.Signal(signalKey) != 0 || pred.Signals[signalKey].Value() != 0 {
					seen = true

					break
				}
			}

			if seen {
				break
			}
		}

		So(seen, ShouldBeTrue)
	})
}

func TestStoreAlgorithms(t *testing.T) {
	setupMarkovTrieValueConfig(t)

	t.Parallel()

	Convey("Algorithms exposes the configured stack length", t, func() {
		store, err := NewStore(context.Background(), primitive.Affinity{})

		So(err, ShouldBeNil)

		So(len(store.Algorithms()), ShouldEqual, 4)
	})
}

func TestStoreObserve(t *testing.T) {
	setupMarkovTrieValueConfig(t)

	t.Parallel()

	Convey("Observe forwards envelopes through the configured stack", t, func() {
		stub := &stubAlgorithm{
			prediction: algo.NewPrediction(),
		}

		store, err := NewStore(
			context.Background(),
			primitive.Affinity{},
			WithAlgorithms(stub),
		)

		So(err, ShouldBeNil)
		So(store.Observe(algo.NewPrediction()), ShouldBeNil)
		So(stub.updates, ShouldEqual, 1)
	})
}

func TestStoreApplyFieldPressure(t *testing.T) {
	setupMarkovTrieValueConfig(t)

	t.Parallel()

	Convey("ApplyFieldPressure on nil store is a no-op", t, func() {
		var store *Store

		So(store.ApplyFieldPressure(1, 1, 1, 0, 0), ShouldBeNil)
	})

	Convey("ApplyFieldPressure runs stack on live store", t, func() {
		store, err := NewStore(context.Background(), primitive.Affinity{})

		So(err, ShouldBeNil)

		beforePhase := store.LocalPhase()

		So(store.ApplyFieldPressure(0.1, 0.2, 0.9, 32, 1), ShouldBeNil)
		So(store.LocalPhase(), ShouldNotResemble, beforePhase)
	})
}

func TestStoreLocalPhase(t *testing.T) {
	setupMarkovTrieValueConfig(t)

	t.Parallel()

	Convey("LocalPhase on a fresh store is zeroed", t, func() {
		store, err := NewStore(context.Background(), primitive.Affinity{})

		So(err, ShouldBeNil)
		So(store.LocalPhase(), ShouldResemble, gf.Vector257{})
	})
}

func BenchmarkStoreLoadSequential(b *testing.B) {
	setupMarkovTrieValueConfig(b)

	store, err := NewStore(context.Background(), primitive.Affinity{})

	if err != nil {
		b.Fatal(err)
	}

	tok, err := primitive.FirstSegment(primitive.NewValue([]byte("bench-load")))

	if err != nil {
		b.Fatal(err)
	}

	defer tok.Close()

	stack := *tok

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = store.Load(stack)
	}
}

func BenchmarkStoreSignals(b *testing.B) {
	setupMarkovTrieValueConfig(b)

	store, err := NewStore(context.Background(), primitive.Affinity{})

	if err != nil {
		b.Fatal(err)
	}

	tok, err := primitive.FirstSegment(primitive.NewValue([]byte("sig-bench")))

	if err != nil {
		b.Fatal(err)
	}

	defer tok.Close()

	if err := store.Load(*tok, "bench-label"); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for b.Loop() {
		_ = store.Signals()
	}
}

func BenchmarkStorePredict(b *testing.B) {
	setupMarkovTrieValueConfig(b)

	store, err := NewStore(context.Background(), primitive.Affinity{})

	if err != nil {
		b.Fatal(err)
	}

	tok, err := primitive.FirstSegment(primitive.NewValue([]byte("bench-predict")))

	if err != nil {
		b.Fatal(err)
	}

	defer tok.Close()

	if err := store.Load(*tok); err != nil {
		b.Fatal(err)
	}

	stack := *tok

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_, _ = store.Predict(stack)
	}
}
