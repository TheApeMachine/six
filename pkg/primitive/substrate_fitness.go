package primitive

import (
	"math"

	"github.com/theapemachine/six/pkg/core"
)

/*
SubstrateExploitScore maps pairwise token-region structure to [0, 1] using only
substrate math: ScanSignals between parent and workspace, then the longest
local cancel or merge run normalized by the configured token bit width.

High values mean a large coherent agreement span (XOR zero-run / AND one-run)
between the canonical frame and the post-execution workspace — a substrate-native
proxy for “this interaction produced sharp structure” without holdouts or text.

nil inputs yield 0.
*/
func SubstrateExploitScore(parent, workspace *Value) float64 {
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
