package causal

import (
	"math"
	"sync"
)

/*
Discovery implements the Peter-Clark (PC) algorithm for constraint-based
causal structure learning. Unlike the invariance heuristic in Graph
(which approximates causal strength from edge stability across labels),
Discovery performs formal conditional independence testing to construct
a causal skeleton, then orients edges using v-structure detection and
Meek's orientation rules.

The algorithm operates over the same edge statistics that Graph
accumulates — no additional data collection. Discovery reads the
per-label conditional distributions from Graph's edges and performs
G-tests (log-likelihood ratio) for conditional independence.

Usage: call Learn periodically (e.g. every N trie epochs) to refine
the causal structure. The skeleton and orientations are incrementally
updated — edges removed stay removed until the graph is reset.
*/
type Discovery struct {
	mu        sync.RWMutex
	graph     *Graph
	skeleton  map[edgeKey]bool
	sepSets   map[undirectedKey][]uint64
	oriented  map[edgeKey]Direction
	nodeIDs   map[uint64]bool
	neighbors map[uint64]map[uint64]bool
}

/*
Direction encodes arrow orientation for a causal edge.
*/
type Direction uint8

const (
	Undirected Direction = iota
	Forward
	Backward
)

/*
undirectedKey is an unordered pair of Value IDs for separation set storage.
The smaller ID is always stored in lo.
*/
type undirectedKey struct {
	lo uint64
	hi uint64
}

func makeUndirectedKey(a, b uint64) undirectedKey {
	if a > b {
		a, b = b, a
	}

	return undirectedKey{lo: a, hi: b}
}

/*
NewDiscovery constructs a causal discovery engine that operates over
the given Graph's accumulated edge statistics.
*/
func NewDiscovery(graph *Graph) *Discovery {
	return &Discovery{
		graph:     graph,
		skeleton:  make(map[edgeKey]bool),
		sepSets:   make(map[undirectedKey][]uint64),
		oriented:  make(map[edgeKey]Direction),
		nodeIDs:   make(map[uint64]bool),
		neighbors: make(map[uint64]map[uint64]bool),
	}
}

/*
Learn runs the full PC algorithm over the current edge statistics:

 1. Build complete undirected skeleton from all observed edges.
 2. For conditioning set sizes 0, 1, 2, ... up to maxConditionSize:
    - For each adjacent pair (X, Y), test conditional independence
      given every subset of Adj(X)\{Y} of the current size.
    - If X ⊥ Y | S, remove edge and record S as the separation set.
 3. Orient v-structures: for each triple X - Z - Y where X and Y are
    not adjacent, if Z is NOT in SepSet(X, Y), orient as X → Z ← Y.
 4. Apply Meek's three orientation rules to propagate directions without
    creating new v-structures or cycles.

maxConditionSize caps the conditioning set depth. For sparse graphs
(typical in online learning) 2–3 is sufficient. Setting to 0 uses
the default of 3.

alpha is the significance threshold for the G-test. Edges with
p-value below alpha are considered dependent (kept). Typical: 0.05.
*/
func (discovery *Discovery) Learn(maxConditionSize int, alpha float64) {
	if maxConditionSize <= 0 {
		maxConditionSize = 3
	}

	if alpha <= 0 {
		alpha = 0.05
	}

	discovery.graph.mu.RLock()
	edges := make(map[edgeKey]*edge, len(discovery.graph.edges))

	for key, entry := range discovery.graph.edges {
		edges[key] = entry
	}

	discovery.graph.mu.RUnlock()

	discovery.mu.Lock()
	defer discovery.mu.Unlock()

	discovery.buildSkeleton(edges)

	for condSize := 0; condSize <= maxConditionSize; condSize++ {
		discovery.eliminateEdges(edges, condSize, alpha)
	}

	discovery.orientVStructures()
	discovery.applyMeekRules()
}

