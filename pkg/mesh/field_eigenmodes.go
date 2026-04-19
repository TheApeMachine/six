package mesh

import (
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
eigenmodeCouplingThreshold is the Jaccard similarity above which two
members share a mode. 0.55 was picked because at 257-bit affinity the
expected Jaccard for random unrelated fingerprints is ~0.33 and for
closely-related ones (single SimHash projection differing) is ≥0.8 —
a threshold in the middle of that gap keeps the partition meaningful
without forcing every Value into one giant mode.
*/
const eigenmodeCouplingThreshold = 0.55

/*
detectEigenmodes runs the greedy DetectModes partition over the current
member population, using Jaccard-over-affinity as the coupling
function. The result is stashed into field.snap so a Write caller
doesn't have to block on this work — the snap is a copy-on-write
snapshot the hot path reads without locks.

Affinity arrays are materialised once up front into an index-aligned
slab so the coupling closure does not touch the Value heap on every
call. Energy is the reduced-surprisal popcount (see surprisalEnergy)
which tends to rank the newest/most-divergent members higher — exactly
the population the README says should drive eigenmode ranking.
*/
func (field *Field) detectEigenmodes() *geometry.EigenSnap {
	return detectEigenmodesFromMembers(field.values)
}

func detectEigenmodesFromMembers(members []*primitive.Value) *geometry.EigenSnap {
	if len(members) == 0 {
		return &geometry.EigenSnap{}
	}

	// Materialise affinity fingerprints once. An id→index map keeps the
	// DetectModes closure's lookups O(1) without retaining any Value
	// pointers beyond the lifetime of this function.
	participants := make([]geometry.ModeParticipant, 0, len(members))
	fingers := make([][primitive.AffinityWords]uint64, 0, len(members))
	idToIdx := make(map[uint64]int, len(members))

	for _, value := range members {
		if value == nil {
			continue
		}

		origin := value.ID()

		// Duplicate IDs shouldn't happen inside a community but if they do
		// we keep the first occurrence — DetectModes dedups by origin so a
		// second entry would silently inflate the population count.
		if _, seen := idToIdx[origin]; seen {
			continue
		}

		idToIdx[origin] = len(participants)
		participants = append(participants, geometry.ModeParticipant{
			Origin: origin,
			Energy: surprisalEnergy(value),
		})
		fingers = append(fingers, loadAffinityArray(value))
	}

	if len(participants) == 0 {
		return &geometry.EigenSnap{}
	}

	couplingFn := func(a, b uint64) float64 {
		ia, okA := idToIdx[a]
		ib, okB := idToIdx[b]

		if !okA || !okB {
			return 0
		}

		return JaccardCouplingAffinity(fingers[ia], fingers[ib])
	}

	modes, dominantIdx := geometry.DetectModes(
		participants, eigenmodeCouplingThreshold, couplingFn,
	)

	return geometry.NewEigenSnap(modes, dominantIdx)
}

/*
updatePhaseDialFromMembers re-encodes the field's PhaseDial from the current
member population. The dial is the 512-dimensional complex fingerprint
two fields use to test alignment — rebuilding it per Cycle keeps it in
step with the population's actual structure rather than drifting on
stale encoded state.

Values are materialised by dereference rather than pointer to match
geometry.EncodeFromValues' signature; the copy is a 1 KB memmove per
member which is already cheaper than the per-dimension trig the
encoder runs.
*/
func (field *Field) updatePhaseDialFromMembers(members []*primitive.Value) {
	if len(members) == 0 {
		return
	}

	// Encoder wants []Value, not []*Value. Staged is built from the
	// snapshot so it cannot race with concurrent appends to field.values.
	staged := make([]primitive.Value, 0, len(members))

	for _, value := range members {
		if value == nil {
			continue
		}

		staged = append(staged, *value)
	}

	if len(staged) == 0 {
		return
	}

	dial := field.dial
	if len(dial) < geometry.PhaseDialDimensions {
		dial = geometry.NewPhaseDial()
	}

	encoded := dial.EncodeFromValues(staged)

	field.dial = encoded
}

/*
dominantEnergyRatio returns the concentration of the field's dominant
eigenmode: dominant.Energy / Σ mode.Energy, bounded into [0,1]. A
uniform population (no dominant cluster) returns 0; a field that has
fully collapsed onto a single mode returns 1.

This is the natural "pressure multiplier" the README talks about: low
ratio → diffuse → the field should emit carriers to consolidate, high
ratio → crystallised → the field can stand pat.
*/
func dominantEnergyRatio(snap *geometry.EigenSnap) float64 {
	if snap == nil {
		return 0
	}

	modes := snap.Modes()

	if len(modes) == 0 {
		return 0
	}

	dominantIdx := snap.DominantIdx()

	if dominantIdx < 0 || dominantIdx >= len(modes) {
		return 0
	}

	var total float64

	for idx := range modes {
		total += modes[idx].Energy()
	}

	if total == 0 {
		return 0
	}

	return modes[dominantIdx].Energy() / total
}
