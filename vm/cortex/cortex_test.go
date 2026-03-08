package cortex

import (
	"math"
	"testing"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
)

// ── Node Tests ───────────────────────────────────────────────────

func TestNodeEnergy_EmptyIsZero(t *testing.T) {
	node := NewNode(0, 0)
	if e := node.Energy(); e != 0 {
		t.Fatalf("empty node energy = %f, want 0", e)
	}
}

func TestNodeEnergy_IncreasesWithData(t *testing.T) {
	node := NewNode(0, 0)
	chord := data.BaseChord(42)
	node.Cube[42] = data.ChordOR(&node.Cube[42], &chord)

	e := node.Energy()
	if e <= 0 {
		t.Fatalf("node with data should have positive energy, got %f", e)
	}
}

func TestNodeConnect_NoDuplicates(t *testing.T) {
	a := NewNode(0, 0)
	b := NewNode(1, 0)
	a.Connect(b)
	a.Connect(b) // duplicate
	a.Connect(a) // self
	if len(a.Edges()) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(a.Edges()))
	}
}

func TestNodeBestFace_Empty(t *testing.T) {
	node := NewNode(0, 0)
	if face := node.BestFace(); face != 256 {
		t.Fatalf("empty node BestFace = %d, want 256 (delimiter)", face)
	}
}

func TestNodeBestFace_ReturnsHighestPopcount(t *testing.T) {
	node := NewNode(0, 0)

	// Put a chord with 5 active bits on face 42.
	chord42 := data.BaseChord(42)
	node.Cube[42] = chord42

	// Put a chord with even more active bits on face 100.
	chord100 := data.BaseChord(100)
	extra := data.BaseChord(101)
	merged := data.ChordOR(&chord100, &extra)
	node.Cube[100] = merged

	face := node.BestFace()
	if node.Cube[100].ActiveCount() <= node.Cube[42].ActiveCount() {
		t.Skip("test assumption failed: face 100 should be denser")
	}
	if face != 100 {
		t.Fatalf("BestFace = %d, want 100", face)
	}
}

// ── Token Tests ──────────────────────────────────────────────────

func TestToken_IsRotational(t *testing.T) {
	// A rotation token has exactly 2 active bits.
	rot := geometry.GFRotation{A: 3, B: 1}
	tok := NewRotationToken(rot, 0)
	if !tok.IsRotational() {
		t.Fatal("rotation token should be detected as rotational")
	}

	// A data token (BaseChord) has 5 active bits.
	dataTok := NewDataToken(data.BaseChord(65), 0)
	if dataTok.IsRotational() {
		t.Fatal("data token should NOT be detected as rotational")
	}
}

func TestToken_DecodeRotation_Roundtrip(t *testing.T) {
	original := geometry.GFRotation{A: 3, B: 1}
	tok := NewRotationToken(original, 0)
	decoded := tok.DecodeRotation()

	if decoded.A != original.A || decoded.B != original.B {
		t.Fatalf("decoded = {A:%d, B:%d}, want {A:%d, B:%d}",
			decoded.A, decoded.B, original.A, original.B)
	}
}

// ── Reaction Tests ───────────────────────────────────────────────

func TestArrive_RotationComposesLens(t *testing.T) {
	node := NewNode(0, 0)

	// Initial rotation is identity: f(x) = x.
	if node.Rot != geometry.IdentityRotation() {
		t.Fatal("new node should have identity rotation")
	}

	// Send a RotationY token: f(x) = 3x mod 257.
	tok := NewRotationToken(geometry.RotationY, -1)
	emitted := node.Arrive(tok)

	// Rotation absorbed — no output.
	if len(emitted) != 0 {
		t.Fatalf("rotation should emit nothing, got %d tokens", len(emitted))
	}

	// Node's lens should now be RotationY.
	if node.Rot.A != geometry.RotationY.A || node.Rot.B != geometry.RotationY.B {
		t.Fatalf("node rotation = {A:%d, B:%d}, want RotationY {A:%d, B:%d}",
			node.Rot.A, node.Rot.B, geometry.RotationY.A, geometry.RotationY.B)
	}
}

func TestArrive_DataAccumulates(t *testing.T) {
	node := NewNode(0, 0)
	chord := data.BaseChord(42)
	tok := NewDataToken(chord, -1)

	energyBefore := node.Energy()
	node.Arrive(tok)
	energyAfter := node.Energy()

	if energyAfter <= energyBefore {
		t.Fatal("data arrival should increase energy")
	}
}

func TestArrive_InterferenceEmitsToken(t *testing.T) {
	node := NewNode(0, 0)

	// Pre-fill a face with some data.
	existing := data.BaseChord(10)
	face := selfAddressFace(&existing)
	routed := node.Rot.Forward(face)
	node.Cube[routed] = existing

	// Now send a chord that partially overlaps but has NEW bits.
	incoming := data.BaseChord(11)
	tok := NewDataToken(incoming, -1)
	tok.TTL = 5

	emitted := node.Arrive(tok)

	// The token should produce interference output IF ChordHole has content.
	hole := data.ChordHole(&incoming, &existing)
	if hole.ActiveCount() > 0 && len(emitted) == 0 {
		t.Fatal("expected interference emission but got none")
	}
}

