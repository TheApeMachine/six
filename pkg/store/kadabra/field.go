package kadabra

import (
	"maps"
	"math"
	"slices"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
modeCouplingThreshold is the minimum affinity coupling for two digests
to be considered part of the same eigenmode. Below this, they are
structurally unrelated regardless of phase.
*/
const modeCouplingThreshold = 0.15

const digestCouplingFloor = 0.01

const clampDecayLearnLimit = 3.0

const clampPruneLimit = 5.0

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
*/
type Field struct {
	digests    atomic.Pointer[digestMap]
	projection atomic.Pointer[modesProjection]
	owner      *Node
	phase      *geometry.Phase
}

/*
NewField constructs a new Field for the given node.
*/
func NewField(owner *Node) *Field {
	return &Field{
		owner: owner,
		phase: geometry.NewPhase(),
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

	for origin, digest := range digestSnap {
		participants = append(participants, geometry.ModeParticipant{
			Origin: origin,
			Energy: digest.SurprisalMean,
		})
	}

	modes, dominant := geometry.DetectModes(
		participants,
		modeCouplingThreshold,
		func(a, b uint64) float64 {
			dA, dB := digestSnap[a], digestSnap[b]
			affA := primitive.NewAffinityFromVector(dA.Affinity)
			affB := primitive.NewAffinityFromVector(dB.Affinity)

			return affA.Coupling(affB)
		},
	)

	field.projection.Store(&modesProjection{modes: modes, dominant: dominant})

	clamp := func(val, limit float64) float64 {
		return math.Max(-limit, math.Min(limit, val))
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

		aligned := false

		if dominant >= 0 && dominant < len(modes) {
			dominantMode := modes[dominant]

			if slices.Contains(dominantMode.Members(), trieID) {
				aligned = true
			}
		}

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
		growthDelta := fieldGrowth - local.GrowthRate
		rawLearn := growthDelta
		rawPrune := growthDelta

		var decayMul, learnMul float64

		if aligned {
			decayMul = core.Cfg.Kadabra.AlignedDecayMultiplier
			learnMul = core.Cfg.Kadabra.AlignedLearnMultiplier
		} else {
			decayMul = core.Cfg.Kadabra.MisalignedDecayMultiplier
			learnMul = core.Cfg.Kadabra.MisalignedLearnMultiplier
		}

		cluster.ApplyFieldPressure(
			clamp(rawDecay*decayMul, clampDecayLearnLimit),
			clamp(rawLearn*learnMul, clampDecayLearnLimit),
			clamp(rawPrune, clampPruneLimit),
		)
	}

	return nil, nil
}
