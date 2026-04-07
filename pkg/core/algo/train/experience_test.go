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
}

func stringPtr(label string) *string {
	return &label
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
