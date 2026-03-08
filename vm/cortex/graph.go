package cortex

import (
	"math"
	"unsafe"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/resonance"
	"github.com/theapemachine/six/store"
)

// BestFillFunc is the GPU resonance search function injected from vm.Machine.
type BestFillFunc func(
	dictionary unsafe.Pointer,
	numChords int,
	context unsafe.Pointer,
	expectedReality unsafe.Pointer,
	mode int,
	geodesicLUT unsafe.Pointer,
) (int, float64, error)

// Analyzer derives topological events from chord sequences.
// The tokenizer.Sequencer satisfies this interface.
type Analyzer interface {
	Analyze(pos int, current data.Chord) (reset bool, events []int)
	Phase() (ema float64, threshold float64)
	Phi() float64
}

/*
Config holds all initialization parameters for the cortex graph.
All fields are set by the caller (vm.Machine); the cortex never
reaches outside its own boundary.
*/
type Config struct {
	// InitialNodes is the number of nodes spawned at birth. Default: 8.
	InitialNodes int

	// PrimeField is the long-term bedrock memory (read-only during thought).
	PrimeField *store.PrimeField

	// Substrate is the geometric phase-dial memory.
	Substrate *geometry.HybridSubstrate

	// BestFill is the GPU resonance search kernel.
	BestFill BestFillFunc

	// EigenMode provides the global phase landscape.
	// When provided, it acts as a routing prior ("the wind") and checks
	// geometric closure for convergence.
	EigenMode *geometry.EigenMode

	// Sequencer derives topological events from incoming chords.
	// Events become LAW (rotation) tokens that flow through the graph,
	// shifting node perspectives. When nil, no rotational events are generated.
	Sequencer Analyzer

	// ExpectedField is the classification precision target.
	// When provided, it biases the sink's output decoding toward faces
	// that align with the expected field distribution.
	ExpectedField *geometry.ExpectedField

	// StopCh signals the cortex to abort generation.
	StopCh <-chan struct{}

	// MaxTicks is the convergence timeout. Default: 256.
	MaxTicks int

	// MaxOutput is the maximum number of bytes to generate. Default: 256.
	MaxOutput int

	// InboxSize is the channel buffer depth per node. Default: 32.
	InboxSize int

	// ConvergenceWindow is the number of consecutive ticks with stable
	// sink energy required to declare convergence. Default: 8.
	ConvergenceWindow int
}

func (c *Config) defaults() {
	if c.InitialNodes <= 0 {
		c.InitialNodes = 8
	}
	if c.MaxTicks <= 0 {
		c.MaxTicks = 256
	}
	if c.MaxOutput <= 0 {
		c.MaxOutput = 256
	}
	if c.InboxSize <= 0 {
		c.InboxSize = defaultInboxSize
	}
	if c.ConvergenceWindow <= 0 {
		c.ConvergenceWindow = 8
	}
}

// CortexSnapshot holds observability counters for the cortex.
type CortexSnapshot struct {
	TotalTicks     int
	FinalNodes     int
	SurvivorCount  int
	BedrockQueries int
	MitosisEvents  int
	PruneEvents    int
	OutputBytes    int
}

/*
Graph is the volatile working-memory cortex.
It is born from a prompt, vibrates until convergence, and dies when thought completes.
Surviving dense nodes are written back to the PrimeField as new memories.
*/
type Graph struct {
	config Config
	nodes  []*Node
	source *Node // injection point (prompt enters here)
	sink   *Node // extraction point (output read from here)
	tick   int
	nextID int

	// convergence tracking
	sinkStableCount int
	sinkLastEnergy  float64

	// Sequencer position tracking (continuous across prompt+generation)
	seqPos uint32
	seqZ   uint8

	// Momentum tracking (ported from Runner's phase decay)
	momentum float64

	// Observability counters
	bedrockQueries int
	mitosisEvents  int
	pruneEvents    int
	outputBytes    int
}

const (
	maxSelfAddressCandidates = 4
	recallCompetitionWidth   = 4
	recallInjectionLimit     = 4
	recallScoreFloor         = 0.05
)

