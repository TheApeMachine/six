package kadabra

import (
	"maps"
	"math"
	"sync"

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
Field lives in the context of a Kadabra Node, and forms a mesh
across all the Markov Tries that the node acts as a routing hub for.
*/
type Field struct {
	mu           sync.RWMutex
	digests      map[uint64]Digest
	owner        *Node
	modes        []geometry.Eigenmode
	dominantMode int
	phase        *geometry.Phase
}

/*
NewField constructs a new Field for the given node.
*/
func NewField(owner *Node) *Field {
	return &Field{
		digests:      make(map[uint64]Digest),
		owner:        owner,
		modes:        make([]geometry.Eigenmode, 0),
		dominantMode: -1,
		phase:        geometry.NewPhase(),
	}
}

/*
ModeCount returns how many eigenmodes were detected in the last projection.
*/
func (field *Field) ModeCount() int {
	if field == nil {
		return 0
	}

	field.mu.RLock()
	defer field.mu.RUnlock()

	return len(field.modes)
}

/*
ModeMembers returns a copy of member node IDs for the mode at modeIdx,
or nil if out of range.
*/
func (field *Field) ModeMembers(modeIdx int) []uint64 {
	if field == nil || modeIdx < 0 {
		return nil
	}

	field.mu.RLock()
	defer field.mu.RUnlock()

	if modeIdx >= len(field.modes) {
		return nil
	}

	members := field.modes[modeIdx].Members()
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

	field.mu.RLock()
	defer field.mu.RUnlock()

	if modeIdx >= len(field.modes) {
		return 0
	}

	return field.modes[modeIdx].Energy()
}

/*
DominantModeIndex returns the index of the highest-energy mode, or -1
if none.
*/
func (field *Field) DominantModeIndex() int {
	if field == nil {
		return -1
	}

	field.mu.RLock()
	defer field.mu.RUnlock()

	return field.dominantMode
}

/*
DominantModeEnergy returns the energy of the dominant mode, or 0 if
there is no dominant mode.
*/
func (field *Field) DominantModeEnergy() float64 {
	if field == nil {
		return 0
	}

	field.mu.RLock()
	defer field.mu.RUnlock()

	if field.dominantMode < 0 || field.dominantMode >= len(field.modes) {
		return 0
	}

	return field.modes[field.dominantMode].Energy()
}

/*
Absorb integrates a received digest, recomputes eigenmodes,
and projects field pressure onto the owning node's tries.
*/
func (field *Field) Absorb(digest Digest) {
	field.mu.Lock()

	if existing, ok := field.digests[digest.Origin]; ok {
		if digest.Epoch <= existing.Epoch {
			field.mu.Unlock()
			return
		}
	}

	field.digests[digest.Origin] = digest
	field.mu.Unlock()

	field.Project()
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
	if field.owner == nil {
		return nil, nil
	}

	field.mu.Lock()

	if len(field.digests) < 2 {
		field.mu.Unlock()

		return nil, nil
	}

	participants := make([]geometry.ModeParticipant, 0, len(field.digests))

	for origin, digest := range field.digests {
		participants = append(participants, geometry.ModeParticipant{
			Origin: origin,
			Energy: digest.SurprisalMean,
		})
	}

	field.modes, field.dominantMode = geometry.DetectModes(
		participants,
		modeCouplingThreshold,
		func(a, b uint64) float64 {
			dA, dB := field.digests[a], field.digests[b]
			affA := primitive.NewAffinityFromVector(dA.Affinity)
			affB := primitive.NewAffinityFromVector(dB.Affinity)
			return affA.Coupling(affB)
		},
	)

	digestSnap := maps.Clone(field.digests)
	modesSnap := field.modes
	dominantSnap := field.dominantMode
	field.mu.Unlock()

	clamp := func(val, limit float64) float64 {
		return math.Max(-limit, math.Min(limit, val))
	}

	field.owner.triesMu.RLock()
	defer field.owner.triesMu.RUnlock()

	for trieIdx := range field.owner.Tries {
		cluster := field.owner.Tries[trieIdx]
		trieID := (field.owner.ID << 32) | uint64(uint32(trieIdx+1))

		local, hasLocal := digestSnap[trieID]

		if !hasLocal {
			continue
		}

		aligned := false

		if dominantSnap >= 0 && dominantSnap < len(modesSnap) {
			dominant := modesSnap[dominantSnap]

			for _, memberID := range dominant.Members() {
				if memberID == trieID {
					aligned = true
					break
				}
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

			clusterAff := cluster.Affinity
			digestAff := primitive.NewAffinityFromVector(digest.Affinity)
			coupling := clusterAff.Coupling(digestAff)

			if coupling < digestCouplingFloor {
				continue
			}

			phaseCoupling := field.phase.Coupling(local.SurprisalGrowth, digest.SurprisalGrowth)
			weight := coupling * (1.0 + phaseCoupling)

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
