package cortex

import (
	"math"
	"sort"
	"unsafe"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/numeric"
	"github.com/theapemachine/six/store"
)

/*
BestFillFunc is the GPU resonance search; injected from vm.Machine.
Returns (bestManifoldIndex, score, error).
*/
type BestFillFunc func(
	dictionary unsafe.Pointer,
	numChords int,
	context unsafe.Pointer,
	expectedReality unsafe.Pointer,
	mode int,
	geodesicLUT unsafe.Pointer,
) (int, float64, error)

/*
Analyzer produces (reset, events) from (pos, byte). tokenizer.Sequencer implements it.
*/
type Analyzer interface {
	Analyze(pos int, byteVal byte) (reset bool, events []int)
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

/*
CortexSnapshot holds tick/node/query counts for observability.
*/
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
Graph is the cortex: source/sink nodes, ring+small-world topology.
Runs Tick() until convergence; survivors with Energy ≥ threshold are writable to PrimeField.
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

	// Pre-allocated tick buffers (zero GC pressure).
	emitBuf  []tickEmission
	dreamBuf []tickDreamCand
	chordBuf []data.Chord
}

// tickEmission groups a node with its emitted tokens from a single tick.
type tickEmission struct {
	from   *Node
	tokens []Token
}

// tickDreamCand pairs a node with its phase deviation for dream prioritization.
type tickDreamCand struct {
	node *Node
	dev  float64
}

const (
	maxSelfAddressCandidates = 4
	recallCompetitionWidth   = 4
	recallInjectionLimit     = 4
	recallMinFixedScore      = 1024.0
	recallScoreFloor         = recallMinFixedScore / numeric.ScoreScale
)

type recallCandidate struct {
	chord data.Chord
	score float64
	face  int
}

type recallCandidateKey struct {
	chord data.Chord
	face  int
}

func appendRecallCandidate(top []recallCandidate, cand recallCandidate, limit int) []recallCandidate {
	for i := range top {
		if top[i].chord != cand.chord || top[i].face != cand.face {
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
	ctx.Header = 0
	expected.Header = 0

	if physicalFace < 0 || physicalFace >= geometry.CubeFaces {
		return ctx, expected
	}

	shift := 0
	if dial > 0 && total > 1 {
		angle := basePhase + float64(dial)*math.Pi/float64(total*2)
		shift = int(angle*257/(2*math.Pi)) % 257
		if shift < 0 {
			shift += 257
		}
	}

	type faceDensity struct {
		face   int
		active int
	}

	shiftedFace := func(face int) int {
		return (face + shift) % geometry.CubeFaces
	}

	anchorFace := shiftedFace(physicalFace)
	anchorExpected := data.ChordOR(&anchor, &hole)
	for cube := 0; cube < 4; cube++ {
		ctx.Cubes[cube][anchorFace] = anchor
		expected.Cubes[cube][anchorFace] = anchorExpected
	}

	var ranked []faceDensity
	for face := range geometry.CubeFaces {
		if face == physicalFace {
			continue
		}

		active := node.Cube[face].ActiveCount()
		if active == 0 {
			continue
		}

		ranked = append(ranked, faceDensity{face: face, active: active})
	}

	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].active == ranked[j].active {
			return ranked[i].face < ranked[j].face
		}

		return ranked[i].active > ranked[j].active
	})

	for idx, faceDensity := range ranked {
		if idx >= 3 {
			break
		}

		face := faceDensity.face
		shifted := shiftedFace(face)
		for cube := 0; cube < 4; cube++ {
			ctx.Cubes[cube][shifted] = node.Cube[face]
			expected.Cubes[cube][shifted] = node.Cube[face]
		}
	}

	return ctx, expected
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

		for cube := range 4 {
			for face := range geometry.CubeFaces {
				faceChord := matched.Cubes[cube][face]
				if faceChord.ActiveCount() == 0 {
					continue
				}

				novel := data.ChordHole(&faceChord, anchor)
				if novel.ActiveCount() == 0 {
					continue
				}

				filler := data.ChordGCD(&novel, hole)
				if filler.ActiveCount() == 0 {
					continue
				}

				score := matchScore
				if filler.ActiveCount() > 0 {
					score += 0.05 * float64(filler.ActiveCount())
				}
				if score < recallScoreFloor {
					continue
				}

				// Do not artificially cap the manifold extraction.
				top = append(top, recallCandidate{
					chord: faceChord,
					score: score,
					face:  face,
				})
			}
		}
	}

	// Sort by score descending
	sort.Slice(top, func(i, j int) bool {
		return top[i].score > top[j].score
	})

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

