package classification

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/store"
)

func TestTextClassificationScoreUsesMacroF1(t *testing.T) {
	Convey("Text classification score is Macro-F1 rather than raw accuracy", t, func() {
		experiment := NewTextClassificationExperiment()
		experiment.tableData = []tools.ExperimentalData{
			{TrueLabel: tools.OptionalLabel(0), PredLabel: tools.OptionalLabel(0)},
			{TrueLabel: tools.OptionalLabel(1), PredLabel: tools.OptionalLabel(0)},
			{TrueLabel: tools.OptionalLabel(2), PredLabel: tools.OptionalLabel(2)},
			{TrueLabel: tools.OptionalLabel(3), PredLabel: tools.OptionalLabel(3)},
		}
		experiment.predictionsComputed = true

		score := experiment.Score()
		metrics := experiment.evaluator.Metrics(experiment.tableData, len(experiment.tableData))

		So(score, ShouldEqual, metrics.MacroF1)
		So(score, ShouldNotEqual, metrics.Accuracy)
		So(metrics.Accuracy, ShouldAlmostEqual, 0.75, 0.000001)
		So(metrics.MacroF1, ShouldAlmostEqual, 0.8888888889, 0.000001)
	})
}

func TestTextClassificationCorpusSamples(t *testing.T) {
	Convey("Text classification stages full article plus label samples", t, func() {
		experiment := NewTextClassificationExperiment()
		experiment.prompt = []string{
			"market tumbles on earnings miss",
			"club seals dramatic playoff win",
		}
		experiment.holdouts = [][]byte{
			[]byte("business"),
			[]byte("sports"),
		}

		samples := experiment.CorpusSamples()

		So(samples, ShouldHaveLength, 2)
		So(string(samples[0]), ShouldEqual, "market tumbles on earnings miss → business")
		So(string(samples[1]), ShouldEqual, "club seals dramatic playoff win → sports")
	})
}

func TestTextClassificationObserveFromCorpus(t *testing.T) {
	Convey("Text classification recovers an exact label from staged corpus evidence", t, func() {
		store.ResetDefaultSpatialIndex()
		defer store.ResetDefaultSpatialIndex()
		backend := compute.NewBackgroundBackend()

		experiment := NewTextClassificationExperiment()
		experiment.prompt = []string{
			"market tumbles on earnings miss",
			"club seals dramatic playoff win",
		}
		experiment.holdouts = [][]byte{
			[]byte("business"),
			[]byte("sports"),
		}

		stageClassificationCorpus(t, backend, experiment, experiment.CorpusSamples())

		promptValue, err := primitive.NewValue([]byte("club seals dramatic playoff win"))
		So(err, ShouldBeNil)

		observed, err := experiment.ObserveFromCorpus([]byte("club seals dramatic playoff win"), promptValue.ID())
		So(err, ShouldBeNil)
		So(string(observed), ShouldEqual, "sports")

		cleanupClassificationValue(t, backend, promptValue, true)
	})

	Convey("Text classification excludes the live prompt value from retrieval", t, func() {
		store.ResetDefaultSpatialIndex()
		defer store.ResetDefaultSpatialIndex()
		backend := compute.NewBackgroundBackend()

		experiment := NewTextClassificationExperiment()

		promptValue, err := primitive.NewValue([]byte("club seals dramatic playoff win"))
		So(err, ShouldBeNil)

		observed, err := experiment.ObserveFromCorpus([]byte("club seals dramatic playoff win"), promptValue.ID())
		So(err, ShouldBeNil)
		So(observed, ShouldResemble, []byte{})

		cleanupClassificationValue(t, backend, promptValue, true)
	})
}

func BenchmarkTextClassificationObserveFromCorpus(b *testing.B) {
	store.ResetDefaultSpatialIndex()
	defer store.ResetDefaultSpatialIndex()
	backend := compute.NewBackgroundBackend()

	experiment := NewTextClassificationExperiment()
	experiment.prompt = []string{
		"market tumbles on earnings miss",
		"club seals dramatic playoff win",
		"orbiter returns fresh images from mars",
		"leaders debate sanctions at summit",
	}
	experiment.holdouts = [][]byte{
		[]byte("business"),
		[]byte("sports"),
		[]byte("sci_tech"),
		[]byte("world"),
	}

	stageClassificationCorpus(b, backend, experiment, experiment.CorpusSamples())

	prompt := []byte("orbiter returns fresh images from mars")

	b.ResetTimer()

	for b.Loop() {
		value, err := primitive.NewValue(prompt)
		if err != nil {
			b.Fatal(err)
		}

		if _, err := experiment.ObserveFromCorpus(prompt, value.ID()); err != nil {
			b.Fatal(err)
		}

		cleanupClassificationValue(b, backend, value, true)
	}
}

func stageClassificationCorpus(tb testing.TB, backend *compute.Backend, experiment *TextClassificationExperiment, samples [][]byte) {
	tb.Helper()

	for _, sample := range samples {
		value, err := primitive.NewValue(sample)
		if err != nil {
			tb.Fatal(err)
		}

		experiment.RegisterCorpusSample(value.ID(), sample)
		cleanupClassificationValue(tb, backend, value, false)
	}
}

func cleanupClassificationValue(tb testing.TB, backend *compute.Backend, value *primitive.Value, removeFromStore bool) {
	tb.Helper()

	if removeFromStore {
		store.DefaultSpatialIndex().RemoveValueIDImmediate(value.ID())
	}

	value.InstallFirmware(core.FirmwareTypeTombstone)

	if err := value.Close(); err != nil {
		tb.Fatal(err)
	}
}
