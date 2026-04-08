package geometry

import "github.com/theapemachine/six/pkg/core/numeric/gf"

/*
Eigenmode represents a cluster of field participants whose affinity
vectors are mutually coupled above a threshold. The members share
structural resonance — they respond to similar input topology. Energy
is the aggregate surprisal mass of the mode, used to rank dominance.
*/
type Eigenmode struct {
	members []uint64
	energy  float64
}

/*
Members returns the participant IDs in this mode.
*/
func (mode *Eigenmode) Members() []uint64 {
	return append([]uint64(nil), mode.members...)
}

/*
Energy returns the aggregate energy score.
*/
func (mode *Eigenmode) Energy() float64 {
	return mode.energy
}

/*
ModeParticipant carries the minimum signals needed for eigenmode
detection: an origin ID and a scalar energy contribution.
*/
type ModeParticipant struct {
	Origin uint64
	Energy float64
}

/*
PhaseMode is the dominant finite-field phase extracted from a vector.
The lane index acts as the phase angle; amplitude and concentration
describe how collapsed the vector is around that lane.
*/
type PhaseMode struct {
	Index         int
	Amplitude     uint32
	Concentration float64
}

/*
DetectPhaseMode257 extracts the dominant lane from a trie-local phase vector.
*/
func DetectPhaseMode257(phaseVector gf.Vector257) PhaseMode {
	return phaseModeFromDominant(phaseVector.Dominant())
}

/*
DetectPhaseMode8191 extracts the dominant lane from a node phase vector.
*/
func DetectPhaseMode8191(phaseVector gf.Vector8191) PhaseMode {
	return phaseModeFromDominant(phaseVector.Dominant())
}

/*
DetectPhaseMode65537 extracts the dominant lane from a global phase vector.
*/
func DetectPhaseMode65537(phaseVector gf.Vector65537) PhaseMode {
	return phaseModeFromDominant(phaseVector.Dominant())
}

/*
PhaseAlignment measures circular agreement between two dominant phase lanes.
*/
func PhaseAlignment(leftMode PhaseMode, rightMode PhaseMode) float64 {
	return gf.Alignment(leftMode.Index, rightMode.Index)
}

/*
DetectModes partitions participants into eigenmodes by greedy affinity
clustering. Two participants belong to the same mode when couplingFn
returns a value at or above the threshold. Returns the modes and the
index of the dominant (highest energy) mode, or -1 if none.

couplingFn receives two origin IDs and returns the coupling strength
between them. This keeps the geometry package free of domain types.
*/
func DetectModes(
	participants []ModeParticipant,
	couplingThreshold float64,
	couplingFn func(a, b uint64) float64,
) ([]Eigenmode, int) {
	assigned := make(map[uint64]bool, len(participants))
	modes := make([]Eigenmode, 0)

	for _, pA := range participants {
		if assigned[pA.Origin] {
			continue
		}

		mode := Eigenmode{
			members: []uint64{pA.Origin},
			energy:  pA.Energy,
		}

		assigned[pA.Origin] = true

		for _, pB := range participants {
			if assigned[pB.Origin] {
				continue
			}

			if couplingFn(pA.Origin, pB.Origin) >= couplingThreshold {
				mode.members = append(mode.members, pB.Origin)
				mode.energy += pB.Energy
				assigned[pB.Origin] = true
			}
		}

		modes = append(modes, mode)
	}

	if len(modes) == 0 {
		return modes, -1
	}

	dominantIdx := 0
	maxEnergy := modes[0].energy

	for idx := 1; idx < len(modes); idx++ {
		if modes[idx].energy > maxEnergy {
			maxEnergy = modes[idx].energy
			dominantIdx = idx
		}
	}

	return modes, dominantIdx
}

func phaseModeFromDominant(dominantPhase gf.DominantPhase) PhaseMode {
	return PhaseMode{
		Index:         dominantPhase.Index,
		Amplitude:     dominantPhase.Amplitude,
		Concentration: dominantPhase.Concentration,
	}
}