/*
buildSkeleton initialises the undirected skeleton from all observed
edges with sufficient data (totalCount >= 5).
*/
func (discovery *Discovery) buildSkeleton(edges map[edgeKey]*edge) {
	discovery.skeleton = make(map[edgeKey]bool)
	discovery.nodeIDs = make(map[uint64]bool)
	discovery.neighbors = make(map[uint64]map[uint64]bool)

	for key, entry := range edges {
		if entry.totalCount < 5 {
			continue
		}

		discovery.nodeIDs[key.from] = true
		discovery.nodeIDs[key.to] = true

		discovery.addUndirectedEdge(key.from, key.to)
	}
}

func (discovery *Discovery) addUndirectedEdge(a, b uint64) {
	discovery.skeleton[edgeKey{from: a, to: b}] = true
	discovery.skeleton[edgeKey{from: b, to: a}] = true

	if discovery.neighbors[a] == nil {
		discovery.neighbors[a] = make(map[uint64]bool)
	}

	if discovery.neighbors[b] == nil {
		discovery.neighbors[b] = make(map[uint64]bool)
	}

	discovery.neighbors[a][b] = true
	discovery.neighbors[b][a] = true
}

func (discovery *Discovery) removeUndirectedEdge(a, b uint64) {
	delete(discovery.skeleton, edgeKey{from: a, to: b})
	delete(discovery.skeleton, edgeKey{from: b, to: a})

	if discovery.neighbors[a] != nil {
		delete(discovery.neighbors[a], b)
	}

	if discovery.neighbors[b] != nil {
		delete(discovery.neighbors[b], a)
	}
}

/*
eliminateEdges tests conditional independence for all adjacent pairs
at the given conditioning set size, removing edges where independence
is detected.
*/
func (discovery *Discovery) eliminateEdges(
	edges map[edgeKey]*edge, condSize int, alpha float64,
) {
	type removal struct {
		a, b   uint64
		sepSet []uint64
	}

	var removals []removal

	for nodeX := range discovery.nodeIDs {
		adjX := discovery.adjacentTo(nodeX)

		for _, nodeY := range adjX {
			if !discovery.skeleton[edgeKey{from: nodeX, to: nodeY}] {
				continue
			}

			candidates := discovery.adjacentExcluding(nodeX, nodeY)

			if len(candidates) < condSize {
				continue
			}

			subsets := enumerateSubsets(candidates, condSize)

			for _, subset := range subsets {
				if discovery.conditionallyIndependent(edges, nodeX, nodeY, subset, alpha) {
					removals = append(removals, removal{
						a: nodeX, b: nodeY, sepSet: subset,
					})

					break
				}
			}
		}
	}

	for _, rem := range removals {
		discovery.removeUndirectedEdge(rem.a, rem.b)
		discovery.sepSets[makeUndirectedKey(rem.a, rem.b)] = rem.sepSet
	}
}

/*
conditionallyIndependent performs a G-test (log-likelihood ratio test)
for independence of X and Y conditioned on the set S. Uses the
per-label edge counts as the contingency table.

The G-statistic is 2 * Σ O_ij * ln(O_ij / E_ij), where O are
observed frequencies and E are expected under independence.
Under H0 (independence), G ~ χ²(df).
*/
func (discovery *Discovery) conditionallyIndependent(
	edges map[edgeKey]*edge, x, y uint64, condSet []uint64, alpha float64,
) bool {
	xyEdge := edges[edgeKey{from: x, to: y}]

	if xyEdge == nil || xyEdge.totalCount < 5 {
		return true
	}

	if len(condSet) == 0 {
		return discovery.marginallyIndependent(xyEdge, alpha)
	}

	condLabels := discovery.labelsWhereAllPresent(edges, condSet)

	if len(condLabels) < 2 {
		return false
	}

	var condTotal float64

	for _, label := range condLabels {
		condTotal += xyEdge.labelCounts[label]
	}

	if condTotal < 5 {
		return false
	}

	var gStat float64

	for _, label := range condLabels {
		observed := xyEdge.labelCounts[label]

		if observed <= 0 {
			continue
		}

		expected := condTotal / float64(len(condLabels))

		if expected <= 0 {
			continue
		}

		gStat += observed * math.Log(observed/expected)
	}

	gStat *= 2

	df := float64(len(condLabels) - 1)

	if df <= 0 {
		return false
	}

	pValue := chiSquaredSurvival(gStat, df)

	return pValue >= alpha
}

