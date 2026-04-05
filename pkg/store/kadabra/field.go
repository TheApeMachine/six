package kadabra

import (
	"math"
	"math/bits"
	"sync"
)

/*
FieldDigest is a compact summary of a trie node's current adaptive state.
It is the unit of gossip: nodes exchange digests so the field can compute
its effect on each node.
*/
type FieldDigest struct {
	Origin        NodeID
	Affinity      [AffinityWords]uint64
	SurprisalMean float64
	SurprisalVar  float64
	SurprisalPrev float64 // previous epoch's mean, for phase velocity
	ClassEntropy  float64
	GrowthRate    float64
	Depth         float64
	Epoch         uint64
}

/*
eigenmode is an emergent cluster of structurally aligned tries. The field
identifies modes by greedy affinity clustering, then selects the dominant
mode — the one with the most collective energy. This is attention: the
system "attends to" the dominant mode by amplifying its constituents and
suppressing everything else.
*/
type eigenmode struct {
	members []NodeID
	energy  float64 // total surprisal activity across members
	center  [AffinityWords]uint64
}

/*
affineRotations are the exponents k for the affine permutation used to
evaluate multi-scale structural alignment between affinity vectors.
*/
var affineRotations = [4]uint64{1, 4, 16, 64}

/*
affineAffinityBitRotationMul scales each affine exponent k (from affineRotations)
before reducing mod 64 for the per-lane bit rotation in affineCoupling.

Using 3 keeps gcd(3, 64) = 1 so the composed shift (k * affineAffinityBitRotationMul) % 64
visits many distinct bit phases as k sweeps the exponentially spaced scales,
spreading overlap mass across the AffinityWords × 64-bit hypercube instead of
reusing a small set of aligned rotations only.
*/
const affineAffinityBitRotationMul = 3

const (
	// modeCouplingThreshold: minimum affine coupling to consider two
	// digests part of the same eigenmode.
	modeCouplingThreshold = 0.15

	// alignedDecayFactor: nodes in the dominant mode decay slowly (retained).
	// misalignedDecayFactor: nodes outside the dominant mode decay fast (pruned).
	// The asymmetry is the selection pressure — this IS the attention.
	alignedDecayMultiplier    = 0.5 // halves decay pressure (retain)
	misalignedDecayMultiplier = 2.0 // doubles decay pressure (forget)
	alignedLearnMultiplier    = 1.5 // amplify learning
	misalignedLearnMultiplier = 0.3 // suppress learning
)

/*
affineCoupling computes multi-scale structural alignment between two
affinity vectors. For each affine rotation k, it permutes bit positions
of one vector and measures overlap with the other.

Returns a coupling strength in [0, 1] where 1 = perfect alignment at
all scales.
*/
func affineCoupling(a, b [AffinityWords]uint64) float64 {
	maxBits := float64(AffinityWords * 64 * len(affineRotations))
	if maxBits == 0 {
		return 0
	}

	var totalOverlap int

	for _, k := range affineRotations {
		var permuted [AffinityWords]uint64
		for w := range AffinityWords {
			srcWord := int((uint64(w) + k) % uint64(AffinityWords))
			shift := (k * affineAffinityBitRotationMul) % 64
			permuted[w] = (a[srcWord] << shift) | (a[srcWord] >> (64 - shift))
		}

		for w := range AffinityWords {
			totalOverlap += bits.OnesCount64(permuted[w] & b[w])
		}
	}

	return float64(totalOverlap) / maxBits
}

func phaseVelocity(d FieldDigest) float64 {
	return d.SurprisalMean - d.SurprisalPrev
}

/*
phaseCoupling scores how strongly two digests' surprisal velocities line up.
The product va*vb is divided by the square of the geometric mean of the
two magnitudes (mg := sqrt(magA*magB)), equivalent to (va*vb)/(magA*magB),
instead of normalizing by max(magA,magB)^2. That avoids over-penalizing
pairs where one velocity is much weaker than the other.
*/
func phaseCoupling(a, b FieldDigest) float64 {
	const magEps = 0.01

	va := phaseVelocity(a)
	vb := phaseVelocity(b)

	magA := math.Abs(va)
	magB := math.Abs(vb)
	mg := math.Sqrt(magA * magB)

	if mg < magEps {
		return 0
	}

	return (va * vb) / (mg * mg)
}

