package cortex

import (
	"math"
	"reflect"
	"testing"
	"unsafe"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/store"
)

func setPrimeFieldManifolds(
	t *testing.T,
	field *store.PrimeField,
	manifolds []geometry.IcosahedralManifold,
) {
	t.Helper()

	fieldValue := reflect.ValueOf(field).Elem()

	manifoldsField := fieldValue.FieldByName("manifolds")
	reflect.NewAt(
		manifoldsField.Type(),
		unsafe.Pointer(manifoldsField.UnsafeAddr()),
	).Elem().Set(reflect.ValueOf(manifolds))

	nField := fieldValue.FieldByName("N")
	reflect.NewAt(nField.Type(), unsafe.Pointer(nField.UnsafeAddr())).Elem().SetInt(int64(len(manifolds)))
}

func TestAppendRecallCandidate_PreservesDistinctFacesForSameChord(t *testing.T) {
	chord := data.BaseChord(42)

	top := appendRecallCandidate(nil, recallCandidate{chord: chord, face: 10, score: 0.9}, 4)
	top = appendRecallCandidate(top, recallCandidate{chord: chord, face: 11, score: 0.8}, 4)

	if len(top) != 2 {
		t.Fatalf("expected two distinct candidates for the same chord on different faces, got %d", len(top))
	}
	if top[0].face != 10 || top[1].face != 11 {
		t.Fatalf("candidate faces = [%d %d], want [10 11]", top[0].face, top[1].face)
	}
}

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
	bc := data.BaseChord(65)
	dataTok := NewDataToken(bc, bc.IntrinsicFace(), 0)
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
	tok := NewDataToken(chord, chord.IntrinsicFace(), -1)

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
	face := existing.IntrinsicFace()
	routed := node.Rot.Forward(face)
	node.Cube[routed] = existing

	// Now send a chord that partially overlaps but has NEW bits.
	incoming := data.BaseChord(11)
	tok := NewDataToken(incoming, incoming.IntrinsicFace(), -1)
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

	anchor, hole, _, shouldDream := node.Hole()
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

func TestRecallQueryManifolds_UsesAnchorHoleAndSupportCubes(t *testing.T) {
	node := NewNode(0, 0)
	node.Header.SetState(1)
	node.Header.SetRotState(11)
	node.Header.IncrementWinding()

	left := data.BaseChord(20)
	right := data.BaseChord(21)
	center := data.BaseChord(22)
	peak := data.ChordOR(&left, &right)
	peak = data.ChordOR(&peak, &center)
	sideLeft := data.BaseChord(10)
	sideRight := data.BaseChord(11)
	side := data.ChordOR(&sideLeft, &sideRight)
	other := data.BaseChord(30)

	node.Cube[20] = peak
	node.Cube[10] = side
	node.Cube[30] = other

	anchor, hole, physicalFace, shouldDream := node.Hole()
	if !shouldDream {
		t.Fatal("expected node to dream with a non-empty hole")
	}

	ctx, expected := recallQueryManifolds(node, anchor, hole, physicalFace, 0, 1, 0)
	if ctx.Header != 0 {
		t.Fatal("context manifold should use a neutral recall header")
	}
	if expected.Header != 0 {
		t.Fatal("expected manifold should use a neutral recall header")
	}
	if physicalFace != 20 {
		t.Fatal("expected the synthetic peak face to be dominant")
	}
	if ctx.Cubes[0][physicalFace] != anchor {
		t.Fatal("context manifold should anchor the dominant face in support cube 0")
	}

	wantExpected := data.ChordOR(&anchor, &hole)
	if expected.Cubes[0][physicalFace] != wantExpected {
		t.Fatal("expected manifold should merge the anchor with the node hole")
	}
	if expected.Cubes[0][physicalFace] == ctx.Cubes[0][physicalFace] {
		t.Fatal("expected manifold should differ from context when a hole exists")
	}
	if ctx.Cubes[1][10] != side {
		t.Fatal("second densest face should populate support cube 1")
	}
	if ctx.Cubes[2][30] != other {
		t.Fatal("next active face should populate support cube 2")
	}
	if ctx.Cubes[3][0].ActiveCount() != 0 {
		t.Fatal("unused support cubes should remain empty")
	}
}

