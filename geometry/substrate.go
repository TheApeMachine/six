package geometry

import (
	"math"
	"math/bits"
	"sort"

	config "github.com/theapemachine/six/core"
	"github.com/theapemachine/six/data"
)

/*
SubstrateEntry is a hybrid memory unit:
512-bit Chord filter,
PhaseDial fingerprint,
and readout payload.
Bitwise filter enables fast candidate pruning;
PhaseDial enables precise ranking.
*/
type SubstrateEntry struct {
	Filter      data.Chord
	Fingerprint PhaseDial
	Readout     []data.Chord
}

/*
HybridSubstrate stores entries and retrieves via two-phase pipeline:
(1) BitwiseFilter → top-K candidates by chord overlap,
(2) PhaseDialScoring → best match.
*/
type HybridSubstrate struct {
	Entries []SubstrateEntry
}

/*
NewHybridSubstrate allocates an empty HybridSubstrate
ready for Add operations.
*/
func NewHybridSubstrate() *HybridSubstrate {
	return &HybridSubstrate{
		Entries: make([]SubstrateEntry, 0),
	}
}

/*
Add appends a new SubstrateEntry (filter, fingerprint, readout)
to the substrate.
*/
func (hs *HybridSubstrate) Add(
	filter data.Chord,
	fingerprint PhaseDial,
	readout []data.Chord,
) {
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
func (hs *HybridSubstrate) Retrieve(
	contextFilter data.Chord,
	contextFingerprint PhaseDial,
	topK int,
) []data.Chord {
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
func (hs *HybridSubstrate) BitwiseFilter(
	contextFilter data.Chord, topK int,
) []int {
	type candidateScore struct {
		idx   int
		score int
	}

	if topK <= 0 || len(hs.Entries) == 0 {
		return []int{}
	}

	if topK > len(hs.Entries) {
		topK = len(hs.Entries)
	}

	scores := make([]candidateScore, 0, topK)

	for idx, entry := range hs.Entries {
		matchCount := 0
		noiseCount := 0

		for j := range config.Numeric.ChordBlocks {
			matchCount += bits.OnesCount64(entry.Filter[j] & contextFilter[j])
			noiseCount += bits.OnesCount64(entry.Filter[j] &^ contextFilter[j])
		}

		score := int((float64(matchCount) / float64(matchCount+noiseCount+1)) * 1e6)
		insertAt := len(scores)

		for i := range scores {
			if score > scores[i].score {
				insertAt = i
				break
			}
		}

		if insertAt == len(scores) && len(scores) == topK {
			continue
		}

		scores = append(scores, candidateScore{})
		copy(scores[insertAt+1:], scores[insertAt:])
		scores[insertAt] = candidateScore{idx: idx, score: score}

		if len(scores) > topK {
			scores = scores[:topK]
		}
	}

	result := make([]int, len(scores))

	for i := range scores {
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
func (hs *HybridSubstrate) PhaseDialRank(
	candidates []int, contextFingerprint PhaseDial,
) []CandidateScore {
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
func (hs *HybridSubstrate) PhaseDialScoring(
	candidates []int, contextFingerprint PhaseDial,
) int {
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
complexCosineSimilarity returns the real part of the normalized
Hermitian inner product. Same as PhaseDial.Similarity; internal
helper for substrate scoring.
*/
func (hs *HybridSubstrate) complexCosineSimilarity(a, b PhaseDial) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dotReal float64
	var normA, normB float64

	for i := range a {
		ar := real(a[i])
		ai := imag(a[i])
		br := real(b[i])
		bi := imag(b[i])

		dotReal += ar*br + ai*bi
		normA += ar*ar + ai*ai
		normB += br*br + bi*bi
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotReal / (math.Sqrt(normA) * math.Sqrt(normB))
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
HopResult holds the result of FirstHop: fingerprint at B,
composed A+B fingerprint, and readout chords.
*/
type HopResult struct {
	FingerprintB  PhaseDial
	FingerprintAB PhaseDial
	ReadoutB      []data.Chord
}

/*
FirstHop finds the best PhaseDial match for seedFP rotated by alpha,
composes the seed and match fingerprints, and returns the result.
Excludes entries whose readout matches excludeReadout.
*/
func (hs *HybridSubstrate) FirstHop(
	seedFP PhaseDial, alpha float64, excludeReadout []data.Chord,
) HopResult {
	rotated := seedFP.Rotate(alpha)
	candidates := hs.Candidates()
	ranked := hs.PhaseDialRank(candidates, rotated)

	bestIdx := hs.TopExcluding(ranked, excludeReadout)
	bestEntry := hs.Entries[bestIdx]

	// Compose: element-wise product of seed and matched fingerprints
	composed := make(PhaseDial, len(seedFP))
	for k := range seedFP {
		if k < len(bestEntry.Fingerprint) {
			composed[k] = seedFP[k] * bestEntry.Fingerprint[k]
		}
	}

	return HopResult{
		FingerprintB:  bestEntry.Fingerprint,
		FingerprintAB: composed,
		ReadoutB:      bestEntry.Readout,
	}
}

/*
TopExcluding returns the index of the highest-ranked candidate whose
readout does not match any of the excluded chord sequences.
Falls back to the top candidate if all are excluded.
*/
func (hs *HybridSubstrate) TopExcluding(
	ranked []CandidateScore, excluded ...[]data.Chord,
) int {
	for _, cand := range ranked {
		readout := hs.Entries[cand.Idx].Readout
		skip := false

		for _, excl := range excluded {
			if chordsEqual(readout, excl) {
				skip = true
				break
			}
		}

		if !skip {
			return cand.Idx
		}
	}

	if len(ranked) > 0 {
		return ranked[0].Idx
	}

	return 0
}

func chordsEqual(a, b []data.Chord) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

/*
GeodesicScan rotates seedFP by stepDeg at each step 0..steps.
For each step, ranks all candidates and records Phase, Margin,
Entropy, BestIdx, BestReadout, Ranked.
*/
func (hs *HybridSubstrate) GeodesicScan(
	seedFP PhaseDial, steps int, stepDeg float64,
) []GeodesicStep {
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
	BestReadout []data.Chord
	Ranked      []CandidateScore
}

/*
BestGain sweeps 360° of single-axis rotation on queryFP, excludes entries
matching any of the excluded chord sequences, and returns the best
min(sim(C, refA), sim(C, refB)) score. This is the 1D baseline for
comparison against torus (2D) gains.
*/
func (hs *HybridSubstrate) BestGain(
	queryFP PhaseDial,
	refA, refB PhaseDial,
	excluded ...[]data.Chord,
) float64 {
	candidates := hs.Candidates()
	bestGain := -1.0

	for s := range 360 {
		alpha := float64(s) * (math.Pi / 180.0)
		rotated := queryFP.Rotate(alpha)
		ranked := hs.PhaseDialRank(candidates, rotated)
		topIdx := hs.TopExcluding(ranked, excluded...)
		fpC := hs.Entries[topIdx].Fingerprint
		gain := math.Min(fpC.Similarity(refA), fpC.Similarity(refB))

		if gain > bestGain {
			bestGain = gain
		}
	}

	return bestGain
}