/*
marginallyIndependent tests whether an edge's label distribution
is uniform (indicating no association). Uses a G-test against the
null hypothesis of equal probability across labels.
*/
func (discovery *Discovery) marginallyIndependent(
	entry *edge, alpha float64,
) bool {
	if len(entry.labelCounts) < 2 {
		return false
	}

	expected := entry.totalCount / float64(len(entry.labelCounts))

	if expected <= 0 {
		return false
	}

	var gStat float64

	for _, count := range entry.labelCounts {
		if count <= 0 {
			continue
		}

		gStat += count * math.Log(count/expected)
	}

	gStat *= 2

	df := float64(len(entry.labelCounts) - 1)
	pValue := chiSquaredSurvival(gStat, df)

	return pValue >= alpha
}

/*
labelsWhereAllPresent returns the set of labels where every node in
the conditioning set has at least one outgoing edge observed.
*/
func (discovery *Discovery) labelsWhereAllPresent(
	edges map[edgeKey]*edge, nodes []uint64,
) []string {
	if len(nodes) == 0 {
		return nil
	}

	labelSets := make([]map[string]bool, len(nodes))

	for idx, nodeID := range nodes {
		labelSets[idx] = make(map[string]bool)

		for key, entry := range edges {
			if key.from != nodeID {
				continue
			}

			for label := range entry.labelCounts {
				labelSets[idx][label] = true
			}
		}
	}

	var result []string

	for label := range labelSets[0] {
		present := true

		for idx := 1; idx < len(labelSets); idx++ {
			if !labelSets[idx][label] {
				present = false
				break
			}
		}

		if present {
			result = append(result, label)
		}
	}

	return result
}

func (discovery *Discovery) adjacentTo(node uint64) []uint64 {
	adj := discovery.neighbors[node]

	if len(adj) == 0 {
		return nil
	}

	out := make([]uint64, 0, len(adj))

	for neighbor := range adj {
		out = append(out, neighbor)
	}

	return out
}

func (discovery *Discovery) adjacentExcluding(node, exclude uint64) []uint64 {
	adj := discovery.neighbors[node]

	if len(adj) == 0 {
		return nil
	}

	out := make([]uint64, 0, len(adj)-1)

	for neighbor := range adj {
		if neighbor != exclude {
			out = append(out, neighbor)
		}
	}

	return out
}

/*
orientVStructures detects v-structures (colliders): for each triple
X - Z - Y where X and Y are NOT adjacent, if Z is not in the
separation set of (X, Y), orient as X → Z ← Y.
*/
func (discovery *Discovery) orientVStructures() {
	discovery.oriented = make(map[edgeKey]Direction)

	for key := range discovery.skeleton {
		discovery.oriented[key] = Undirected
	}

	for nodeZ := range discovery.nodeIDs {
		adjZ := discovery.adjacentTo(nodeZ)

		for idxA := 0; idxA < len(adjZ); idxA++ {
			for idxB := idxA + 1; idxB < len(adjZ); idxB++ {
				nodeX := adjZ[idxA]
				nodeY := adjZ[idxB]

				if discovery.skeleton[edgeKey{from: nodeX, to: nodeY}] {
					continue
				}

				sepSet := discovery.sepSets[makeUndirectedKey(nodeX, nodeY)]

				if containsID(sepSet, nodeZ) {
					continue
				}

				discovery.oriented[edgeKey{from: nodeX, to: nodeZ}] = Forward
				discovery.oriented[edgeKey{from: nodeZ, to: nodeX}] = Backward
				discovery.oriented[edgeKey{from: nodeY, to: nodeZ}] = Forward
				discovery.oriented[edgeKey{from: nodeZ, to: nodeY}] = Backward
			}
		}
	}
}

