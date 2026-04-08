package algo

import (
	"math"

	"github.com/theapemachine/six/pkg/core/numeric/gf"
)

/*
ParseGlobalPhaseIndex interprets a numeric GlobalPhase signal value.

Non-finite inputs (NaN, ±Inf) yield active false. Beam search treats that as
no captured phase; online training defaults the lane to 0 per tuning policy.

When active is true and lane is negative, the rounded value was negative; beam
clears phase and online sets phaseIndex to -1.

When active is true and lane is non-negative, lane is in [0, gf.PhaseWidth).
Large magnitudes are clamped before rounding so conversion to int cannot overflow.
*/
func ParseGlobalPhaseIndex(value float64) (lane int, active bool) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}

	const absClamp = 1e12

	if value > absClamp {
		value = absClamp
	}

	if value < -absClamp {
		value = -absClamp
	}

	rounded := math.Round(value)

	if rounded < 0 {
		return -1, true
	}

	mod := math.Mod(rounded, float64(gf.PhaseWidth))

	if mod < 0 {
		mod += float64(gf.PhaseWidth)
	}

	return int(mod), true
}