type recallCandidate struct {
	chord data.Chord
	score float64
}

func appendRecallCandidate(top []recallCandidate, cand recallCandidate, limit int) []recallCandidate {
	for i := range top {
		if top[i].chord != cand.chord {
			continue
		}
		if cand.score <= top[i].score {
			return top
		}
		top = append(top[:i], top[i+1:]...)
		break
	}

	insertAt := len(top)
	for i := range top {
		if cand.score > top[i].score {
			insertAt = i
			break
		}
	}

	if len(top) < limit {
		top = append(top, recallCandidate{})
	}
	if insertAt < len(top) {
		copy(top[insertAt+1:], top[insertAt:len(top)-1])
		top[insertAt] = cand
	}

	if len(top) > limit {
		top = top[:limit]
	}

	return top
}

func recallQueryManifolds(node *Node, anchor, hole data.Chord, physicalFace, dial, total int, basePhase float64) (geometry.IcosahedralManifold, geometry.IcosahedralManifold) {
	var (
		ctx      geometry.IcosahedralManifold
		expected geometry.IcosahedralManifold
	)
	ctx.Header = node.Header
	expected.Header = node.Header

	shift := 0
	if dial > 0 && total > 1 {
		angle := basePhase + float64(dial)*math.Pi/float64(total*2)
		shift = int(angle*257/(2*math.Pi)) % 257
		if shift < 0 {
			shift += 257
		}
	}

	for f := 0; f < 257; f++ {
		sf := (f + shift) % 257
		ctx.Cubes[0][sf] = node.Cube[f]
		expected.Cubes[0][sf] = node.Cube[f]
	}

	return ctx, expected
}

func scoreRecallCandidate(anchor, hole, faceChord *data.Chord, matchScore float64) float64 {
	quality := resonance.FillScore(hole, faceChord)
	if quality <= 0 {
		return 0
	}

	score := quality + 0.5*matchScore
	if anchor.ActiveCount() > 0 {
		score += 0.25 * (float64(data.ChordSimilarity(anchor, faceChord)) / float64(anchor.ActiveCount()))
	}

	return score
}

func (g *Graph) collectRecallCandidates(
	dictPtr unsafe.Pointer,
	dictN int,
	dictOffset int,
	ctx *geometry.IcosahedralManifold,
	expected *geometry.IcosahedralManifold,
	anchor *data.Chord,
	hole *data.Chord,
	limit int,
) []recallCandidate {
	if limit <= 0 {
		return nil
	}

	type maskedManifold struct {
		idx      int
		manifold geometry.IcosahedralManifold
	}

	var (
		masked []maskedManifold
		top    []recallCandidate
	)

	defer func() {
		for i := len(masked) - 1; i >= 0; i-- {
			g.config.PrimeField.Unmask(masked[i].idx, masked[i].manifold)
		}
	}()

	for attempt := 0; attempt < limit; attempt++ {
		bestIdx, matchScore, err := g.config.BestFill(
			dictPtr,
			dictN,
			unsafe.Pointer(ctx),
			unsafe.Pointer(expected),
			0,
			unsafe.Pointer(&geometry.UnifiedGeodesicMatrix[0]),
		)
		if err != nil || matchScore < recallScoreFloor || bestIdx < 0 || bestIdx >= dictN {
			break
		}

		absIdx := dictOffset + bestIdx
		matched := g.config.PrimeField.Mask(absIdx)
		masked = append(masked, maskedManifold{idx: absIdx, manifold: matched})

		for cube := 0; cube < 4; cube++ {
			for face := range geometry.CubeFaces {
				faceChord := matched.Cubes[cube][face]
				if faceChord.ActiveCount() == 0 {
					continue
				}

				score := scoreRecallCandidate(anchor, hole, &faceChord, matchScore)
				if score < recallScoreFloor {
					continue
				}

				top = appendRecallCandidate(top, recallCandidate{
					chord: faceChord,
					score: score,
				}, limit)
			}
		}
	}

	return top
}

