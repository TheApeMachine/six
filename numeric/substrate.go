package numeric

import (
	"math"
	"math/bits"
	"math/cmplx"
	"sort"
	"unsafe"

	"github.com/theapemachine/six/gpu/metal"
)

/*
Chord represents a 512-bit candidate filter used for high-speed collision-compressed memory.
Stored as 8 blocks of uint64.
*/
type Chord [ChordBlocks]uint64

/*
PhaseDial represents the complex fingerprint for an entry, using 64-256 dimensions.
*/
type PhaseDial []complex128

/*
SubstrateEntry is a hybrid memory structure that keeps the highly compressed bitwise
Chord along with a continuous complex Phase Dial fingerprint for precise scoring.
*/
type SubstrateEntry struct {
	Filter      Chord
	Fingerprint PhaseDial
	Readout     []byte
}

/*
HybridSubstrate provides the storage and retrieval pipeline that uses
bitwise filtering for speed, followed by Phase Dial scoring.
*/
type HybridSubstrate struct {
	Entries []SubstrateEntry
}

func NewHybridSubstrate() *HybridSubstrate {
	return &HybridSubstrate{
		Entries: make([]SubstrateEntry, 0),
	}
}

/*
Add stores a new entry in the hybrid memory substrate.
*/
func (hs *HybridSubstrate) Add(filter Chord, fingerprint PhaseDial, readout []byte) {
	hs.Entries = append(hs.Entries, SubstrateEntry{
		Filter:      filter,
		Fingerprint: fingerprint,
		Readout:     readout,
	})
}

/*
Retrieve executes the two-phase pipeline:
1. bitwise filter -> top-K candidates
2. phase dial scoring on those candidates -> select/mix
3. readout
*/
func (hs *HybridSubstrate) Retrieve(contextFilter Chord, contextFingerprint PhaseDial, topK int) []byte {
	if len(hs.Entries) == 0 {
		return nil
	}

	// Phase 1: Bitwise Filter (get top-K candidates)
	candidates := hs.BitwiseFilter(contextFilter, topK)

	if len(candidates) == 0 {
		return nil
	}

	// Phase 2: Phase Dial Scoring
	bestIdx := hs.PhaseDialScoring(candidates, contextFingerprint)

	// Phase 3: Readout
	return hs.Entries[bestIdx].Readout
}

/*
BitwiseFilter scores all memory entries using the 512-bit Chord. 
Currently implements a software popcount for top-K. The Metal GPU implementation
could be updated to return a top-K buffer.
*/
func (hs *HybridSubstrate) BitwiseFilter(contextFilter Chord, topK int) []int {
	type candidateScore struct {
		idx   int
		score int
	}

	scores := make([]candidateScore, len(hs.Entries))
	for i, entry := range hs.Entries {
		matchCount := 0
		noiseCount := 0

		for j := 0; j < ChordBlocks; j++ {
			cBits := entry.Filter[j]
			aBits := contextFilter[j]

			// same logic as the Metal bitwise shader
			matchCount += bits.OnesCount64(cBits & aBits)
			noiseCount += bits.OnesCount64(cBits & ^aBits)
		}

		// simplified resonance score (unscaled)
		// For accurate sorting, higher match/noise ratio is better
		// We use an integer score logic roughly similar to float scaling
		resScore := int((float64(matchCount) / float64(matchCount+noiseCount+1)) * 1e6)
		scores[i] = candidateScore{idx: i, score: resScore}
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score // Descending
	})

	if topK > len(scores) {
		topK = len(scores)
	}

	result := make([]int, topK)
	for i := 0; i < topK; i++ {
		result[i] = scores[i].idx
	}

	return result
}

/*
CandidateScore explicitly binds an entry index to its evaluation score.
*/
type CandidateScore struct {
	Idx   int
	Score float64
}

/*
PhaseDialRank evaluates all passed candidates against the context fingerprint and returns
them strictly ordered by score (highest to lowest).
*/
func (hs *HybridSubstrate) PhaseDialRank(candidates []int, contextFingerprint PhaseDial) []CandidateScore {
	scores := make([]CandidateScore, len(candidates))
	for i, idx := range candidates {
		entry := hs.Entries[idx]
		score := hs.complexCosineSimilarity(entry.Fingerprint, contextFingerprint)
		scores[i] = CandidateScore{Idx: idx, Score: score}
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].Score > scores[j].Score // Descending order
	})
	
	return scores
}

/*
PhaseDialScoring finds the highest correlating SubstrateEntry among the candidates
by scoring against the complex fingerprint.
*/
func (hs *HybridSubstrate) PhaseDialScoring(candidates []int, contextFingerprint PhaseDial) int {
	bestIdx := -1
	var bestScore float64 = -math.MaxFloat64

	for _, idx := range candidates {
		entry := hs.Entries[idx]
		score := hs.complexCosineSimilarity(entry.Fingerprint, contextFingerprint)
		
		if score > bestScore {
			bestScore = score
			bestIdx = idx
		}
	}

	return bestIdx
}

/*
complexCosineSimilarity calculates the similarity between two complex phase dials.
It computes the dot product of the two vectors, normalized by their magnitudes.
*/
func (hs *HybridSubstrate) complexCosineSimilarity(a, b PhaseDial) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot complex128
	var normA, normB float64

	for i := range a {
		// Conjugate of a[i] * b[i]
		valA := a[i]
		valB := b[i]
		dot += cmplx.Conj(valA) * valB

		normA += real(valA)*real(valA) + imag(valA)*imag(valA)
		normB += real(valB)*real(valB) + imag(valB)*imag(valB)
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	// Real projection of the dot product over the norms
	return real(dot) / (math.Sqrt(normA) * math.Sqrt(normB))
}

/*
FastGPUFilter provides an example of utilizing the existing Metal BestFill 
capability as a fast path for exact 1-nearest-neighbor. 
Since current Metal compute only gives the very best result, this acts 
as a bridging utility.
*/
func (hs *HybridSubstrate) FastGPUFilter(contextFilter Chord) (int, float64, error) {
	if len(hs.Entries) == 0 {
		return 0, 0, nil
	}

	// We need to pack the filters contiguously to send to the GPU.
	// Since SubstrateEntry has extra fields, we construct a flat array of just Chords.
	dictionary := make([]Chord, len(hs.Entries))
	for i, entry := range hs.Entries {
		dictionary[i] = entry.Filter
	}

	return metal.BestFill(unsafe.Pointer(&dictionary[0]), len(dictionary), unsafe.Pointer(&contextFilter), 0)
}
