package classification

import (
	"testing"

	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestTextClassificationAddResultMapsSubstrateLabelOneToClassZero(t *testing.T) {
	experiment := NewTextClassificationExperiment()
	value := primitive.Emit(primitive.WithLabels(1))
	defer value.Close()

	experiment.AddResult(tools.ExperimentalData{
		Idx:      0,
		Resolved: []*primitive.Value{value},
	})

	rows := experiment.TableData().([]tools.ExperimentalData)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].PredLabel == nil {
		t.Fatal("PredLabel is nil, want class 0")
	}
	if got := int(*rows[0].PredLabel); got != 0 {
		t.Fatalf("PredLabel = %d, want 0", got)
	}
}