/*
applyMeekRules propagates edge orientations using three rules that
avoid creating new v-structures or directed cycles. Iterates until
no new orientations are produced.

Rule 1: If X → Z - Y and X ⊥ Y, orient Z → Y.
Rule 2: If X → Z → Y and X - Y, orient X → Y.
Rule 3: If X - Z → Y and X - W → Y and Z ≠ W, orient X → Y.
*/
func (discovery *Discovery) applyMeekRules() {
	changed := true

	for changed {
		changed = false

		for nodeZ := range discovery.nodeIDs {
			adjZ := discovery.adjacentTo(nodeZ)

			for _, nodeY := range adjZ {
				if discovery.oriented[edgeKey{from: nodeZ, to: nodeY}] != Undirected {
					continue
				}

				if discovery.meekRule1(nodeZ, nodeY) {
					changed = true
					continue
				}

				if discovery.meekRule2(nodeZ, nodeY) {
					changed = true
					continue
				}

				if discovery.meekRule3(nodeZ, nodeY) {
					changed = true
				}
			}
		}
	}
}

func (discovery *Discovery) meekRule1(z, y uint64) bool {
	adjZ := discovery.adjacentTo(z)

	for _, nodeX := range adjZ {
		if nodeX == y {
			continue
		}

		if discovery.oriented[edgeKey{from: nodeX, to: z}] != Forward {
			continue
		}

		if discovery.skeleton[edgeKey{from: nodeX, to: y}] {
			continue
		}

		discovery.orient(z, y)

		return true
	}

	return false
}

func (discovery *Discovery) meekRule2(z, y uint64) bool {
	if discovery.oriented[edgeKey{from: z, to: y}] != Undirected {
		return false
	}

	adjZ := discovery.adjacentTo(z)

	for _, nodeX := range adjZ {
		if nodeX == y {
			continue
		}

		if discovery.oriented[edgeKey{from: nodeX, to: z}] != Forward {
			continue
		}

		if discovery.oriented[edgeKey{from: z, to: y}] == Forward {
			if discovery.skeleton[edgeKey{from: nodeX, to: y}] &&
				discovery.oriented[edgeKey{from: nodeX, to: y}] == Undirected {
				discovery.orient(nodeX, y)

				return true
			}
		}
	}

	return false
}

func (discovery *Discovery) meekRule3(z, y uint64) bool {
	if discovery.oriented[edgeKey{from: z, to: y}] != Undirected {
		return false
	}

	adjY := discovery.adjacentTo(y)
	intoY := make([]uint64, 0)

	for _, nodeW := range adjY {
		if nodeW == z && discovery.oriented[edgeKey{from: nodeW, to: y}] == Forward {
			continue
		}

		if discovery.oriented[edgeKey{from: nodeW, to: y}] == Forward {
			intoY = append(intoY, nodeW)
		}
	}

	for idxA := 0; idxA < len(intoY); idxA++ {
		for idxB := idxA + 1; idxB < len(intoY); idxB++ {
			wA := intoY[idxA]
			wB := intoY[idxB]

			zAdjA := discovery.skeleton[edgeKey{from: z, to: wA}] &&
				discovery.oriented[edgeKey{from: z, to: wA}] == Undirected
			zAdjB := discovery.skeleton[edgeKey{from: z, to: wB}] &&
				discovery.oriented[edgeKey{from: z, to: wB}] == Undirected

			if zAdjA && zAdjB && !discovery.skeleton[edgeKey{from: wA, to: wB}] {
				discovery.orient(z, y)

				return true
			}
		}
	}

	return false
}

func (discovery *Discovery) orient(from, to uint64) {
	discovery.oriented[edgeKey{from: from, to: to}] = Forward
	discovery.oriented[edgeKey{from: to, to: from}] = Backward
}

/*
IsDirectCause returns true if the PC algorithm has oriented from → to
as a direct causal edge.
*/
func (discovery *Discovery) IsDirectCause(from, to uint64) bool {
	discovery.mu.RLock()
	defer discovery.mu.RUnlock()

	return discovery.oriented[edgeKey{from: from, to: to}] == Forward
}

