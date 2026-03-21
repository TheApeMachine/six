package topology

import (
	"math"
	"math/bits"

	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	config "github.com/theapemachine/six/pkg/system/core"
)

/*
BirthDeath records the birth and death filtration thresholds for one
topological feature. Persistence = Death − Birth. Features that never
die have Death = −1 (essential features that survive the entire
filtration).
*/
type BirthDeath struct {
	Birth      float64
	Death      float64
	Dimension  int
	ComponentA int32
	ComponentB int32
}

/*
Persistence returns the lifespan of this feature. Essential features
(Death < 0) return +Inf because they persist across the entire
filtration range.
*/
func (bd BirthDeath) Persistence() float64 {
	if bd.Death < 0 {
		return math.Inf(1)
	}

	return bd.Death - bd.Birth
}

/*
Barcode tracks the persistence diagram for a stream of Values.
H_0 (connected components) is tracked in real-time via UnionFind.
H_1 (loops) is detected via incremental cycle counting during sweeps.
H_2 (voids) is reserved for offline computation.
*/
type Barcode struct {
	unionFind *UnionFind
	features  []BirthDeath
	valueIDs  map[int32]int
	threshold float64
	loops     int
}

type barcodeOpts func(*Barcode)

/*
NewBarcode allocates a Barcode with an initial UnionFind capacity and
empty feature list. Optional functional options configure thresholds.
*/
func NewBarcode(opts ...barcodeOpts) *Barcode {
	barcode := &Barcode{
		unionFind: NewUnionFind(256),
		features:  make([]BirthDeath, 0, 64),
		valueIDs:  make(map[int32]int, 256),
	}

	for _, opt := range opts {
		opt(barcode)
	}

	return barcode
}

/*
AddValue introduces a new Value to the filtration at the current
threshold. Creates a birth event for H_0 (a new connected component
is born).
*/
func (barcode *Barcode) AddValue(index int) int32 {
	id := barcode.unionFind.MakeSet()

	barcode.valueIDs[id] = index
	barcode.features = append(barcode.features, BirthDeath{
		Birth:      barcode.threshold,
		Death:      -1,
		Dimension:  0,
		ComponentA: id,
		ComponentB: -1,
	})

	return id
}

/*
Connect merges two components when their structural similarity exceeds
the current filtration threshold. If they were in different components,
records a death event for H_0: the younger component is absorbed by
the elder (the one born earlier survives, per the elder rule).
Returns true if a merge happened.
*/
func (barcode *Barcode) Connect(idA, idB int32) bool {
	if !barcode.unionFind.Union(idA, idB) {
		return false
	}

	barcode.loops++

	var dying int32

	birthA := barcode.birthOf(idA)
	birthB := barcode.birthOf(idB)

	if birthA <= birthB {
		dying = idB
	} else {
		dying = idA
	}

	for idx := range barcode.features {
		feature := &barcode.features[idx]

		if feature.Dimension == 0 && feature.Death < 0 && feature.ComponentA == dying {
			feature.Death = barcode.threshold
			feature.ComponentB = idA + idB - dying
			break
		}
	}

	return true
}

/*
SweepPair evaluates two Values and connects them if their Jaccard
similarity (shared core bits / union core bits) exceeds the current
filtration threshold.
*/
func (barcode *Barcode) SweepPair(valA, valB primitive.Value, idA, idB int32) bool {
	similarity := JaccardSimilarity(valA, valB)

	if similarity < barcode.threshold {
		return false
	}

	return barcode.Connect(idA, idB)
}

/*
AdvanceThreshold steps the filtration parameter. Values connected
below this threshold stay connected; new connections form as the
threshold drops (more lenient similarity requirements).
*/
func (barcode *Barcode) AdvanceThreshold(threshold float64) {
	barcode.threshold = threshold
}

/*
Sweep runs a full filtration over a set of Values, sweeping the Jaccard
similarity threshold from 1.0 down to 0.0 in 1/CoreBits resolution steps
(8191 steps). Records all birth-death events. Early-terminates when a
single connected component remains.
*/
func (barcode *Barcode) Sweep(values []primitive.Value) []BirthDeath {
	barcode.unionFind.Reset()
	barcode.features = barcode.features[:0]
	barcode.loops = 0

	for k := range barcode.valueIDs {
		delete(barcode.valueIDs, k)
	}

	ids := make([]int32, len(values))
	barcode.threshold = 1.0

	for idx := range values {
		ids[idx] = barcode.AddValue(idx)
	}

	steps := config.CoreBits
	valueCount := len(values)

	for step := steps; step >= 0; step-- {
		threshold := float64(step) / float64(steps)
		barcode.AdvanceThreshold(threshold)

		for outer := 0; outer < valueCount; outer++ {
			for inner := outer + 1; inner < valueCount; inner++ {
				barcode.SweepPair(
					values[outer], values[inner],
					ids[outer], ids[inner],
				)
			}
		}

		if barcode.unionFind.Components() <= 1 {
			break
		}
	}

	return barcode.features
}

/*
Features returns the current persistence diagram as a slice of
birth-death pairs.
*/
func (barcode *Barcode) Features() []BirthDeath {
	return barcode.features
}

/*
BettiNumbers returns [H_0, H_1, H_2] at the current filtration level.
H_0 = number of connected components (from UnionFind).
H_1 = number of independent loops detected during sweeps.
H_2 = reserved for offline void computation (always 0 currently).
*/
func (barcode *Barcode) BettiNumbers() [3]int {
	return [3]int{barcode.unionFind.Components(), barcode.loops, 0}
}

/*
StableFeatures returns features with persistence above the given minimum,
filtering out topological noise (short-lived features that are likely
artifacts of sampling granularity rather than genuine structure).
*/
func (barcode *Barcode) StableFeatures(minPersistence float64) []BirthDeath {
	stable := make([]BirthDeath, 0, len(barcode.features))

	for _, feature := range barcode.features {
		if feature.Persistence() >= minPersistence {
			stable = append(stable, feature)
		}
	}

	return stable
}

/*
birthOf scans features for the birth threshold of the component that
contains the given id. Returns +Inf if not found (should never happen
in well-formed usage).
*/
func (barcode *Barcode) birthOf(id int32) float64 {
	root := barcode.unionFind.Find(id)

	for _, feature := range barcode.features {
		if feature.Dimension == 0 && barcode.unionFind.Find(feature.ComponentA) == root {
			return feature.Birth
		}
	}

	return math.Inf(1)
}

/*
JaccardSimilarity computes the Jaccard index between two Values using
core blocks only: |A ∩ B| / |A ∪ B|. Returns 0 when both values are
empty (no bits set) to avoid division by zero.
*/
func JaccardSimilarity(valA, valB primitive.Value) float64 {
	intersection := 0
	union := 0
	lastCore := config.CoreBlocks - 1

	for idx := range lastCore {
		blockA := valA.Block(idx)
		blockB := valB.Block(idx)

		intersection += bits.OnesCount64(blockA & blockB)
		union += bits.OnesCount64(blockA | blockB)
	}

	maskLast := uint64((1 << (config.CoreBits % 64)) - 1)
	blockA := valA.Block(lastCore) & maskLast
	blockB := valB.Block(lastCore) & maskLast

	intersection += bits.OnesCount64(blockA & blockB)
	union += bits.OnesCount64(blockA | blockB)

	if union == 0 {
		return 0
	}

	return float64(intersection) / float64(union)
}
