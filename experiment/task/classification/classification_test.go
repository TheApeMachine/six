package classification

import (
	"testing"

	tools "github.com/theapemachine/six/experiment"
)

func BenchmarkTextClassificationExperiment(b *testing.B) {
	experiment := NewTextClassificationExperiment()
	experiment.tableData = append(experiment.tableData, tools.ExperimentalData{
		Holdout:        []byte("world"),
		Generation:     []byte("world"),
		Classification: []byte("world"),
		PredLabel:      tools.OptionalLabel(0),
		TrueLabel:      tools.OptionalLabel(0),
	})
	experiment.evaluator.Enrich(&experiment.tableData[0])

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		experiment.ComputePredictions()
		_ = experiment.Score()
	}
}

func BenchmarkBlindClassificationExperiment(b *testing.B) {
	experiment := NewBlindClassificationExperiment()
	experiment.tableData = append(experiment.tableData, tools.ExperimentalData{
		Holdout:        []byte("world"),
		Generation:     []byte("world"),
		Classification: []byte("world"),
		PredLabel:      tools.OptionalLabel(0),
		TrueLabel:      tools.OptionalLabel(0),
	})
	experiment.evaluator.Enrich(&experiment.tableData[0])

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = experiment.Score()
	}
}
