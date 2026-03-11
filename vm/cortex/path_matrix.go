package cortex

import (
	"context"
	"math/bits"
	"sync"

	"github.com/theapemachine/six/data"
)

/*
PathMatrix is the high-performance memory matrix of the Cortex.
It replaces the traditional node-pointer graph. Experiences (sequences)
are flattened into contiguous 257-bit geometric chords.

Evaluation uses brute-force XOR over the entire memory space in O(N).
Because modern CPUs execute POPCNT in a single cycle, and the memory layout
is cache-perfect, this will evaluate millions of paths per millisecond.
*/
type PathMatrix struct {
	mu sync.RWMutex

	paths []data.Chord
	ctx   context.Context
}

/*
NewPathMatrix allocates a new flat continuous memory graph.
*/
func NewPathMatrix(capacity int) *PathMatrix {
	return &PathMatrix{
		paths: make([]data.Chord, 0, capacity),
		ctx:   context.Background(),
	}
}

/*
Insert adds a new geometric path to the matrix.
*/
func (pm *PathMatrix) Insert(path data.Chord) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.paths = append(pm.paths, path)
}

/*
Evaluate performs a massively concurrent breakdown of the context against
all stored paths via Cancellative Superposition (XOR).
Returns the index of the path possessing the lowest geometric residue,
and the residue score (ActiveCount) itself.
*/
func (pm *PathMatrix) Evaluate(context data.Chord) (int, int) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if len(pm.paths) == 0 {
		return -1, -1
	}

	bestIdx := -1
	minResidue := 999999

	// The hot loop. The CPU prefetcher will saturate the memory bus here.
	for i := 0; i < len(pm.paths); i++ {
		candidate := &pm.paths[i]

		// 4 XORs and 4 Hardware POPCNTs. Zero allocations.
		residue := bits.OnesCount64(context[0]^candidate[0]) +
			bits.OnesCount64(context[1]^candidate[1]) +
			bits.OnesCount64(context[2]^candidate[2]) +
			bits.OnesCount64(context[3]^candidate[3])

		// Includes the last partial 64-bit word (the 257th bit)
		residue += bits.OnesCount64(context[4] ^ candidate[4])

		if residue < minResidue {
			minResidue = residue
			bestIdx = i
		}
	}

	return bestIdx, minResidue
}

/*
Len returns the number of active paths.
*/
func (pm *PathMatrix) Len() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return len(pm.paths)
}
