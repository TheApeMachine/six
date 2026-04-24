package program

import (
	"testing"
)

func TestCompiler(t *testing.T) {
	lay := Layout{
		Regions: map[string]RegionExtent{
			"program":    {Start: 16, Words: 16},
			"signals":    {Start: 32, Words: 8},
			"context":    {Start: 40, Words: 8},
			"gradient":   {Start: 48, Words: 8},
			"properties": {Start: 56, Words: 16},
			"rom":        {Start: 100, Words: 16},
		},
		Properties: map[string]int{
			"stuck":        14,
			"falsified":    13,
			"unsupervised": 0,
		},
	}

	tests := []struct {
		name  string
		src   string
		valid bool
	}{
		{
			name:  "Basic Local Sweep",
			src:   `[ (16..24 self) <= (0..8 ^ 8..16) <= (0..n) ]`,
			valid: true,
		},
		{
			name:  "Predicated Fold",
			src:   `[ (gradient fold) <= (0..8 ^ context) ? (properties.falsified != 0) <= community ]`,
			valid: true,
		},
		{
			name:  "Reduction",
			src:   `[ (properties.falsified self) <= any_zero(16..24 -> context) <= community ]`,
			valid: true,
		},
		{
			name:  "Immediate with predicate",
			src:   `[ (program self) <= (rom.unsupervised | 0) ? (properties.stuck != 0) <= community ]`,
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiled, err := Compile(tt.src, lay)
			if tt.valid && err != nil {
				t.Fatalf("expected valid, got error: %v", err)
			}
			if !tt.valid && err == nil {
				t.Fatalf("expected error, got valid")
			}
			if tt.valid && len(compiled.Words) != 1 {
				t.Fatalf("expected 1 word, got %d", len(compiled.Words))
			}
		})
	}
}
