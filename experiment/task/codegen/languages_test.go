package codegen

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core/algo"
)

func TestLanguagesExperimentAnswerForPrompt(t *testing.T) {
	Convey("Given a Languages prediction longer than the holdout horizon", t, func() {
		experiment := &LanguagesExperiment{
			holdouts: [][]byte{[]byte("abc")},
		}

		prediction := algo.NewPrediction()
		prediction.Continuations = append(prediction.Continuations, algo.Continuation{
			Sequence: []byte("abcdef"),
			Score:    1,
		})

		Convey("It should score the fixed completion window", func() {
			So(experiment.AnswerForPrompt(0, prediction), ShouldEqual, "abc")
		})

		Convey("It should leave prompts without a horizon untouched", func() {
			So(experiment.AnswerForPrompt(1, prediction), ShouldEqual, "abcdef")
		})
	})
}

func BenchmarkLanguagesExperimentAnswerForPrompt(b *testing.B) {
	experiment := &LanguagesExperiment{
		holdouts: [][]byte{make([]byte, 50)},
	}

	prediction := algo.NewPrediction()
	prediction.Continuations = append(prediction.Continuations, algo.Continuation{
		Sequence: make([]byte, 64),
		Score:    1,
	})

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = experiment.AnswerForPrompt(0, prediction)
	}
}
