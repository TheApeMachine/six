package geometry

import "strconv"

/*
UnifiedGeodesicMatrix is the 60×60 lookup table of shortest-path geodesic distances
between the 60 discrete chiral states of the Icosahedral Manifold (A₅).
Precomputed for O(1) ambiguity resolution on the GPU without runtime arccos.
Populated at init from BFS over the A₅ generator group.
*/
var UnifiedGeodesicMatrix [3600]byte

/*
StateTransitionMatrix is the 60×4 Cayley table for the A₅ state machine.
StateTransitionMatrix[currentState][topologicalEvent] = nextState.
The four events (EventLowVarianceFlux, EventDensitySpike, EventDensityTrough,
EventPhaseInversion) correspond to the four A₅ generators.
*/
var StateTransitionMatrix [60][4]uint8

/*
Topological Events map to A₅ generators: 5-cycle, 3-cycle, inverse 3-cycle, double transposition.
Used to drive state transitions and geodesic distance computation.
*/
const (
	EventLowVarianceFlux = 0 // 5-Cycle
	EventDensitySpike    = 1 // 3-Cycle
	EventDensityTrough   = 2 // Inverse 3-Cycle
	EventPhaseInversion  = 3 // Double Transposition
)

/*
a5State is a permutation of {0,1,2,3,4} representing one of 60 A₅ group elements.
Used internally to enumerate states and compute the Cayley table.
*/
type a5State [5]byte

/*
apply composes permutation p with s: result[i] = s[p[i]].
Corresponds to group multiplication in A₅.
*/
func (s a5State) apply(p a5State) a5State {
	var n a5State
	for i := range 5 {
		n[i] = s[p[i]]
	}
	return n
}

/*
init enumerates all 60 A₅ states via BFS, builds the Cayley table for O(1) transitions,
and computes all-pairs shortest-path distances for the geodesic matrix.
Panics if state count is not 60 (sanity check on generator correctness).
*/
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