/*
FieldView accumulates gossipped digests and projects field pressure onto
the owning node's trie. The field identifies emergent eigenmodes — clusters
of phase-coherent, structurally aligned tries — then selects a dominant
mode and projects asymmetric pressure: amplify the attended mode, suppress
the rest.
*/
type FieldView struct {
	mu      sync.RWMutex
	digests map[NodeID]FieldDigest
	owner   *KadabraNode

	// Eigenmode state, recomputed on each projection.
	modes        []eigenmode
	dominantMode int // index into modes, or -1
}

func newFieldView(owner *KadabraNode) *FieldView {
	return &FieldView{
		digests:      make(map[NodeID]FieldDigest),
		owner:        owner,
		dominantMode: -1,
	}
}

/*
ModeCount returns how many eigenmodes were detected in the last projection.
*/
func (fv *FieldView) ModeCount() int {
	if fv == nil {
		return 0
	}

	fv.mu.RLock()
	defer fv.mu.RUnlock()

	return len(fv.modes)
}

/*
ModeMembers returns a copy of member node IDs for the mode at modeIdx, or nil if out of range.
*/
func (fv *FieldView) ModeMembers(modeIdx int) []NodeID {
	if fv == nil || modeIdx < 0 {
		return nil
	}

	fv.mu.RLock()
	defer fv.mu.RUnlock()

	if modeIdx >= len(fv.modes) {
		return nil
	}

	out := make([]NodeID, len(fv.modes[modeIdx].members))
	copy(out, fv.modes[modeIdx].members)

	return out
}

/*
ModeEnergy returns the aggregate energy score for modeIdx, or 0 if out of range.
*/
func (fv *FieldView) ModeEnergy(modeIdx int) float64 {
	if fv == nil || modeIdx < 0 {
		return 0
	}

	fv.mu.RLock()
	defer fv.mu.RUnlock()

	if modeIdx >= len(fv.modes) {
		return 0
	}

	return fv.modes[modeIdx].energy
}

/*
DominantModeIndex returns the index of the highest-energy mode, or -1 if none.
*/
func (fv *FieldView) DominantModeIndex() int {
	if fv == nil {
		return -1
	}

	fv.mu.RLock()
	defer fv.mu.RUnlock()

	return fv.dominantMode
}

/*
DominantModeEnergy returns the energy of the dominant mode, or 0 if there is no dominant mode.
*/
func (fv *FieldView) DominantModeEnergy() float64 {
	if fv == nil {
		return 0
	}

	fv.mu.RLock()
	defer fv.mu.RUnlock()

	if fv.dominantMode < 0 || fv.dominantMode >= len(fv.modes) {
		return 0
	}

	return fv.modes[fv.dominantMode].energy
}

/*
Absorb integrates a received digest, recomputes eigenmodes, and projects
field pressure onto the owning node's trie.
*/
func (fv *FieldView) Absorb(digest FieldDigest) {
	fv.mu.Lock()

	if existing, ok := fv.digests[digest.Origin]; ok {
		if digest.Epoch <= existing.Epoch {
			fv.mu.Unlock()
			return
		}
	}

	fv.digests[digest.Origin] = digest
	fv.mu.Unlock()

	fv.project()
}

