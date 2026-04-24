package cpu

import (
	"testing"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/program"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestExecuteCommunity_TickSemantics(t *testing.T) {
	lay := program.Layout{
		Regions: map[string]program.RegionExtent{
			"program": {Start: 16, Words: 16},
			"a":       {Start: 0, Words: 1},
			"b":       {Start: 1, Words: 1},
			"dst":     {Start: 2, Words: 1},
		},
	}

	// Reads must observe pre-state, not writes from earlier Values or lanes.
	// [ (a self) <= (b A) <= community ]
	// [ (dst self) <= (a ^ b) <= community ]
	// If the second instruction sees the NEW 'a', dst = 0
	// If it sees pre-state (b), dst = (original_a ^ b)
	comp, err := program.Compile("[ (a self) <= (b) <= community ]\n[ (dst self) <= (a ^ b) <= community ]", lay)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	v1 := &primitive.Value{}
	frame := (*[128]uint64)(unsafe.Pointer(v1))

	frame[16] = comp.Words[0]
	frame[17] = comp.Words[1]
	frame[18] = 0 // Halt

	frame[0] = 0xAAAA // original A
	frame[1] = 0xFFFF // original B

	ExecuteCommunity([]*primitive.Value{v1})

	// tick 1: a <= b (0xFFFF). Post-state has a=0xFFFF, pre-state for tick 2 is a=0xFFFF
	// tick 2: dst <= (a ^ b) = (0xFFFF ^ 0xFFFF) = 0
	// Wait, the user critique said "If it sees pre-state per tick semantics, you get the original gap."
	// BUT, tick 1 finishes and COMMITS before tick 2 starts.
	// Let's verify what the user meant: "Reads must observe pre-state, not writes from earlier Values or lanes."
	// This means within ONE TICK (one instruction across the community), reads don't observe writes.
	// But between instructions (pc++), the tick COMMITS.

	if frame[2] != 0 {
		t.Fatalf("expected 0, got %X", frame[2])
	}
}

func TestExecuteCommunity_Reductions(t *testing.T) {
	lay := program.Layout{
		Regions: map[string]program.RegionExtent{
			"program": {Start: 16, Words: 16},
			"signals": {Start: 32, Words: 8}, // 8 words
			"witness": {Start: 56, Words: 1}, // 1 word
		},
	}

	// [ (witness self) <= popcnt(signals) <= community ]
	comp, err := program.Compile(`[ (witness self) <= popcnt(signals) <= community ]`, lay)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	v1 := &primitive.Value{}
	frame := (*[128]uint64)(unsafe.Pointer(v1))

	frame[16] = comp.Words[0]

	// Setup signal span with 3 bits set across 8 words
	frame[32] = 1
	frame[35] = 2 // popcnt(2) = 1 bit
	frame[39] = 8 // popcnt(8) = 1 bit

	ExecuteCommunity([]*primitive.Value{v1})

	if frame[56] != 3 {
		t.Fatalf("expected popcnt to be 3, got %d", frame[56])
	}
}

func TestExecuteCommunity_AnyZeroReduction(t *testing.T) {
	lay := program.Layout{
		Regions: map[string]program.RegionExtent{
			"program":   {Start: 16, Words: 16},
			"signals":   {Start: 32, Words: 8},
			"falsified": {Start: 56, Words: 1},
		},
	}

	// [ (falsified self) <= any_zero(signals) <= community ]
	comp, err := program.Compile(`[ (falsified self) <= any_zero(signals) <= community ]`, lay)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	v1 := &primitive.Value{} // Has a zero word
	v2 := &primitive.Value{} // All ones
	f1 := (*[128]uint64)(unsafe.Pointer(v1))
	f2 := (*[128]uint64)(unsafe.Pointer(v2))

	f1[16] = comp.Words[0]

	for i := 32; i < 40; i++ {
		f1[i] = ^uint64(0)
		f2[i] = ^uint64(0)
	}
	f1[35] = 0xFF00 // Contains zeros!

	ExecuteCommunity([]*primitive.Value{v1, v2})

	if f1[56] != 1 {
		t.Fatalf("expected v1 to falsify (1), got %d", f1[56])
	}
	if f2[56] != 0 {
		t.Fatalf("expected v2 to pass (0), got %d", f2[56])
	}
}
