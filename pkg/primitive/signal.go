package primitive

import (
	"sort"

	"github.com/theapemachine/six/pkg/core"
)

/*
Signal describes a detected structure in a Value's token region.
RunLen is the length of the longest contiguous run found.
Position is the bit offset where the run starts.
Kind distinguishes zero-runs (absence) from one-runs (density).
*/
type Signal struct {
	WordIndex int
	RunLen    int
	Position  int
	Kind      SignalKind
}

type SignalKind uint8

const (
	SignalZeroRun SignalKind = iota
	SignalOneRun
)

/*
longestOneRun returns the length of the longest contiguous run of 1-bits
in a uint64.
*/
func longestOneRun(x uint64) int {
	if x == 0 {
		return 0
	}

	best := 0
	for x != 0 {
		x &= x << 1
		best++
	}

	return best
}

/*
ScanSignals detects one-runs and zero-runs across the token region by
inspecting each token word directly. The runner is called once to execute
the surface program (populating Signals), then the token words themselves
are scanned for contiguous bit runs.

Returns signals sorted by run length (longest first).
*/
func ScanSignals(v *Value, runner func(*Value) error) ([]Signal, error) {
	if err := runner(v); err != nil {
		return nil, err
	}

	tokenStart := core.Cfg.Value.Region.Tokens.Start
	tokenWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)

	var signals []Signal

	for i := range tokenWords {
		word := v[tokenStart+i]

		// Detect one-runs.
		if oneRun := longestOneRun(word); oneRun >= 4 {
			signals = append(signals, Signal{
				WordIndex: i,
				RunLen:    oneRun,
				Kind:      SignalOneRun,
			})
		}

		// Detect zero-runs (invert and find one-runs).
		if zeroRun := longestOneRun(^word); zeroRun >= 4 {
			signals = append(signals, Signal{
				WordIndex: i,
				RunLen:    zeroRun,
				Kind:      SignalZeroRun,
			})
		}
	}

	sort.Slice(signals, func(i, j int) bool {
		return signals[i].RunLen > signals[j].RunLen
	})

	return signals, nil
}
