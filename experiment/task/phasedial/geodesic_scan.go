package phasedial

import (
	"fmt"
	"math"
	"math/cmplx"
	"strings"

	"github.com/theapemachine/six/numeric"
)

func (experiment *Experiment) runGeodesicScan(substrate *numeric.HybridSubstrate, seedFingerprint numeric.PhaseDial, aphorisms []string) []ScanResult {
	// Dummy array to query all candidates
	candidates := make([]int, len(substrate.Entries))
	for i := range candidates {
		candidates[i] = i
	}

	var results []ScanResult
	const steps = 72
	for s := 0; s <= steps; s++ {
		alphaDegrees := float64(s) * 5.0
		alphaRadians := alphaDegrees * (math.Pi / 180.0)
		rotationFactor := cmplx.Rect(1.0, alphaRadians)

		rotatedDial := make(numeric.PhaseDial, numeric.NBasis)
		for k, val := range seedFingerprint {
			rotatedDial[k] = val * rotationFactor
		}

		ranked := substrate.PhaseDialRank(candidates, rotatedDial)
		bestReadout := substrate.Entries[ranked[0].Idx].Readout

		// Compute metrics
		margin := ranked[0].Score - ranked[1].Score
		
		// Approximate localized entropy over Top 5 explicitly to monitor density
		var pSum, entropy float64
		for _, rank := range ranked[:5] {
			normScore := math.Max(0, rank.Score) // Relu to zero
			pSum += normScore
		}
		if pSum > 0 {
			for _, rank := range ranked[:5] {
				p := math.Max(0, rank.Score) / pSum
				if p > 0 {
					entropy -= p * math.Log2(p)
				}
			}
		}

		origIdxMap := make(map[string]int)
		for i, text := range aphorisms {
			origIdxMap[text] = i
		}

		scores := make([]float64, len(aphorisms))
		var matchOrigIdx int
		for i, rank := range ranked {
			entry := substrate.Entries[rank.Idx]
			// The readout is "shuffledIdx: text", split to get original text
			parts := strings.SplitN(string(entry.Readout), ": ", 2)
			if len(parts) == 2 {
				origIdx := origIdxMap[parts[1]]
				scores[origIdx] = rank.Score
				if i == 0 {
					matchOrigIdx = origIdx
				}
			}
		}

		fmt.Printf("Phase %3.0f° -> Margin: %.3f | Entropy (Top5): %.3f | Best Match: %s\n", alphaDegrees, margin, entropy, string(bestReadout))
		results = append(results, ScanResult{
			Phase:        alphaDegrees,
			Margin:       margin,
			Entropy:      entropy,
			Match:        string(bestReadout),
			MatchOrigIdx: matchOrigIdx,
			Scores:       scores,
		})
	}
	
	return results
}