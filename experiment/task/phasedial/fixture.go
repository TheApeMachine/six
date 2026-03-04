package phasedial

// fixture.go — shared test helpers for the phasedial experiment suite.
//
// Provides:
//   - NewSubstrate()          builds and returns the ingested aphorism substrate + candidates
//   - FirstHop(...)           resolves anchor B from a rotated seed, returns fpB/textB/fpAB
//   - BestExcluding(...)      sweeps 360° over an anchor, returns best gain excluding two texts
//   - TopExcluding(...)       returns the top-ranked corpus index excluding two texts

import (
	"fmt"
	"math"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
)

// Substrate bundles the ingested substrate with a pre-built all-candidates slice.
type Substrate struct {
	*geometry.HybridSubstrate
	Candidates []int
}

// NewSubstrate ingests Aphorisms into a fresh HybridSubstrate and returns it
// together with a full all-candidates index slice.
func NewSubstrate() Substrate {
	sub := geometry.NewHybridSubstrate()
	var universalFilter data.Chord
	for i, text := range Aphorisms {
		fp := geometry.NewPhaseDial().Encode(text)
		sub.Add(universalFilter, fp, []byte(fmt.Sprintf("%d: %s", i, text)))
	}
	cands := make([]int, len(sub.Entries))
	for i := range cands {
		cands[i] = i
	}
	return Substrate{HybridSubstrate: sub, Candidates: cands}
}

// FirstHopResult holds the outputs of a first-hop resolution.
type FirstHopResult struct {
	FingerprintB geometry.PhaseDial
	TextB        string
	FingerprintAB geometry.PhaseDial
}

// FirstHop rotates fpA by alpha1Rad, finds the top-ranked corpus item that is
// not the seed query, and returns its fingerprint, text, and the midpoint F_AB.
func (s Substrate) FirstHop(fpA geometry.PhaseDial, alpha1Rad float64, seedQuery string) FirstHopResult {
	rotated := fpA.Rotate(alpha1Rad)
	ranked := s.PhaseDialRank(s.Candidates, rotated)
	best := ranked[0]
	for _, r := range ranked {
		if geometry.ReadoutText(s.Entries[r.Idx].Readout) != seedQuery {
			best = r
			break
		}
	}
	fpB := s.Entries[best.Idx].Fingerprint
	textB := geometry.ReadoutText(s.Entries[best.Idx].Readout)
	return FirstHopResult{
		FingerprintB:  fpB,
		TextB:         textB,
		FingerprintAB: fpA.ComposeMidpoint(fpB),
	}
}

// TopExcluding returns the index of the top-ranked entry that is neither
// excludeA nor excludeB.
func (s Substrate) TopExcluding(ranked []geometry.CandidateScore, excludeA, excludeB string) int {
	idx := ranked[0].Idx
	for _, r := range ranked {
		if ct := geometry.ReadoutText(s.Entries[r.Idx].Readout); ct != excludeA && ct != excludeB {
			idx = r.Idx
			break
		}
	}
	return idx
}

// BestGain sweeps 360 unit-degree rotations of anchor, skipping corpus items
// whose text equals excludeA or excludeB, and returns the best
// min(sim(C, fpA), sim(C, fpB)) found.
func (s Substrate) BestGain(anchor, fpA, fpB geometry.PhaseDial, excludeA, excludeB string) float64 {
	best := -1.0
	for deg := range 360 {
		rotated := anchor.Rotate(float64(deg) * (math.Pi / 180.0))
		ranked := s.PhaseDialRank(s.Candidates, rotated)
		topIdx := s.TopExcluding(ranked, excludeA, excludeB)
		fpC := s.Entries[topIdx].Fingerprint
		if g := math.Min(fpC.Similarity(fpA), fpC.Similarity(fpB)); g > best {
			best = g
		}
	}
	return best
}