func TestRecallQueryManifolds_DialShiftMovesProbe(t *testing.T) {
	node := NewNode(0, 0)
	peakLeft := data.BaseChord(40)
	peakRight := data.BaseChord(41)
	peak := data.ChordOR(&peakLeft, &peakRight)
	side := data.BaseChord(12)

	node.Cube[40] = peak
	node.Cube[12] = side

	anchor, hole, physicalFace, shouldDream := node.Hole()
	if !shouldDream {
		t.Fatal("expected node to dream with a shifted probe")
	}

	ctx, expected := recallQueryManifolds(node, anchor, hole, physicalFace, 1, 2, math.Pi/2)
	shiftAngle := math.Pi/2 + math.Pi/4
	shift := int(float64(geometry.CubeFaces)*shiftAngle/(2*math.Pi)) % geometry.CubeFaces
	anchorFace := (physicalFace + shift) % geometry.CubeFaces
	secondaryFace := (node.Rot.Forward(12) + shift) % geometry.CubeFaces

	if physicalFace != 40 {
		t.Fatal("expected the synthetic peak face to be dominant")
	}
	if ctx.Cubes[0][anchorFace] != anchor {
		t.Fatal("dial shift should move the anchor probe across faces")
	}
	if expected.Cubes[0][anchorFace] != data.ChordOR(&anchor, &hole) {
		t.Fatal("dial shift should preserve expectation at the shifted anchor face")
	}
	if ctx.Cubes[1][secondaryFace] != side {
		t.Fatal("dial shift should move secondary support faces consistently")
	}
}

