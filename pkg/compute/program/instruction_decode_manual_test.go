package program

import "testing"

func TestDecodeInstructionPredFields(t *testing.T) {
	const w = uint64(2414778202105355972)
	_, _, _, _, _, _, _, _, _, predStart, predCond, _, _ := DecodeInstruction(w)
	if predStart != 3 {
		t.Fatalf("predStart=%d want 3", predStart)
	}
	if predCond != 3 {
		t.Fatalf("predCond=%d want 3", predCond)
	}
}