/*
Nodes returns the current node list.
*/
func (g *Graph) Nodes() []*Node { return g.nodes }

/*
Source returns the prompt injection node.
*/
func (g *Graph) Source() *Node { return g.source }

/*
Sink returns the output extraction node.
*/
func (g *Graph) Sink() *Node { return g.sink }

/*
TickCount returns the number of Tick() calls completed.
*/
func (g *Graph) TickCount() int { return g.tick }

/*
Snapshot returns observability counters.
*/
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
bestNeighbor returns the neighbor with highest ChordSimilarity to c.
If EigenMode is set, score is scaled by 1/(1+phaseDelta) (phase-aligned routing).
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
routeTargets returns the set of nodes a token should be sent to.

When the routing medium has structure (at least one neighbor has positive
resonance), this returns a single best neighbor — directional gravity.

When all neighbors score zero (unstructured medium), this returns ALL
neighbors — omnidirectional wave propagation. A perturbation in an
unstructured medium spreads in all directions.
*/
func (g *Graph) routeTargets(from *Node, c data.Chord) []*Node {
	var best *Node
	bestScore := -1.0
	allZero := true

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
			for phaseDelta > math.Pi {
				phaseDelta = 2*math.Pi - phaseDelta
			}
			wind := 1.0 / (1.0 + phaseDelta)
			score = sim * wind
		}

		if score > 0 {
			allZero = false
		}
		if score > bestScore {
			bestScore = score
			best = neighbor
		}
	}

	if allZero {
		// Degenerate case: broadcast to all neighbors.
		return from.edges
	}

	if best != nil {
		return []*Node{best}
	}
	return nil
}

/*
queryBedrock runs BestFill over PrimeField using the node's Hole (anchor, hole).
nDial sweep angles when EigenMode is set (4 angles); otherwise single query.
For each match, collects recall candidates (ChordHole filler) and sends top
scorers as tokens. Nodes with higher phase deviation from global mean use
wider sweep.
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
	seen := make(map[recallCandidateKey]bool)

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
			key := recallCandidateKey{chord: cand.chord, face: cand.face}
			if !seen[key] {
				seen[key] = true
				recalled = append(recalled, cand)
			}
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
			Chord:       cand.chord,
			LogicalFace: node.Rot.Reverse(cand.face),
			Origin:      -1,
			TTL:         ttl,
		})

		console.Info("bedrock recall",
			"node", node.ID,
			"score", cand.score,
			"chord_active", cand.chord.ActiveCount(),
		)
	}
}

/*
WriteSurvivors inserts each survivor's Cube faces into PrimeField.
Face at physical index i → Insert(Rot.Reverse(i), ...). Returns count of faces written.
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

/*
Wipe clears all 257 faces of the node's working memory.
Preserves internal rotation lens and birth tick.
*/
func (n *Node) Wipe() {
	for i := range n.Cube {
		n.Cube[i] = data.Chord{}
	}
	n.InvalidateChordCache()
}

/*
Wipe clears the working memory (MacroCube) of all nodes in the graph.
*/
func (g *Graph) Wipe() {
	for _, n := range g.nodes {
		n.Wipe()
	}
}

/*
WipeFace clears a specific logical face across all nodes in the graph.
*/
func (g *Graph) WipeFace(logicalFace int) {
	for _, n := range g.nodes {
		n.WipeFace(logicalFace)
	}
}