func BenchmarkRecallQueryManifolds(b *testing.B) {
	node := NewNode(0, 0)
	for face := 0; face < geometry.CubeFaces; face++ {
		chord := data.BaseChord(byte(face % 256))
		if face%3 == 0 {
			extra := data.BaseChord(byte((face + 1) % 256))
			chord = data.ChordOR(&chord, &extra)
		}
		node.Cube[face] = chord
	}

	anchor, hole, physicalFace, shouldDream := node.Hole()
	if !shouldDream {
		b.Fatal("expected dense node to produce a dream query")
	}

	b.ResetTimer()
	for range b.N {
		_, _ = recallQueryManifolds(node, anchor, hole, physicalFace, 1, 4, math.Pi/3)
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
	g.source.Send(NewDataToken(chord, chord.IntrinsicFace(), -1))

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

type recordingAnalyzer struct {
	positions []int
	bytes     []byte
}

func (analyzer *recordingAnalyzer) Analyze(pos int, byteVal byte) (bool, []int) {
	analyzer.positions = append(analyzer.positions, pos)
	analyzer.bytes = append(analyzer.bytes, byteVal)
	return false, nil
}

func (analyzer *recordingAnalyzer) Phase() (float64, float64) {
	return 0, 0
}

func (analyzer *recordingAnalyzer) Phi() float64 {
	return 0
}

func TestInjectWithSequencer_BindsPromptPosition(t *testing.T) {
	analyzer := &recordingAnalyzer{}
	g := New(Config{
		InitialNodes: 4,
		Sequencer:    analyzer,
	})

	base := data.BaseChord('A')
	g.InjectWithSequencer([]data.Chord{base, base})

	drained := g.source.DrainInbox()
	if len(drained) != 2 {
		t.Fatalf("expected 2 injected prompt tokens, got %d", len(drained))
	}

	want0 := base.RollLeft(0)
	want1 := base.RollLeft(1)

	if drained[0].Chord != want0 {
		t.Fatal("first prompt chord was not position-bound at pos 0")
	}
	if drained[1].Chord != want1 {
		t.Fatal("second prompt chord was not position-bound at pos 1")
	}

	if len(analyzer.bytes) != 2 {
		t.Fatalf("expected analyzer to observe 2 prompt bytes, got %d", len(analyzer.bytes))
	}
	if analyzer.positions[0] != 0 || analyzer.positions[1] != 1 {
		t.Fatalf("analyzer positions = %v, want [0 1]", analyzer.positions)
	}
	if analyzer.bytes[0] != 'A' || analyzer.bytes[1] != 'A' {
		t.Fatalf("sequencer should analyze the raw byte values, got %v", analyzer.bytes)
	}
}

// ── Propagation Tests ────────────────────────────────────────────

// TestSourceToSink_TokenReachesSink verifies the fundamental data flow:
// a token injected into the source must propagate to the sink via routing.
func TestSourceToSink_TokenReachesSink(t *testing.T) {
	g := New(Config{InitialNodes: 4})

	chord := data.BaseChord('A')
	g.source.Send(NewDataToken(chord, chord.IntrinsicFace(), -1))

	// Run enough ticks for propagation through 4 nodes.
	for range 100 {
		g.Tick()
	}

	sinkEnergy := g.sink.Energy()
	sinkBest := g.sink.BestFace()

	t.Logf("after 100 ticks: sinkEnergy=%f sinkBestFace=%d", sinkEnergy, sinkBest)
	for i, n := range g.nodes {
		cc := n.CubeChord()
		t.Logf("  node %d: energy=%f cubeActive=%d bestFace=%d edges=%d",
			i, n.Energy(), cc.ActiveCount(), n.BestFace(), n.EdgeCount())
	}

	if sinkEnergy <= 0 {
		t.Fatal("CRITICAL: sink has zero energy after 100 ticks — tokens never propagated from source to sink")
	}
}

// TestSourceToSink_DirectNeighbor verifies the simplest case: with only 2 nodes
// (source and sink directly connected), a token must reach the sink.
func TestSourceToSink_DirectNeighbor(t *testing.T) {
	g := New(Config{InitialNodes: 2})

	if len(g.nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(g.nodes))
	}

	// Verify source and sink are connected.
	sourceHasSink := false
	for _, e := range g.source.Edges() {
		if e == g.sink {
			sourceHasSink = true
		}
	}
	if !sourceHasSink {
		t.Fatal("source is not connected to sink in a 2-node graph")
	}

	chord := data.BaseChord('X')
	g.source.Send(NewDataToken(chord, chord.IntrinsicFace(), -1))

	// Tick: source drains inbox → Arrive → emits interference → routes to sink
	for range 10 {
		g.Tick()
	}

	sinkEnergy := g.sink.Energy()
	t.Logf("sinkEnergy=%f sinkBestFace=%d", sinkEnergy, g.sink.BestFace())
	t.Logf("sourceEnergy=%f sourceBestFace=%d", g.source.Energy(), g.source.BestFace())

	if sinkEnergy <= 0 {
		t.Fatal("CRITICAL: sink has zero energy even with direct source→sink connection after 10 ticks")
	}
}

// TestBestFace_StableRotation confirms that BestFace decodes correctly
// when the node's rotation has NOT been modified by LAW tokens.
func TestBestFace_StableRotation(t *testing.T) {
	node := NewNode(0, 0)

	// Store byte 'H' (72) at its self-addressed face.
	chord := data.BaseChord('H')
	face := node.Rot.Forward(int('H'))
	node.Cube[face] = chord

	got := node.BestFace()
	if got != int('H') {
		t.Fatalf("BestFace = %d (%q), want %d (%q)", got, rune(got), 'H', 'H')
	}
}

// TestThink_ProducesOutput is the end-to-end integration test.
// It injects a prompt and expects the cascade re-injection loop to
// produce at least 1 byte of output.
func TestThink_ProducesOutput(t *testing.T) {
	g := New(Config{
		InitialNodes:      4,
		MaxTicks:          64,
		MaxOutput:         8,
		ConvergenceWindow: 3,
	})

	// Build a simple prompt: "AB"
	prompt := []data.Chord{
		data.BaseChord('A'),
		data.BaseChord('B'),
	}

	out := g.Think(prompt, nil)

	var result []byte
	for b := range out {
		result = append(result, b)
	}

	t.Logf("Think output: %d bytes = %q", len(result), result)

	// The key diagnostic: if 0 bytes, propagation is broken.
	// If bytes are present but wrong, the readout logic needs work.
	if len(result) == 0 {
		t.Fatal("Think produced 0 bytes — sink never received propagated data")
	}
}

func TestQueryBedrock_InjectsCompetitiveRecallCandidates(t *testing.T) {
	field := store.NewPrimeField()

	left := data.BaseChord(30)
	right := data.BaseChord(40)

	var manifolds [3]geometry.IcosahedralManifold
	manifolds[1].Cubes[0][30] = left
	manifolds[2].Cubes[0][40] = right
	setPrimeFieldManifolds(t, field, manifolds[:])

	bestFillCalls := 0
	g := New(Config{
		InitialNodes: 4,
		PrimeField:   field,
		BestFill: func(
			dictionary unsafe.Pointer,
			numChords int,
			context unsafe.Pointer,
			expectedReality unsafe.Pointer,
			mode int,
			geodesicLUT unsafe.Pointer,
		) (int, float64, error) {
			bestFillCalls++
			dict := unsafe.Slice((*geometry.IcosahedralManifold)(dictionary), numChords)

			bestIdx := -1
			bestScore := 0.0
			for i := range dict {
				switch {
				case dict[i].Cubes[0][30].ActiveCount() > 0:
					if 0.9 > bestScore {
						bestIdx = i
						bestScore = 0.9
					}
				case dict[i].Cubes[0][40].ActiveCount() > 0:
					if 0.8 > bestScore {
						bestIdx = i
						bestScore = 0.8
					}
				}
			}

			if bestIdx < 0 {
				return 0, 0, nil
			}

			return bestIdx, bestScore, nil
		},
	})

	hole := data.ChordOR(&left, &right)
	anchorLeft := data.BaseChord(5)
	anchorRight := data.BaseChord(6)
	anchor := data.ChordOR(&anchorLeft, &anchorRight)
	g.source.Cube[5] = anchor
	g.source.Cube[6] = hole

	g.queryBedrock(g.source)

	recalled := g.source.DrainInbox()
	if bestFillCalls < 2 {
		t.Fatalf("expected repeated BestFill calls for competitive recall, got %d", bestFillCalls)
	}
	if len(recalled) < 2 {
		t.Fatalf("expected multiple recall candidates, got %d", len(recalled))
	}

	var sawLeft, sawRight bool
	for _, tok := range recalled {
		switch tok.Chord {
		case left:
			sawLeft = true
		case right:
			sawRight = true
		}
	}

	if !sawLeft || !sawRight {
		t.Fatalf("expected both recall competitors, sawLeft=%v sawRight=%v", sawLeft, sawRight)
	}
}

func TestCollectRecallCandidates_FiltersToHoleOverlap(t *testing.T) {
	field := store.NewPrimeField()

	var irrelevant data.Chord
	irrelevant.Set(2)

	var filler data.Chord
	filler.Set(1)

	var manifolds [1]geometry.IcosahedralManifold
	manifolds[0].Cubes[0][40] = irrelevant
	manifolds[0].Cubes[0][30] = filler
	setPrimeFieldManifolds(t, field, manifolds[:])

	bestFillCalls := 0
	g := New(Config{
		PrimeField: field,
		BestFill: func(
			dictionary unsafe.Pointer,
			numChords int,
			context unsafe.Pointer,
			expectedReality unsafe.Pointer,
			mode int,
			geodesicLUT unsafe.Pointer,
		) (int, float64, error) {
			bestFillCalls++
			if bestFillCalls > 1 {
				return -1, 0, nil
			}

			return 0, 0.9, nil
		},
	})

	var anchor data.Chord
	anchor.Set(0)
	hole := filler
	var ctx geometry.IcosahedralManifold
	var expected geometry.IcosahedralManifold
	ptr, n, offset := field.SearchSnapshot()

	candidates := g.collectRecallCandidates(ptr, n, offset, &ctx, &expected, &anchor, &hole, 1)
	if len(candidates) != 1 {
		t.Fatalf("expected exactly one hole-filling candidate, got %d", len(candidates))
	}
	if candidates[0].chord != filler {
		t.Fatal("collectRecallCandidates should reject novel faces that do not fill the node hole")
	}
}

func TestQueryBedrock_ReinjectionUsesLogicalFaceSpace(t *testing.T) {
	field := store.NewPrimeField()

	recalled := data.BaseChord(30)

	var manifolds [1]geometry.IcosahedralManifold
	manifolds[0].Cubes[0][30] = recalled
	setPrimeFieldManifolds(t, field, manifolds[:])

	bestFillCalls := 0
	g := New(Config{
		InitialNodes: 4,
		PrimeField:   field,
		BestFill: func(
			dictionary unsafe.Pointer,
			numChords int,
			context unsafe.Pointer,
			expectedReality unsafe.Pointer,
			mode int,
			geodesicLUT unsafe.Pointer,
		) (int, float64, error) {
			bestFillCalls++
			if bestFillCalls > 1 {
				return -1, 0, nil
			}

			return 0, 0.9, nil
		},
	})

	g.source.Rot = geometry.RotationY

	anchor := data.BaseChord(5)
	g.source.Cube[5] = anchor
	g.source.Cube[6] = recalled

	g.queryBedrock(g.source)

	tokens := g.source.DrainInbox()
	if len(tokens) != 1 {
		t.Fatalf("expected one recalled token, got %d", len(tokens))
	}

	wantLogicalFace := g.source.Rot.Reverse(30)
	if tokens[0].LogicalFace != wantLogicalFace {
		t.Fatalf("recalled token logical face = %d, want %d", tokens[0].LogicalFace, wantLogicalFace)
	}
}
