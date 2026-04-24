package cpu

import (
	"testing"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/program"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestToyWorld_CausalIntervention(t *testing.T) {
	lay := program.Layout{
		Regions: map[string]program.RegionExtent{
			"program":    {Start: 16, Words: 16},
			"signals":    {Start: 32, Words: 8},
			"context":    {Start: 40, Words: 8},
			"properties": {Start: 56, Words: 16},
			"id":         {Start: 122, Words: 1},
		},
		Properties: map[string]int{
			"ttl":          3,
			"falsified":    13,
			"continuation": 15,
		},
		Opcodes: program.Opcodes,
	}

	// Scenario: "A -> B -> C"
	// We intervene at B.
	// Expected: Causal path changes, counterfactual TTL=3 dies after 3 hops.

	src := `
	; Check if expectation (context) matches reality (signals)
	[ (properties.falsified self) <= any_zero(context -> signals) <= community ]
	
	; If falsified, decay TTL
	[ (properties.ttl self) <= (properties.ttl \ 1) ? (properties.falsified != 0) <= community ]
	
	; If TTL hits 0, halt
	[ (properties.continuation self) <= (0) ? (properties.ttl == 0) <= community ]
	`

	comp, err := program.Compile(src, lay)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	v1 := primitive.AllocValue()
	v1.StampID()
	frame := (*[128]uint64)(unsafe.Pointer(v1))

	// Copy program into frame
	copy(frame[16:32], comp.Words)

	// Set initial state
	frame[59] = 1 // TTL = 1
	frame[71] = v1.ID() // continuation = own id (keep looping)
	
	// Expectation (context) = 1 (A -> B)
	// Reality (signals) = 0 (Intervention do(B=X), making B disappear)
	frame[40] = 1 
	frame[32] = 0

	// Tick 1: evaluate falsification, decay TTL, and halt!
	// Because ExecuteCommunity now properly syncs after EACH instruction,
	// the second instruction observes falsified=1, and the third observes TTL=0.
	ExecuteCommunity([]*primitive.Value{v1})
	if frame[69] != 1 {
		t.Fatalf("expected falsified=1 after tick 1, got %d", frame[69])
	}
	if frame[59] != 0 {
		t.Fatalf("expected TTL 0 to decay in same tick, got %d", frame[59])
	}
	if frame[71] != 0 {
		t.Fatalf("expected continuation 0 (halted) when TTL hit 0 in same tick, got %d", frame[71])
	}
}

func TestToyWorld_SpawnedLineage(t *testing.T) {
	lay := program.Layout{
		Regions: map[string]program.RegionExtent{
			"program":    {Start: 16, Words: 16},
			"signals":    {Start: 32, Words: 8},
			"context":    {Start: 40, Words: 8},
			"properties": {Start: 56, Words: 16},
			"id":         {Start: 122, Words: 1},
		},
		Properties: map[string]int{
			"ttl":          3,
			"falsified":    13,
			"continuation": 15,
		},
		Opcodes: program.Opcodes,
	}

	// Spawn a new Value when falsified.
	src := `
	[ (properties.falsified self) <= any_zero(context -> signals) <= community ]
	[ (context spawn) <= (0) ? (properties.falsified != 0) <= community ]
	`

	comp, err := program.Compile(src, lay)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	v1 := primitive.AllocValue()
	v1.StampID()
	frame := (*[128]uint64)(unsafe.Pointer(v1))

	copy(frame[16:32], comp.Words)
	frame[40] = 1 
	frame[32] = 0 // falsified
	frame[59] = 10 // TTL

	// Tick 1: evaluates falsification
	ExecuteCommunity([]*primitive.Value{v1})

	// Tick 2: falsified is 1, so spawn happens
	spawned := ExecuteCommunity([]*primitive.Value{v1})
	if len(spawned) != 1 {
		t.Fatalf("expected 1 spawned value, got %d", len(spawned))
	}
	
	sFrame := (*[128]uint64)(unsafe.Pointer(spawned[0]))
	if sFrame[122] == 0 || sFrame[122] == v1.ID() {
		t.Fatalf("expected spawned value to have a new ID")
	}
	if sFrame[59] != 10 {
		t.Fatalf("expected spawned value to inherit TTL 10, got %d", sFrame[59])
	}
}
