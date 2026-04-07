package kadabra

import (
	"maps"
	"math"
	"slices"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/numeric"
	"github.com/theapemachine/six/pkg/core/numeric/adaptive"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/viz"
)

/*
minAlignmentRatio floors alignmentRatio before reciprocals so misaligned
learn/decay multipliers cannot diverge when dominant energy is tiny.
*/
const minAlignmentRatio = 1e-3

/*
digestMap is an immutable snapshot of remote trie digests for eigenmode work.
*/
type digestMap struct {
	m map[uint64]Digest
}

/*
modesProjection stores the latest DetectModes result without a mutex so
readers can query mode membership lock-free.
*/
type modesProjection struct {
	modes    []geometry.Eigenmode
	dominant int
}

/*
Field lives in the context of a Kadabra Node, and forms a mesh
across all the Markov Tries that the node acts as a routing hub for.

All thresholds and multipliers are Derived chains — no magic numbers.
The coupling threshold tracks the mean observed coupling and gates
above it. Decay and learn pressure clamps each track their own spread.
Alignment multipliers emerge from the ratio of dominant mode energy
to total energy.
*/
type Field struct {
	digests    atomic.Pointer[digestMap]
	projection atomic.Pointer[modesProjection]
	owner      *Node
	phase      *geometry.Phase

	couplingThreshold  *numeric.Derived
	pressureDecayClamp *numeric.Derived
	pressureLearnClamp *numeric.Derived
	alignmentRatio     *numeric.Derived
}

/*
NewField constructs a new Field for the given node.
*/
func NewField(owner *Node) *Field {
	return &Field{
		owner: owner,
		phase: geometry.NewPhase(),

		couplingThreshold: numeric.NewDerived(
			numeric.WithDynamics(adaptive.NewEMA()),
		),

		pressureDecayClamp: numeric.NewDerived(
			numeric.WithDynamics(adaptive.NewSpread()),
		),

		pressureLearnClamp: numeric.NewDerived(
			numeric.WithDynamics(adaptive.NewSpread()),
		),

		alignmentRatio: numeric.NewDerived(
			numeric.WithDynamics(
				adaptive.NewRatio(0),
				adaptive.NewSigmaClamp(3, 8, 0.0625),
			),
		),
	}
}

/*
digestLookup returns one digest origin if present.
*/
func (field *Field) digestLookup(origin uint64) (Digest, bool) {
	if field == nil {
		return Digest{}, false
	}

	dm := field.digests.Load()

	if dm == nil || dm.m == nil {
		return Digest{}, false
	}

	d, ok := dm.m[origin]

	return d, ok
}

/*
ModeCount returns how many eigenmodes were detected in the last projection.
*/
func (field *Field) ModeCount() int {
	if field == nil {
		return 0
	}

	proj := field.projection.Load()

	if proj == nil {
		return 0
	}

	return len(proj.modes)
}

/*
ModeMembers returns a copy of member node IDs for the mode at modeIdx,
or nil if out of range.
*/
func (field *Field) ModeMembers(modeIdx int) []uint64 {
	if field == nil || modeIdx < 0 {
		return nil
	}

	proj := field.projection.Load()

	if proj == nil || modeIdx >= len(proj.modes) {
		return nil
	}

	members := proj.modes[modeIdx].Members()
	out := make([]uint64, len(members))
	copy(out, members)

	return out
}

/*
ModeEnergy returns the aggregate energy score for modeIdx, or 0 if
out of range.
*/
func (field *Field) ModeEnergy(modeIdx int) float64 {
	if field == nil || modeIdx < 0 {
		return 0
	}

	proj := field.projection.Load()

	if proj == nil || modeIdx >= len(proj.modes) {
		return 0
	}

	return proj.modes[modeIdx].Energy()
}

/*
DominantModeIndex returns the index of the highest-energy mode, or -1
if none.
*/
func (field *Field) DominantModeIndex() int {
	if field == nil {
		return -1
	}

	proj := field.projection.Load()

	if proj == nil {
		return -1
	}

	return proj.dominant
}

