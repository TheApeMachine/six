package program

import (
	"testing"
)

func layoutLikeConfigViper() Layout {
	return Layout{
		Regions: map[string]RegionExtent{
			"tokens":     {Start: 0, Words: 16},
			"program":    {Start: 16, Words: 16},
			"signals":    {Start: 32, Words: 8},
			"context":    {Start: 40, Words: 8},
			"gradient":   {Start: 48, Words: 8},
			"properties": {Start: 56, Words: 16},
			"asset":      {Start: 72, Words: 48},
			"prev":       {Start: 120, Words: 1},
			"next":       {Start: 121, Words: 1},
			"id":         {Start: 122, Words: 1},
			"affinity":   {Start: 123, Words: 5},
		},
		Properties: map[string]int{
			"labels": 0, "confidence": 1, "epoch": 2, "ttl": 3, "temperature": 4, "status": 5, "noise": 6, "program_id": 7,
			"community": 8, "target": 9, "role": 10, "reference": 11, "surprisal": 12, "prev_surprisal": 13, "delta_surprisal": 14,
			"stuck_count": 15, "falsified": 16, "stuck": 17, "continuation": 18,
		},
	}
}

func TestStructuralComponentProgramMatchesContract(t *testing.T) {
	t.Parallel()
	lay := layoutLikeConfigViper()
	src := `
<[
  { B(prev) B(id) ^ }
  { B(next) B(id) ^ }
] <= [
  { B(tokens) signals[0,1] ^ }
  { B(tokens) signals[1,1] ^ }
  { B(tokens) signals[2,1] ^ }
]> [
  { B(signals) }
] <= [
  { B(tokens[0,16]) B(tokens[0,16]) & }
  { B(tokens[0,16]) { B(tokens[0,16] 8 <<) } & }
]
`
	out, err := Compile(src, lay)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(out.Words) != 8 {
		t.Fatalf("expected 8 words, got %d", len(out.Words))
	}
	var cancel, merge, emit bool
	for _, word := range out.Words {
		_, _, _, _, dst, _, opcode, mode, _, _, _, _, _ := DecodeInstruction(word)
		cancel = cancel || dst == 32 && opcode == Opcodes["^"]
		merge = merge || dst >= 36 && dst < 40 && opcode == Opcodes["&"]
		emit = emit || mode == ModeEmit
	}
	if !cancel || !merge || !emit {
		t.Fatalf("expected structural component to compile cancel, merge, and emit instructions")
	}
}

func TestFeedExampleProgramMatchesContract(t *testing.T) {
	t.Parallel()
	lay := layoutLikeConfigViper()
	out, err := Compile(`[(B popcnt)] <= [(A B ^)]`, lay)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(out.Words) != 2 {
		t.Fatalf("expected 2 words, got %d", len(out.Words))
	}
}

func TestCompiler(t *testing.T) {
	lay := Layout{
		Regions: map[string]RegionExtent{
			"program":    {Start: 16, Words: 16},
			"signals":    {Start: 32, Words: 8},
			"context":    {Start: 40, Words: 8},
			"gradient":   {Start: 48, Words: 8},
			"properties": {Start: 56, Words: 16},
			"rom":        {Start: 100, Words: 16},
			"affinity":   {Start: 123, Words: 5},
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
		{
			name:  "Popcnt threshold predicate",
			src:   `[ (properties.falsified self) <= (1) ? (popcnt(affinity[0,5]) | 120) <= community ]`,
			valid: true,
		},
		{
			name:  "Bare A expression (must not eat closing bracket)",
			src:   `[ (properties.stuck self) <= A <= community ]`,
			valid: true,
		},
		{
			name:  "Saturates is not a language intrinsic",
			src:   `[ (properties.falsified self) <= saturates(affinity[0,5]) <= community ]`,
			valid: false,
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
