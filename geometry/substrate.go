package geometry

import (
	"math"
	"math/bits"
	"math/cmplx"
	"sort"
	"strings"

	config "github.com/theapemachine/six/core"
	"github.com/theapemachine/six/data"
)

/*
Stored as 8 blocks of uint64.
*/
/*
SubstrateEntry is a hybrid memory structure that keeps the highly compressed bitwise
Chord along with a continuous complex Phase Dial fingerprint for precise scoring.
*/
type SubstrateEntry struct {
	Filter      data.Chord
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
func (hs *HybridSubstrate) Add(filter data.Chord, fingerprint PhaseDial, readout []byte) {
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
func (hs *HybridSubstrate) Retrieve(contextFilter data.Chord, contextFingerprint PhaseDial, topK int) []byte {
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
func (hs *HybridSubstrate) BitwiseFilter(contextFilter data.Chord, topK int) []int {
	type candidateScore struct {
		idx   int
		score int
	}

	scores := make([]candidateScore, len(hs.Entries))
	for i, entry := range hs.Entries {
		matchCount := 0
		noiseCount := 0

		for j := 0; j < config.Numeric.ChordBlocks; j++ {
			cBits := entry.Filter.Bytes()[j]
			aBits := contextFilter.Bytes()[j]

			// same logic as the Metal bitwise shader
			matchCount += bits.OnesCount64(uint64(cBits) & uint64(aBits))
			noiseCount += bits.OnesCount64(uint64(cBits) & ^uint64(aBits))
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
ReadoutText returns the payload from readout bytes stored as "idx: payload".
Used when entries are added with fmt.Sprintf("%d: %s", i, text).
*/
func ReadoutText(readout []byte) string {
	parts := strings.SplitN(string(readout), ": ", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return string(readout)
}

/*
GeodesicScan performs a phase rotation scan: for each step 0..steps, rotates
seedFP by step*(360/steps) degrees, ranks all candidates, and returns step results.
Candidates are all entry indices. StepDeg is degrees per step (e.g. 5.0 for 73 steps).
*/
func (hs *HybridSubstrate) GeodesicScan(seedFP PhaseDial, steps int, stepDeg float64) []GeodesicStep {
	candidates := make([]int, len(hs.Entries))
	for i := range candidates {
		candidates[i] = i
	}
	var results []GeodesicStep
	for s := 0; s <= steps; s++ {
		alphaRadians := float64(s) * stepDeg * (math.Pi / 180.0)
		rotated := seedFP.Rotate(alphaRadians)
		ranked := hs.PhaseDialRank(candidates, rotated)
		best := ranked[0]
		margin := 0.0
		if len(ranked) > 1 {
			margin = best.Score - ranked[1].Score
		}
		var entropy float64
		pSum := 0.0
		for _, r := range ranked {
			pSum += math.Max(0, r.Score)
		}
		if pSum > 0 {
			for _, r := range ranked {
				p := math.Max(0, r.Score) / pSum
				if p > 0 {
					entropy -= p * math.Log2(p)
				}
			}
		}
		results = append(results, GeodesicStep{
			Phase:       float64(s) * stepDeg,
			Margin:      margin,
			Entropy:     entropy,
			BestIdx:     best.Idx,
			BestReadout: hs.Entries[best.Idx].Readout,
			Ranked:      ranked,
		})
	}
	return results
}

// GeodesicStep holds one step of a geodesic phase scan.
type GeodesicStep struct {
	Phase       float64
	Margin      float64
	Entropy     float64
	BestIdx     int
	BestReadout []byte
	Ranked      []CandidateScore
}