/*
DominantModeEnergy returns the energy of the dominant mode, or 0 if
there is no dominant mode.
*/
func (field *Field) DominantModeEnergy() float64 {
	if field == nil {
		return 0
	}

	proj := field.projection.Load()

	if proj == nil || proj.dominant < 0 || proj.dominant >= len(proj.modes) {
		return 0
	}

	return proj.modes[proj.dominant].Energy()
}

/*
Absorb integrates a received digest, recomputes eigenmodes,
and projects field pressure onto the owning node's tries.
*/
func (field *Field) Absorb(digest Digest) {
	if field == nil {
		return
	}

	if !field.absorbDigest(digest) {
		return
	}

	viz.DefaultBus.Publish(viz.GossipReceived(
		field.owner.ID, digest.Origin, digest.Epoch,
	))

	_, _ = field.Project()
}

func (field *Field) absorbDigest(digest Digest) bool {
	for {
		old := field.digests.Load()
		var base map[uint64]Digest

		if old != nil && old.m != nil {
			if existing, ok := old.m[digest.Origin]; ok && digest.Epoch <= existing.Epoch {
				return false
			}

			base = maps.Clone(old.m)
		} else {
			base = make(map[uint64]Digest)
		}

		base[digest.Origin] = digest
		newSnap := &digestMap{m: base}

		if field.digests.CompareAndSwap(old, newSnap) {
			return true
		}
	}
}