/*
detectModes performs greedy eigenmode detection: iterate digests, assign
each to the first mode it couples with above threshold, or start a new
mode. Then identify the dominant mode by total energy.

This is O(n*m) where n=digests, m=modes. For DHT-scale networks (hundreds
to low thousands of nodes) this is fine. The clustering doesn't need to be
perfect — it needs to identify the dominant "thought" so the field can
select for it.
*/
func (fv *FieldView) detectModes() {
	fv.modes = fv.modes[:0]
	fv.dominantMode = -1

	digests := make([]FieldDigest, 0, len(fv.digests))
	for _, d := range fv.digests {
		digests = append(digests, d)
	}

	if len(digests) == 0 {
		return
	}

	// Assign each digest to the best-coupling existing mode, or start a new one.
	for _, d := range digests {
		bestMode := -1
		bestCoupling := 0.0

		for mi := range fv.modes {
			coupling := affineCoupling(fv.modes[mi].center, d.Affinity)
			if coupling >= modeCouplingThreshold && coupling > bestCoupling {
				// Phase must also be compatible.
				seed := fv.digests[fv.modes[mi].members[0]]
				phase := phaseCoupling(seed, d)
				if phase >= -0.3 {
					bestMode = mi
					bestCoupling = coupling
				}
			}
		}

		if bestMode >= 0 {
			m := &fv.modes[bestMode]
			m.members = append(m.members, d.Origin)
			m.energy += d.SurprisalMean + math.Abs(phaseVelocity(d))
			for w := range AffinityWords {
				m.center[w] |= d.Affinity[w]
			}
		} else {
			fv.modes = append(fv.modes, eigenmode{
				members: []NodeID{d.Origin},
				energy:  d.SurprisalMean + math.Abs(phaseVelocity(d)),
				center:  d.Affinity,
			})
		}
	}

	// Dominant mode: highest total energy.
	if len(fv.modes) > 0 {
		best := 0
		for i := 1; i < len(fv.modes); i++ {
			if fv.modes[i].energy > fv.modes[best].energy {
				best = i
			}
		}
		fv.dominantMode = best
	}
}

/*
project recomputes eigenmodes and applies asymmetric pressure to the
owning node's trie based on whether it belongs to the dominant mode.

Aligned nodes (in the dominant eigenmode, in-phase):
  - Decay pressure suppressed → knowledge retained longer
  - Learning pressure amplified → absorbs related input faster

Misaligned nodes (outside the dominant mode, or anti-phase):
  - Decay pressure amplified → stale knowledge pruned faster
  - Learning pressure suppressed → doesn't waste effort on noise

This asymmetric fork IS the attention mechanism. No matrices, no softmax.
The field selects by differential survival.
*/
func (fv *FieldView) project() {
	if fv.owner == nil || fv.owner.Store == nil {
		return
	}

	fv.mu.Lock()
	defer fv.mu.Unlock()

	if len(fv.digests) < 2 {
		return
	}

	// Detect emergent eigenmodes from the current digest population.
	fv.detectModes()

	position := fv.owner.Affinity
	local, hasLocal := fv.digests[fv.owner.ID]
	if !hasLocal {
		return
	}

	// Determine alignment: is this node in the dominant eigenmode?
	aligned := false
	if fv.dominantMode >= 0 && fv.dominantMode < len(fv.modes) {
		dominant := fv.modes[fv.dominantMode]
		for _, id := range dominant.members {
			if id == fv.owner.ID {
				aligned = true
				break
			}
		}
	}

	// Compute base field pressure from coupled neighbors.
	var (
		weightedSurprisal float64
		weightedGrowth    float64
		totalWeight       float64
	)

	for _, d := range fv.digests {
		if d.Origin == fv.owner.ID {
			continue
		}

		coupling := affineCoupling(position, d.Affinity)
		if coupling < 0.01 {
			continue
		}

		phase := phaseCoupling(local, d)
		w := coupling * (1.0 + phase)

		if w <= 0 {
			continue
		}

		totalWeight += w
		weightedSurprisal += w * d.SurprisalMean
		weightedGrowth += w * d.GrowthRate
	}

	if totalWeight == 0 {
		return
	}

	fieldSurprisal := weightedSurprisal / totalWeight
	fieldGrowth := weightedGrowth / totalWeight

	// Raw pressure differentials.
	rawDecay := fieldSurprisal - local.SurprisalMean
	rawLearn := fieldSurprisal - local.SurprisalMean
	rawPrune := fieldGrowth - local.GrowthRate

	// Apply eigenmode selection: the asymmetric fork.
	var decayMul, learnMul float64
	if aligned {
		decayMul = alignedDecayMultiplier
		learnMul = alignedLearnMultiplier
	} else {
		decayMul = misalignedDecayMultiplier
		learnMul = misalignedLearnMultiplier
	}

	clamp := func(v, limit float64) float64 {
		return math.Max(-limit, math.Min(limit, v))
	}

	fv.owner.Store.ApplyFieldPressure(
		clamp(rawDecay*decayMul, 3.0),
		clamp(rawLearn*learnMul, 3.0),
		clamp(rawPrune, 5.0),
	)
}
