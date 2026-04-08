package classify

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestClassifierUpdate(t *testing.T) {
	t.Parallel()

	Convey("Update ignores empty context", t, func() {
		classifier := NewClassifier()
		prediction := algo.NewPrediction()
		prediction.Targets = append(prediction.Targets, algo.Label{
			Label:      []byte("L"),
			Confidence: 1,
		})

		out, err := classifier.Update(prediction)

		So(err, ShouldBeNil)
		So(len(out.Labels), ShouldEqual, 0)
	})

	Convey("Update trains then classifies repeated tokens", t, func() {
		classifier := NewClassifier()
		trainPred := algo.NewPrediction()
		trainPred.Targets = append(trainPred.Targets, algo.Label{
			Label:      []byte("pos"),
			Confidence: 1,
		})

		vTrain, err := primitive.NewValue([]byte("good data"))

		So(err, ShouldBeNil)

		defer vTrain.Close()

		trainPred.Context = append(trainPred.Context, *vTrain)

		_, err = classifier.Update(trainPred)

		So(err, ShouldBeNil)

		runPred := algo.NewPrediction()
		runPred.Context = append(runPred.Context, *vTrain)

		out, err := classifier.Update(runPred)

		So(err, ShouldBeNil)
		So(len(out.Labels), ShouldBeGreaterThan, 0)
		So(out.Label(), ShouldEqual, "pos")
	})
}

func TestClassifierValue(t *testing.T) {
	t.Parallel()

	Convey("Value clones prediction without sharing label slices", t, func() {
		classifier := NewClassifier()
		prediction := algo.NewPrediction()

		v, err := primitive.NewValue([]byte("signal"))

		So(err, ShouldBeNil)

		defer v.Close()

		prediction.Context = append(prediction.Context, *v)
		prediction.Targets = append(prediction.Targets, algo.Label{
			Label:      []byte("k"),
			Confidence: 0.9,
		})

		_, err = classifier.Update(prediction)

		So(err, ShouldBeNil)

		cloned := classifier.Value()

		So(cloned, ShouldNotBeNil)
		So(string(cloned.Labels[0].Label), ShouldEqual, "k")

		cloned.Labels[0].Label[0] = 'z'

		again := classifier.Value()

		So(string(again.Labels[0].Label), ShouldEqual, "k")
	})

	Convey("Update composes child label evidence without local context training", t, func() {
		classifier := NewClassifier()
		prediction := algo.NewPrediction()
		prediction.Labels = append(
			prediction.Labels,
			algo.Label{Label: []byte("alpha"), Confidence: 0.6},
			algo.Label{Label: []byte("beta"), Confidence: 0.2},
			algo.Label{Label: []byte("alpha"), Confidence: 0.4},
		)

		out, err := classifier.Update(prediction)

		So(err, ShouldBeNil)
		So(len(out.Labels), ShouldBeGreaterThan, 0)
		So(out.Label(), ShouldEqual, "alpha")
	})
}

func BenchmarkClassifierUpdate(b *testing.B) {
	classifier := NewClassifier()
	payload, _ := primitive.NewValue([]byte("feature token stream"))

	defer payload.Close()

	b.ResetTimer()

	for b.Loop() {
		prediction := algo.NewPrediction()
		prediction.Targets = append(prediction.Targets, algo.Label{
			Label:      []byte("lbl"),
			Confidence: 1,
		})
		prediction.Context = append(prediction.Context, *payload)

		_, _ = classifier.Update(prediction)
	}
}
