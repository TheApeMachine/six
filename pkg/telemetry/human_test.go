package telemetry

import (
	"testing"

	"github.com/theapemachine/six/pkg/primitive"
)

func TestTruthOpName(t *testing.T) {
	if TruthOpName(6) != "xor" {
		t.Fatalf("got %q", TruthOpName(6))
	}
	if TruthOpName(0xFF) != "const-1" {
		t.Fatalf("mask: got %q", TruthOpName(0xFF))
	}
}

func TestHumanDescribeValue(t *testing.T) {
	v := primitive.NewValue()
	s := HumanDescribeValue(v)
	if s == "" {
		t.Fatal("empty describe")
	}
}
