package primitive

import (
	"sort"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
)

/*
Signal describes a detected structure in a Value's token region.
RunLen is the length of the longest contiguous run found.
Kind distinguishes zero-runs (absence) from one-runs (density).
*/
type Signal struct {
	WordIndex int
	RunLen    int
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
ScanSignalRegion scans the emitted Signals region for one-runs and zero-runs.
Returns signals sorted by run length (longest first).
*/
func ScanSignalRegion(value *Value) []Signal {
	if value == nil {
		return nil
	}

	signalStart := core.Cfg.Value.Region.Signals.Start
	tokenBits := int(core.Cfg.Value.Region.Signals.Bits)
	signalWords := (tokenBits + 63) / 64

	if signalStart < 0 || signalStart+signalWords > len(value) {
		errnie.Warn(
			"primitive.ScanSignalRegion: signals region out of bounds",
			"signals_start", signalStart,
			"signals_bits", tokenBits,
			"signal_words", signalWords,
			"value_len", len(value),
		)

		return nil
	}

	var signals []Signal

	for i := range signalWords {
		word := value[signalStart+i]

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

	return signals
}

/*
ScanSignals executes the surface program once, then scans the emitted
Signals region for one-runs and zero-runs.
*/
func ScanSignals(v *Value, runner func(*Value) error) ([]Signal, error) {
	if err := runner(v); err != nil {
		return nil, err
	}

	return ScanSignalRegion(v), nil
}
