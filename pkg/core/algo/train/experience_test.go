package train

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/algo/classify"
)

type stubSurprisal struct{}

func (stubSurprisal) SurprisalSeries(sequence string) []SurprisalItem {
	return []SurprisalItem{
		{Token: sequence, Bits: 2},
	}
}

type stubScores struct{}

func (stubScores) ClassifyScores(sequence string) map[string]float64 {
	return map[string]float64{
		"auto": 0.99,
	}
}

type stubSink struct {
	lastSeq   string
	lastLabel string
}

func (sink *stubSink) TrainStep(sequence string, label string, learningRate float64) {
	_ = learningRate

	sink.lastSeq = sequence
	sink.lastLabel = label
}

func TestNewExperience(t *testing.T) {
	t.Parallel()

	Convey("NewExperience rejects nil Online", t, func() {
		_, err := NewExperience(nil, classify.NewClassifier(), nil, nil, nil)

		So(err, ShouldNotBeNil)
	})
}

func TestExperienceRun(t *testing.T) {
	t.Parallel()

	Convey("Run returns empty label when surprisal series empty", t, func() {
		online := NewOnline()
		exp, err := NewExperience(
			online,
			classify.NewClassifier(),
			stubSurprisalSeries(nil),
			stubScores{},
			&stubSink{},
		)

		So(err, ShouldBeNil)

		result := exp.Run("hello", nil)

		So(result.Label, ShouldEqual, core.Cfg.MarkovTrie.ExperienceEmptyLabel)
	})

	Convey("Run forwards training sink with derived rate", t, func() {
		online := NewOnline()
		sink := &stubSink{}

		exp, err := NewExperience(
			online,
			classify.NewClassifier(),
			stubSurprisal{},
			stubScores{},
			sink,
		)

		So(err, ShouldBeNil)

		result := exp.Run("hello", stringPtr("supervised"))

		So(sink.lastSeq, ShouldEqual, "hello")
		So(sink.lastLabel, ShouldEqual, "supervised")
		So(result.LearningRate, ShouldBeGreaterThan, 0)
	})

	Convey("Sustained high surprisal raises learning rate versus an isolated spike", t, func() {
		online := NewOnline()
		sink := &stubSink{}

		// Keep mean surprisal low enough that MaxLearningRate does not flatten
		// both cold and boosted paths — we only want to observe the plasticity
		// multiplier from sustained bursts.
		burstStub := &rampSurprisalStub{lo: 0.1, hi: 0.8}

		expBoosted, err := NewExperience(
			online,
			classify.NewClassifier(),
			burstStub,
			stubScores{},
			sink,
		)

		So(err, ShouldBeNil)

		for range 3 {
			_ = expBoosted.Run("burst", stringPtr("supervised"))
		}

		onlineCold := NewOnline()
		coldSink := &stubSink{}

		expCold, errCold := NewExperience(
			onlineCold,
			classify.NewClassifier(),
			stubSurprisalSeries(func(string) []SurprisalItem {
				return []SurprisalItem{{Token: "x", Bits: 0.8}}
			}),
			stubScores{},
			coldSink,
		)

		So(errCold, ShouldBeNil)

		cold := expCold.Run("once", stringPtr("supervised"))
		boosted := expBoosted.Run("burst", stringPtr("supervised"))

		So(boosted.LearningRate, ShouldBeGreaterThan, cold.LearningRate)
	})
}

func stringPtr(label string) *string {
	return &label
}

type rampSurprisalStub struct {
	lo float64
	hi float64
	n  int
}

func (stub *rampSurprisalStub) SurprisalSeries(sequence string) []SurprisalItem {
	stub.n++

	bits := stub.hi

	if stub.n == 1 {
		bits = stub.lo
	}

	return []SurprisalItem{
		{Token: sequence, Bits: bits},
	}
}

type stubSurprisalSeries func(string) []SurprisalItem

func (stub stubSurprisalSeries) SurprisalSeries(sequence string) []SurprisalItem {
	if stub == nil {
		return nil
	}

	return stub(sequence)
}

func BenchmarkExperienceRun(b *testing.B) {
	online := NewOnline()
	sink := &stubSink{}

	exp, err := NewExperience(
		online,
		classify.NewClassifier(),
		stubSurprisal{},
		stubScores{},
		sink,
	)

	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for b.Loop() {
		_ = exp.Run("benchmark corpus line", stringPtr("lbl"))
	}
}