/*
Project recomputes eigenmodes and applies asymmetric pressure to the
owning node's tries based on whether each belongs to the dominant mode.

Aligned tries (in the dominant eigenmode, in-phase):
  - Decay pressure suppressed — knowledge retained longer
  - Learning pressure amplified — absorbs related input faster

Misaligned tries (outside the dominant mode, or anti-phase):
  - Decay pressure amplified — stale knowledge pruned faster
  - Learning pressure suppressed — doesn't waste effort on noise

This asymmetric fork IS the attention mechanism. No matrices, no softmax.
The field selects by differential survival.

All multipliers are derived:
  - Coupling threshold: smoothed mean of observed coupling values
  - Pressure clamps: separate smoothed spread for decay and learn streams
  - Alignment ratio: dominant mode energy / total energy, smoothed
*/
func (field *Field) Project(values ...Routable) (*algo.Prediction, error) {
	_ = values

	if field.owner == nil {
		return nil, nil
	}

	dm := field.digests.Load()

	if dm == nil || dm.m == nil || len(dm.m) < 2 {
		return nil, nil
	}

	digestSnap := maps.Clone(dm.m)

	participants := make([]geometry.ModeParticipant, 0, len(digestSnap))
	var totalEnergy float64

	affinityByOrigin := make(
		map[uint64]*primitive.Affinity,
		len(digestSnap),
	)

	for origin, digest := range digestSnap {
		affinityByOrigin[origin] = primitive.NewAffinityFromVector(digest.Affinity)

		participants = append(participants, geometry.ModeParticipant{
			Origin: origin,
			Energy: digest.SurprisalMean,
		})

		totalEnergy += digest.SurprisalMean
	}

	couplingFn := func(a, b uint64) float64 {
		affA := affinityByOrigin[a]
		affB := affinityByOrigin[b]

		return affA.Coupling(affB)
	}

	// Feed all pairwise coupling values into the threshold chain
	// so it learns the distribution and can gate meaningfully.
	for idx := range participants {
		for jdx := idx + 1; jdx < len(participants); jdx++ {
			coupling := couplingFn(
				participants[idx].Origin,
				participants[jdx].Origin,
			)

			field.couplingThreshold.Next(coupling)

			viz.DefaultBus.Publish(viz.TrieCouplingEvent(
				field.owner.ID,
				idx, jdx,
				coupling,
			))
		}
	}

	threshold := field.couplingThreshold.Value()

	modes, dominant := geometry.DetectModes(
		participants,
		threshold,
		couplingFn,
	)

	field.projection.Store(&modesProjection{modes: modes, dominant: dominant})

	if dominant >= 0 && dominant < len(modes) {
		viz.DefaultBus.Publish(viz.EigenmodeDetected(
			field.owner.ID,
			len(modes),
			modes[dominant].Energy(),
		))
	}

	// Derive alignment ratio from mode energy distribution.
	if dominant >= 0 && dominant < len(modes) && totalEnergy > 0 {
		dominantEnergy := modes[dominant].Energy()
		field.alignmentRatio.Next(dominantEnergy, totalEnergy)
	}

	tries := field.owner.triesSnapshot()

	for trieIdx := range tries {
		cluster := tries[trieIdx]

		if cluster == nil {
			continue
		}

		trieID := (field.owner.ID << 32) | uint64(uint32(trieIdx+1))

		local, hasLocal := digestSnap[trieID]

		if !hasLocal {
			continue
		}

		// Per-trie signal snapshot.
		viz.DefaultBus.Publish(viz.TrieSignalEvent(
			field.owner.ID, trieIdx,
			local.SurprisalMean, local.ClassEntropy, local.GrowthRate,
		))

		aligned := false
		modeIdx := -1

		if dominant >= 0 && dominant < len(modes) {
			if slices.Contains(modes[dominant].Members(), trieID) {
				aligned = true
				modeIdx = dominant
			}
		}

		energy := 0.0

		if modeIdx >= 0 && modeIdx < len(modes) {
			energy = modes[modeIdx].Energy()
		}

		viz.DefaultBus.Publish(viz.TrieModeEvent(
			field.owner.ID, trieIdx, modeIdx, aligned, energy,
		))

		var (
			weightedSurprisal float64
			weightedGrowth    float64
			totalWeight       float64
		)

		for origin, digest := range digestSnap {
			if origin == trieID {
				continue
			}

			phaseCoupling := field.phase.Coupling(local.SurprisalGrowth, digest.SurprisalGrowth)
			weight := primitive.NewAffinityFromVector(
				local.Affinity,
			).Coupling(
				primitive.NewAffinityFromVector(digest.Affinity),
			) * (1.0 + phaseCoupling)

			if weight <= 0 {
				continue
			}

			totalWeight += weight
			weightedSurprisal += weight * digest.SurprisalMean
			weightedGrowth += weight * digest.GrowthRate
		}

		if totalWeight == 0 {
			continue
		}

		fieldSurprisal := weightedSurprisal / totalWeight
		fieldGrowth := weightedGrowth / totalWeight

		rawDecay := fieldSurprisal - local.SurprisalMean
		rawLearn := fieldGrowth - local.GrowthRate

		field.pressureDecayClamp.Next(rawDecay)
		field.pressureLearnClamp.Next(rawLearn)

		clampLimitDecay := field.pressureDecayClamp.Value()
		clampLimitLearn := field.pressureLearnClamp.Value()

		if clampLimitDecay <= 0 {
			clampLimitDecay = 1
		}

		if clampLimitLearn <= 0 {
			clampLimitLearn = 1
		}

		// Alignment ratio acts as the asymmetric multiplier.
		// Aligned tries get the ratio as learn boost and its
		// inverse as decay suppression. Misaligned get the
		// opposite. The ratio itself is derived from mode
		// energy distribution — no config constants needed.
		ratio := field.alignmentRatio.Value()
		safeRatio := math.Max(ratio, minAlignmentRatio)
		inv := 1.0 / safeRatio

		var decayMul, learnMul float64

		if aligned {
			decayMul = inv
			learnMul = safeRatio
		} else {
			decayMul = safeRatio
			learnMul = inv
		}

		decay := clamp(rawDecay*decayMul, clampLimitDecay)
		learn := clamp(rawLearn*learnMul, clampLimitLearn)

		cluster.ApplyFieldPressure(decay, learn, decay)

		viz.DefaultBus.Publish(viz.TriePressureEvent(
			field.owner.ID, trieIdx, decay, learn, decayMul, learnMul,
		))

		viz.DefaultBus.Publish(viz.FieldPressureEvent(
			field.owner.ID, decay, learn, decay,
		))
	}

	return nil, nil
}

func clamp(val, limit float64) float64 {
	if val > limit {
		return limit
	}

	if val < -limit {
		return -limit
	}

	return val
}
