package geometry

import "strconv"

// UnifiedGeodesicMatrix is the 60x60 lookup table storing the shortest-path
// geodesic distances between the 60 discrete chiral states of the Icosahedral Manifold ($A_5$).
// It universally houses both the 24-state $O$ metrics and 60-state $A_5$ metrics.
// The distances are precalculated to allow $O(1)$ ambiguity resolution natively on the GPU
// without runtime floating-point \arccos execution.
var UnifiedGeodesicMatrix [3600]byte

// StateTransitionMatrix is a 60x4 lookup table containing the
// Cayley table (A_5 State Machine) for the 4 permitted geometric topologies.
// [currentState][topologicalEvent] = nextState
var StateTransitionMatrix [60][4]uint8

// Topological Events mapping to A_5 Generators
const (
	EventLowVarianceFlux = 0 // 5-Cycle
	EventDensitySpike    = 1 // 3-Cycle
	EventDensityTrough   = 2 // Inverse 3-Cycle
	EventPhaseInversion  = 3 // Double Transposition
)

type a5State [5]byte

func (s a5State) apply(p a5State) a5State {
	var n a5State
	for i := range 5 {
		n[i] = s[p[i]]
	}
	return n
}

func init() {
	// Generators corresponding to the 4 permitted topological triggers in PrimeField
	gen5 := a5State{4, 0, 1, 2, 3}    // 5-Cycle
	gen3 := a5State{2, 0, 1, 3, 4}    // 3-Cycle
	gen3inv := a5State{1, 2, 0, 3, 4} // Inverse 3-Cycle
	genD := a5State{3, 4, 2, 0, 1}    // Double Transposition

	generators := []a5State{gen5, gen3, gen3inv, genD}

	// 1. Discover all 60 states via BFS
	identity := a5State{0, 1, 2, 3, 4}
	states := []a5State{identity}
	stateMap := map[a5State]int{identity: 0}

	head := 0
	for head < len(states) {
		curr := states[head]
		head++
		for _, g := range generators {
			next := curr.apply(g)
			if _, ok := stateMap[next]; !ok {
				stateMap[next] = len(states)
				states = append(states, next)
			}
		}
	}

	if len(states) != 60 {
		panic("expected 60 states, found " + strconv.Itoa(len(states)))
	}

	// 1.5 Compute Cayley Table for O(1) State Transitions
	for i, start := range states {
		for eventIdx, g := range generators {
			next := start.apply(g)
			StateTransitionMatrix[i][eventIdx] = uint8(stateMap[next])
		}
	}

	// 2. Compute all-pairs shortest path matrix
	for i, start := range states {
		dist := make(map[a5State]byte)
		dist[start] = 0
		q := []a5State{start}
		qh := 0

		for qh < len(q) {
			curr := q[qh]
			qh++
			d := dist[curr]

			for _, g := range generators {
				next := curr.apply(g)
				if _, ok := dist[next]; !ok {
					dist[next] = d + 1
					q = append(q, next)
				}
			}
		}

		for j, end := range states {
			if value, ok := dist[end]; ok {
				UnifiedGeodesicMatrix[i*60+j] = value
			} else {
				UnifiedGeodesicMatrix[i*60+j] = 255 // Sentinel for unreachable
			}
		}
	}
}