/*
New creates a cortex graph with the specified configuration.

The initial topology is a small-world network: a ring of N nodes with 2
random long-range edges per node. The source is node 0, the sink is node N-1.
*/
func New(cfg Config) *Graph {
	cfg.defaults()

	g := &Graph{
		config:   cfg,
		nodes:    make([]*Node, 0, cfg.InitialNodes),
		momentum: 0.0,
	}

	// Spawn seed nodes.
	for i := range cfg.InitialNodes {
		node := NewNode(i, 0)
		g.nodes = append(g.nodes, node)
		g.nextID = i + 1
	}

	// Ring topology: each node connects to its immediate neighbors.
	n := len(g.nodes)
	for i := range n {
		g.nodes[i].Connect(g.nodes[(i+1)%n])
		g.nodes[(i+1)%n].Connect(g.nodes[i])
	}

	// Small-world shortcuts: 2 long-range edges per node.
	for i := range n {
		far1 := (i + n/3) % n
		far2 := (i + 2*n/3) % n
		if far1 != i {
			g.nodes[i].Connect(g.nodes[far1])
			g.nodes[far1].Connect(g.nodes[i])
		}
		if far2 != i {
			g.nodes[i].Connect(g.nodes[far2])
			g.nodes[far2].Connect(g.nodes[i])
		}
	}

	g.source = g.nodes[0]
	g.sink = g.nodes[n-1]

	return g
}

// Nodes returns the current node list. Read-only view.
func (g *Graph) Nodes() []*Node { return g.nodes }

// Source returns the graph's injection point.
func (g *Graph) Source() *Node { return g.source }

// Sink returns the graph's extraction point.
func (g *Graph) Sink() *Node { return g.sink }

// Tick returns the current tick count.
func (g *Graph) TickCount() int { return g.tick }

// Snapshot returns observability counters.
func (g *Graph) Snapshot() CortexSnapshot {
	return CortexSnapshot{
		TotalTicks:     g.tick,
		FinalNodes:     len(g.nodes),
		SurvivorCount:  len(g.Survivors(0.1)),
		BedrockQueries: g.bedrockQueries,
		MitosisEvents:  g.mitosisEvents,
		PruneEvents:    g.pruneEvents,
		OutputBytes:    g.outputBytes,
	}
}

// stopped checks whether the stop channel has been signalled.
func (g *Graph) stopped() bool {
	if g.config.StopCh == nil {
		return false
	}
	select {
	case <-g.config.StopCh:
		return true
	default:
		return false
	}
}

/*
SpawnNode creates a new node, connects it bidirectionally to `parent`,
and optionally to the nearest existing node (by cube chord similarity).
This is the structural expansion mechanism triggered by density pressure.
*/
func (g *Graph) SpawnNode(parent *Node) *Node {
	child := NewNode(g.nextID, g.tick)
	g.nextID++
	g.nodes = append(g.nodes, child)
	g.mitosisEvents++

	// Bidirectional link to parent.
	parent.Connect(child)
	child.Connect(parent)

	// Find the most resonant existing node (excluding parent) and connect.
	parentSummary := parent.CubeChord()
	var bestNode *Node
	bestSim := 0
	for _, n := range g.nodes {
		if n == child || n == parent {
			continue
		}
		nSummary := n.CubeChord()
		sim := data.ChordSimilarity(&parentSummary, &nSummary)
		if sim > bestSim {
			bestSim = sim
			bestNode = n
		}
	}
	if bestNode != nil {
		child.Connect(bestNode)
		bestNode.Connect(child)
	}

	return child
}

/*
bestNeighbor selects the edge with highest resonance to the given chord.
This is "Topological Gravity" — tokens naturally flow toward nodes
whose existing content has the most constructive interference.

If EigenMode is configured, routing is also biased by the "Toroidal Wind".
*/
func (g *Graph) bestNeighbor(from *Node, c data.Chord) *Node {
	var best *Node
	bestScore := -1.0

	var tokenTheta float64
	useWind := g.config.EigenMode != nil
	if useWind {
		tokenTheta, _ = g.config.EigenMode.PhaseForChord(&c)
	}

	for _, neighbor := range from.edges {
		nSum := neighbor.CubeChord()
		sim := float64(data.ChordSimilarity(&c, &nSum))

		score := sim
		if useWind {
			neighborTheta, _ := g.config.EigenMode.PhaseForChord(&nSum)
			phaseDelta := math.Abs(tokenTheta - neighborTheta)

			// Shortest path around torus boundary
			for phaseDelta > math.Pi {
				phaseDelta = 2*math.Pi - phaseDelta
			}

			// Wind factor: 1.0 down to ~0.24 depending on phase disagreement
			wind := 1.0 / (1.0 + phaseDelta)
			score = sim * wind
		}

		if score > bestScore {
			bestScore = score
			best = neighbor
		}
	}

	return best
}