/*
CausalChildren returns all nodes that the PC algorithm has identified
as direct effects of the given node.
*/
func (discovery *Discovery) CausalChildren(node uint64) []uint64 {
	discovery.mu.RLock()
	defer discovery.mu.RUnlock()

	var children []uint64

	for _, neighbor := range discovery.adjacentTo(node) {
		if discovery.oriented[edgeKey{from: node, to: neighbor}] == Forward {
			children = append(children, neighbor)
		}
	}

	return children
}

/*
SeparationSet returns the conditioning set that rendered X and Y
independent, or nil if they were never separated.
*/
func (discovery *Discovery) SeparationSet(x, y uint64) []uint64 {
	discovery.mu.RLock()
	defer discovery.mu.RUnlock()

	return discovery.sepSets[makeUndirectedKey(x, y)]
}

func containsID(slice []uint64, id uint64) bool {
	for _, item := range slice {
		if item == id {
			return true
		}
	}

	return false
}

/*
enumerateSubsets returns all subsets of the given slice with exactly
size elements. Uses iterative bit masking for sizes up to 20.
*/
func enumerateSubsets(items []uint64, size int) [][]uint64 {
	if size == 0 {
		return [][]uint64{{}}
	}

	if size > len(items) {
		return nil
	}

	if size > 20 {
		size = 20
	}

	total := uint64(1) << uint(len(items))
	var result [][]uint64

	for mask := uint64(0); mask < total; mask++ {
		if popcount64(mask) != size {
			continue
		}

		subset := make([]uint64, 0, size)

		for bit := range len(items) {
			if mask&(1<<uint(bit)) != 0 {
				subset = append(subset, items[bit])
			}
		}

		result = append(result, subset)
	}

	return result
}

func popcount64(x uint64) int {
	x = x - ((x >> 1) & 0x5555555555555555)
	x = (x & 0x3333333333333333) + ((x >> 2) & 0x3333333333333333)
	x = (x + (x >> 4)) & 0x0f0f0f0f0f0f0f0f

	return int((x * 0x0101010101010101) >> 56)
}

/*
chiSquaredSurvival computes the upper-tail probability P(X > x) for
a chi-squared distribution with df degrees of freedom using the
regularized incomplete gamma function. This is the p-value for the
G-test: large p-values indicate independence.
*/
func chiSquaredSurvival(x, df float64) float64 {
	if x <= 0 || df <= 0 {
		return 1.0
	}

	return 1.0 - regularizedGammaP(df/2, x/2)
}

/*
regularizedGammaP computes the regularized lower incomplete gamma
function P(a, x) = γ(a, x) / Γ(a) using the series expansion.
Convergence is rapid for x < a + 1.
*/
func regularizedGammaP(a, x float64) float64 {
	if x <= 0 {
		return 0
	}

	if x > a+1 {
		return 1.0 - regularizedGammaQ(a, x)
	}

	lgamma, _ := math.Lgamma(a)
	term := 1.0 / a
	sum := term

	for n := 1; n < 200; n++ {
		term *= x / (a + float64(n))
		sum += term

		if math.Abs(term) < 1e-14*math.Abs(sum) {
			break
		}
	}

	return sum * math.Exp(-x+a*math.Log(x)-lgamma)
}

/*
regularizedGammaQ computes the upper regularized incomplete gamma
function Q(a, x) = 1 - P(a, x) using Legendre's continued fraction.
Convergence is rapid for x >= a + 1.
*/
func regularizedGammaQ(a, x float64) float64 {
	lgamma, _ := math.Lgamma(a)

	f := 1e-30
	c := 1e-30
	d := 1.0 / (x + 1 - a)
	f = d

	for n := 1; n < 200; n++ {
		an := float64(n) * (a - float64(n))
		bn := x + float64(2*n+1) - a
		d = bn + an*d

		if math.Abs(d) < 1e-30 {
			d = 1e-30
		}

		c = bn + an/c

		if math.Abs(c) < 1e-30 {
			c = 1e-30
		}

		d = 1.0 / d
		delta := d * c
		f *= delta

		if math.Abs(delta-1) < 1e-14 {
			break
		}
	}

	return f * math.Exp(-x+a*math.Log(x)-lgamma)
}