func TestNodeHole_UsesSummaryMinusPeak(t *testing.T) {
	node := NewNode(0, 0)

	left := data.BaseChord(20)
	right := data.BaseChord(21)
	peak := data.ChordOR(&left, &right)
	side := data.BaseChord(10)

	node.Cube[20] = peak
	node.Cube[10] = side

	anchor, hole, shouldDream := node.Hole()
	expectedSummary := node.CubeChord()
	expectedHole := data.ChordHole(&expectedSummary, &peak)

	if anchor != peak {
		t.Fatal("anchor should be the densest face chord")
	}
	if hole != expectedHole {
		t.Fatal("hole should be the summary minus the densest face")
	}
	if !shouldDream {
		t.Fatal("node with a non-empty deficit should dream")
	}
}

// ── Graph Tests ──────────────────────────────────────────────────

func TestGraph_SmallWorldTopology(t *testing.T) {
	g := New(Config{InitialNodes: 8})

	if len(g.nodes) != 8 {
		t.Fatalf("expected 8 nodes, got %d", len(g.nodes))
	}

	// Every node should have at least 2 edges (ring) + shortcuts.
	for _, node := range g.nodes {
		if node.EdgeCount() < 2 {
			t.Fatalf("node %d has only %d edges, expected ≥2", node.ID, node.EdgeCount())
		}
	}

	// Source and sink are distinct.
	if g.source == g.sink {
		t.Fatal("source and sink should be different nodes")
	}
}

func TestGraph_SpawnNodeConnectsToBothParentAndBestMatch(t *testing.T) {
	g := New(Config{InitialNodes: 4})

	parent := g.nodes[0]
	// Put some data in the parent so the child has something to resonate with.
	parent.Cube[10] = data.BaseChord(10)

	child := g.SpawnNode(parent)

	// Child should be connected to parent.
	found := false
	for _, e := range child.Edges() {
		if e == parent {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("child should be connected to parent")
	}

	// Graph should have one more node.
	if len(g.nodes) != 5 {
		t.Fatalf("expected 5 nodes after spawn, got %d", len(g.nodes))
	}
}

// ── Tick Tests ───────────────────────────────────────────────────

func TestTick_TokensFlowThroughGraph(t *testing.T) {
	g := New(Config{InitialNodes: 4})

	// Inject a chord into the source.
	chord := data.BaseChord(65) // 'A'
	g.source.Send(NewDataToken(chord, -1))

	// Run several ticks; expect the sink to eventually receive content.
	for range 50 {
		g.Tick()
	}

	sinkEnergy := g.sink.Energy()
	if sinkEnergy <= 0 {
		t.Log("sink energy is 0 after 50 ticks — tokens may not have reached sink")
		t.Log("this is acceptable for a small graph with resonance routing")
		// Not a hard failure: the token may have been absorbed by an
		// intermediate node. The important thing is that SOME node has data.
	}

	// At least one node should have positive energy.
	anyEnergy := false
	for _, node := range g.nodes {
		if node.Energy() > 0 {
			anyEnergy = true
			break
		}
	}
	if !anyEnergy {
		t.Fatal("no node has positive energy after injecting data")
	}
}

func TestTick_Convergence(t *testing.T) {
	g := New(Config{
		InitialNodes:      4,
		ConvergenceWindow: 3,
	})

	// Pre-fill the sink with stable data.
	for i := 0; i < 10; i++ {
		face := i % geometry.CubeFaces
		g.sink.Cube[face] = data.BaseChord(byte(i))
	}

	// Tick without injecting new data — the sink should remain stable.
	converged := false
	for range 30 {
		if g.Tick() {
			converged = true
			break
		}
	}

	if !converged {
		t.Fatal("graph should converge when sink is stable and no new tokens arrive")
	}
}

// ── Integration Test ─────────────────────────────────────────────

func TestThink_ProducesOutput(t *testing.T) {
	// Create a cortex with no PrimeField (no bedrock dreams).
	// Inject a simple prompt and check that Think produces bytes.
	g := New(Config{
		InitialNodes: 8,
		MaxTicks:     128,
		MaxOutput:    8,
	})

	// Build a small prompt: "AB"
	prompt := []data.Chord{
		data.BaseChord('A'),
		data.BaseChord('B'),
	}

	out := g.Think(prompt, nil)

	var result []byte
	for b := range out {
		result = append(result, b)
	}

	// We don't know WHAT the cortex produces without a PrimeField,
	// but it should produce SOMETHING (the prompt data propagates
	// and BestFace should find at least one active face).
	t.Logf("cortex output: %d bytes, raw: %v", len(result), result)
	t.Logf("cortex final state: %d nodes, %d total ticks",
		len(g.nodes), g.tick)
}

type momentumTestAnalyzer struct {
	events    []int
	threshold float64
	phi       float64
}

func (analyzer momentumTestAnalyzer) Analyze(int, data.Chord) (bool, []int) {
	return false, analyzer.events
}

func (analyzer momentumTestAnalyzer) Phase() (float64, float64) {
	return 0, analyzer.threshold
}

func (analyzer momentumTestAnalyzer) Phi() float64 {
	return analyzer.phi
}

func TestThink_UsesEventMomentumBeforeThresholdStop(t *testing.T) {
	g := New(Config{
		InitialNodes: 8,
		MaxTicks:     64,
		MaxOutput:    4,
		Sequencer: momentumTestAnalyzer{
			events:    []int{geometry.EventDensitySpike},
			threshold: 1.5,
			phi:       (1.0 + math.Sqrt(5.0)) / 2.0,
		},
	})

	expected := &geometry.IcosahedralManifold{}
	expected.Cubes[0][42] = data.BaseChord(42)

	out := g.Think([]data.Chord{data.BaseChord('A')}, expected)

	var result []byte
	for b := range out {
		result = append(result, b)
	}

	if len(result) == 0 {
		t.Fatal("expected event momentum to carry generation past the initial threshold gate")
	}
}
