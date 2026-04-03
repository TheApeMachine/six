package primitive

import (
	"math"

	"github.com/theapemachine/six/pkg/core"
)

/*
SubstrateExploitScore maps pairwise token-region structure to [0, 1] using only
substrate math. Longest-run signal strength and chunked holistic similarity are
both computed; the larger score wins (same combine rule the emitter uses when
run-path emission is empty).

nil inputs yield 0.
*/
func SubstrateExploitScore(parent, workspace *Value) float64 {

	if parent == nil || workspace == nil {
		return 0
	}

	runScore := substrateExploitScoreRuns(parent, workspace)
	hol := HolisticSubstrateScore(parent, workspace)

	if hol > runScore {
		return hol
	}

	return runScore
}

func substrateExploitScoreRuns(parent, workspace *Value) float64 {

	if parent == nil || workspace == nil {
		return 0
	}

	tokenBits := core.Cfg.Value.Region.Tokens.Bits
	if tokenBits == 0 {
		return 0
	}

	tokenWords := int((tokenBits + 63) / 64)
	baseIdx := core.Cfg.Value.Region.Tokens.Start
	if tokenWords <= 0 {
		return 0
	}

	signals := ScanSignals(parent, workspace, tokenWords, baseIdx)
	if len(signals) == 0 {
		return 0
	}

	local, _ := SplitSignals(signals)
	if len(local) == 0 {
		return 0
	}

	longest := 0
	for _, signal := range local {
		if signal.Length > longest {
			longest = signal.Length
		}
	}

	if longest <= 0 {
		return 0
	}

	return math.Min(1.0, float64(longest)/float64(tokenBits))
}