/*
queryBedrock fires multi-angle BestFill queries against the PrimeField using
the node's ChordHole as the base query. This is "Thermodynamic Suction" with
diverse torus sweep — the node dreams about what it's missing from multiple
rotational perspectives, finding memories that a single-angle query would miss.

Ported from textgen/fixture.go's RetrieveDiverse pattern:
 1. Compute the node's ChordHole.
 2. For each of nDial torus angles, rotate the query context and fire BestFill.
 3. For each match, scan ALL 257 faces for the best hole-filler (not just the
    self-addressed face).
 4. Inject the best hole-filler as a token.

When EigenMode is available, the sweep angles are biased by the node's phase
deviation from the global mean — nodes more out-of-phase explore wider angles.
*/
func (g *Graph) queryBedrock(node *Node) {
	if g.config.PrimeField == nil || g.config.BestFill == nil {
		return
	}

	anchor, hole, physicalFace, shouldDream := node.Hole()
	if !shouldDream {
		return
	}

	dictPtr, dictN, dictOffset := g.config.PrimeField.SearchSnapshot()
	if dictN == 0 {
		return
	}

	g.bedrockQueries++

	// Determine sweep parameters. More diverse sweep when EigenMode is available.
	nDial := 1
	var basePhase float64
	if g.config.EigenMode != nil {
		nDial = 4 // 4 torus angles for diverse recall
		basePhase, _ = g.config.EigenMode.PhaseForChord(&hole)
	}

	var recalled []recallCandidate

	for d := range nDial {
		ctx, expected := recallQueryManifolds(node, anchor, hole, physicalFace, d, nDial, basePhase)
		dialCandidates := g.collectRecallCandidates(
			dictPtr,
			dictN,
			dictOffset,
			&ctx,
			&expected,
			&anchor,
			&hole,
			recallCompetitionWidth,
		)
		for _, cand := range dialCandidates {
			recalled = appendRecallCandidate(recalled, cand, recallInjectionLimit)
		}
	}

	if len(recalled) == 0 {
		return
	}

	bestScore := recalled[0].score
	for _, cand := range recalled {
		if cand.score < bestScore*0.35 {
			continue
		}

		ttl := 2
		if cand.score >= bestScore*0.8 {
			ttl = 3
		}

		node.Send(Token{
			Chord:  cand.chord,
			Origin: -1,
			TTL:    ttl,
		})
	}
}

/*
WriteSurvivors commits dense surviving node cubes back to the PrimeField
as new long-term memories. This is the learning loop — what the cortex
discovered during thought persists into the bedrock.

Each face of a survivor's MacroCube is written as a chord at the face's
self-addressed position, using the node's accumulated rotational state
to derive topological events.
*/
func (g *Graph) WriteSurvivors(threshold float64) int {
	if g.config.PrimeField == nil {
		return 0
	}

	survivors := g.Survivors(threshold)
	written := 0

	for _, node := range survivors {
		for face := range geometry.CubeFaces {
			if node.Cube[face].ActiveCount() == 0 {
				continue
			}
			// Reverse the rotation to get the logical byte value.
			logicalByte := byte(node.Rot.Reverse(face))
			g.config.PrimeField.Insert(
				logicalByte,
				uint32(written),
				node.Cube[face],
				nil, // no events for consolidated memories
			)
			written++
		}
	}

	if written > 0 {
		console.Info("cortex survivors committed",
			"survivors", len(survivors),
			"facesWritten", written,
		)
	}

	return written
}
