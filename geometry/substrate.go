package geometry

import (
	"encoding/binary"
	"math"
	"math/bits"
	"math/cmplx"
	"slices"
	"sort"
	"strings"

	config "github.com/theapemachine/six/core"
	"github.com/theapemachine/six/data"
)

/*
SubstrateEntry is a hybrid memory unit: 512-bit Chord filter, PhaseDial fingerprint, and readout payload.
Bitwise filter enables fast candidate pruning; PhaseDial enables precise ranking.
*/
type SubstrateEntry struct {
	Filter      data.Chord
	Fingerprint PhaseDial
	Readout     []byte
}

/*
HybridSubstrate stores entries and retrieves via two-phase pipeline:
(1) BitwiseFilter → top-K candidates by chord overlap, (2) PhaseDialScoring → best match.
*/
type HybridSubstrate struct {
	Entries []SubstrateEntry
}

/*
NewHybridSubstrate allocates an empty HybridSubstrate ready for Add operations.
*/
func NewHybridSubstrate() *HybridSubstrate {
	return &HybridSubstrate{
		Entries: make([]SubstrateEntry, 0),
	}
}

/*
Add appends a new SubstrateEntry (filter, fingerprint, readout) to the substrate.
*/
func (hs *HybridSubstrate) Add(filter data.Chord, fingerprint PhaseDial, readout []byte) {
	hs.Entries = append(hs.Entries, SubstrateEntry{
		Filter:      filter,
		Fingerprint: fingerprint,
		Readout:     readout,
	})
}

/*
Filters returns a contiguous array of all candidate Filter chords,
suitable for dispatching to the GPU for completely separate resonance scoring.
*/
func (hs *HybridSubstrate) Filters() []data.Chord {
	if len(hs.Entries) == 0 {
		return nil
	}
	filters := make([]data.Chord, len(hs.Entries))
	for i, e := range hs.Entries {
		filters[i] = e.Filter
	}
	return filters
}

/*
Retrieve runs the two-phase pipeline: BitwiseFilter → PhaseDialScoring → readout.
Returns the Readout of the best-matching entry, or nil if no entries or no candidates.
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
BitwiseFilter scores entries by chord overlap (match/noise ratio).
Returns top-K entry indices descending by score. Software popcount; GPU could accelerate.
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
			off := j * 8
			cBits := binary.LittleEndian.Uint64(entry.Filter.Bytes()[off : off+8])
			aBits := binary.LittleEndian.Uint64(contextFilter.Bytes()[off : off+8])

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
CandidateScore binds an entry index to its PhaseDial similarity score.
*/
type CandidateScore struct {
	Idx   int
	Score float64
}

/*
PhaseDialRank scores candidates by complex cosine similarity to contextFingerprint.
Returns CandidateScore slice ordered descending by Score.
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
PhaseDialScoring returns the index of the candidate with highest PhaseDial similarity.
Returns -1 if no candidates.
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
complexCosineSimilarity returns the real part of the normalized Hermitian inner product.
Same as PhaseDial.Similarity; internal helper for substrate scoring.
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
ReadoutText parses "idx: payload" format, returning the payload part.
Falls back to raw readout if no ": " separator.
*/
func ReadoutText(readout []byte) string {
	parts := strings.SplitN(string(readout), ": ", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return string(readout)
}

/*
Candidates returns indices 0..len(Entries)-1 for full-substrate PhaseDialRank scans.
*/
func (hs *HybridSubstrate) Candidates() []int {
	c := make([]int, len(hs.Entries))
	for i := range c {
		c[i] = i
	}
	return c
}

/*
TopExcluding returns the highest-ranked entry index whose ReadoutText is not in excluded.
Falls back to ranked[0].Idx if all are excluded.
*/
func (hs *HybridSubstrate) TopExcluding(ranked []CandidateScore, excluded ...string) int {
	for _, r := range ranked {
		text := ReadoutText(hs.Entries[r.Idx].Readout)
		found := slices.Contains(excluded, text)
		if !found {
			return r.Idx
		}
	}
	return ranked[0].Idx // Fallback
}

/*
BestGain sweeps fpScan through 360 rotation steps; at each step ranks candidates
and selects the top non-excluded entry. Returns the maximum over all steps of
min(Similarity(efp,fpA), Similarity(efp,fpB)) where efp is that entry's fingerprint.
*/
func (hs *HybridSubstrate) BestGain(fpScan, fpA, fpB PhaseDial, excludeA, excludeB string) float64 {
	bestGain := -1.0
	for s := range 360 {
		alpha := float64(s) * (math.Pi / 180.0)
		rot := fpScan.Rotate(alpha)
		rnk := hs.PhaseDialRank(hs.Candidates(), rot)
		topIdx := hs.TopExcluding(rnk, excludeA, excludeB)
		efp := hs.Entries[topIdx].Fingerprint
		gain := math.Min(efp.Similarity(fpA), efp.Similarity(fpB))
		if gain > bestGain {
			bestGain = gain
		}
	}
	return bestGain
}

/*
HopResult holds the result of FirstHop: fingerprint at B, composed A+B fingerprint, and readout text.
*/
type HopResult struct {
	FingerprintB  PhaseDial
	FingerprintAB PhaseDial
	TextB         string
}

/*
FirstHop rotates fpA by alpha, ranks candidates, selects best excluding textA.
Returns HopResult with fpB, composed fpAB (A+B), and textB.
*/
func (hs *HybridSubstrate) FirstHop(fpA PhaseDial, alpha float64, textA string) HopResult {
	fpB_virtual := fpA.Rotate(alpha)
	rnk := hs.PhaseDialRank(hs.Candidates(), fpB_virtual)
	idxB := hs.TopExcluding(rnk, textA)
	fpB := hs.Entries[idxB].Fingerprint
	textB := ReadoutText(hs.Entries[idxB].Readout)

	// Composed fingerprint (A + B)
	fpAB := make(PhaseDial, len(fpA))
	for k := range fpA {
		fpAB[k] = fpA[k] + fpB[k]
	}

	return HopResult{
		FingerprintB:  fpB,
		FingerprintAB: fpAB,
		TextB:         textB,
	}
}

/*
GeodesicScan rotates seedFP by stepDeg at each step 0..steps.
For each step, ranks all candidates and records Phase, Margin, Entropy, BestIdx, BestReadout, Ranked.
*/
func (hs *HybridSubstrate) GeodesicScan(seedFP PhaseDial, steps int, stepDeg float64) []GeodesicStep {
	candidates := hs.Candidates()
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

/*
GeodesicStep holds one step of a GeodesicScan: phase angle, margin, entropy, best index/readout, ranked scores.
*/
type GeodesicStep struct {
	Phase       float64
	Margin      float64
	Entropy     float64
	BestIdx     int
	BestReadout []byte
	Ranked      []CandidateScore
}
