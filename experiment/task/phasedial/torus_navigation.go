package phasedial

import (
	"fmt"
	"math"
	"math/cmplx"
	"strings"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/numeric"
)

// testTorusNavigation implements Experiment 9: U(1)×U(1) Torus Navigation.
//
// The previous experiments established that the embedding manifold admits a
// single U(1) global phase symmetry and that this 1D rotational degree of
// freedom is insufficient for super-additive composition. The snap-to-surface
// experiment proved that rotating the interior midpoint F_AB always returns to
// the same ridge that B already dominates.
//
// This experiment tests whether the manifold is fundamentally 1D or richer by
// splitting the 512 complex dimensions into two disjoint subspaces:
//
//	F → (F₁, F₂)  where F₁ = F[0:256], F₂ = F[256:512]
//
// and applying independent phase rotations:
//
//	rotate(F, α₁, α₂) = (e^{iα₁}·F₁, e^{iα₂}·F₂)
//
// This gives a 2D torus T² = U(1)×U(1) of surface-preserving navigation.
// We sweep a grid of (α₁, α₂) and measure whether any point on the torus
// achieves gain > max(Baseline_A, Baseline_B), which would demonstrate
// super-additive composition — something impossible with 1D rotation.
func (experiment *Experiment) testTorusNavigation(aphorisms []string) TorusResult {
	substrate := numeric.NewHybridSubstrate()
	var universalFilter numeric.Chord

	for i, text := range aphorisms {
		fingerprint := numeric.EncodeText(text)
		readout := []byte(fmt.Sprintf("%d: %s", i, text))
		substrate.Add(universalFilter, fingerprint, readout)
	}

	seedQuery := "Democracy requires individual sacrifice."
	fingerprintA := numeric.EncodeText(seedQuery)

	candidates := make([]int, len(substrate.Entries))
	for i := range candidates {
		candidates[i] = i
	}

	cleanReadout := func(text string) string {
		parts := strings.SplitN(text, ": ", 2)
		if len(parts) == 2 {
			return parts[1]
		}
		return text
	}

	sim := func(x, y numeric.PhaseDial) float64 {
		var dot complex128
		var nx, ny float64
		for i := range x {
			dot += cmplx.Conj(x[i]) * y[i]
			nx += real(x[i])*real(x[i]) + imag(x[i])*imag(x[i])
			ny += real(y[i])*real(y[i]) + imag(y[i])*imag(y[i])
		}
		if nx == 0 || ny == 0 {
			return 0
		}
		return real(dot) / (math.Sqrt(nx) * math.Sqrt(ny))
	}

	// Torus rotation: independent phase on each half of the embedding.
	splitPoint := numeric.NBasis / 2 // 256

	torusRotate := func(fp numeric.PhaseDial, alpha1, alpha2 float64) numeric.PhaseDial {
		factor1 := cmplx.Rect(1.0, alpha1)
		factor2 := cmplx.Rect(1.0, alpha2)
		rotated := make(numeric.PhaseDial, numeric.NBasis)
		for k := 0; k < splitPoint; k++ {
			rotated[k] = fp[k] * factor1
		}
		for k := splitPoint; k < numeric.NBasis; k++ {
			rotated[k] = fp[k] * factor2
		}
		return rotated
	}

	// Torus scoring: rank candidates using the torus-rotated query.
	// This is the same cosine scoring as PhaseDialRank but applied to the
	// torus-rotated fingerprint directly.
	torusRank := func(query numeric.PhaseDial) []numeric.CandidateScore {
		return substrate.PhaseDialRank(candidates, query)
	}

	var result TorusResult
	result.SplitPoint = splitPoint

	// Grid resolution: step size in degrees for the torus sweep
	const stepDeg = 5.0
	gridSize := int(360.0 / stepDeg)

	alpha1List := []float64{15.0, 30.0, 45.0, 60.0, 75.0}

	for _, hopAlpha1Deg := range alpha1List {
		console.Info(fmt.Sprintf("\n--- Torus Navigation: hop α₁ = %.0f° ---", hopAlpha1Deg))

		// --- Hop 1: resolve B using standard 1D rotation ---
		hopAlpha1Rad := hopAlpha1Deg * (math.Pi / 180.0)
		factor := cmplx.Rect(1.0, hopAlpha1Rad)
		rotatedA := make(numeric.PhaseDial, numeric.NBasis)
		for k, val := range fingerprintA {
			rotatedA[k] = val * factor
		}
		rankedA1 := substrate.PhaseDialRank(candidates, rotatedA)
		bestMatchB := rankedA1[0]
		for _, rank := range rankedA1 {
			if cleanReadout(string(substrate.Entries[rank.Idx].Readout)) != seedQuery {
				bestMatchB = rank
				break
			}
		}
		fingerprintB := substrate.Entries[bestMatchB.Idx].Fingerprint
		textB := cleanReadout(string(substrate.Entries[bestMatchB.Idx].Readout))
		console.Info(fmt.Sprintf("  B = %s", textB))

		// --- Compose midpoint F_AB = Normalize(Â + B̂) ---
		var magSqA, magSqB float64
		for k := 0; k < numeric.NBasis; k++ {
			magSqA += real(fingerprintA[k])*real(fingerprintA[k]) + imag(fingerprintA[k])*imag(fingerprintA[k])
			magSqB += real(fingerprintB[k])*real(fingerprintB[k]) + imag(fingerprintB[k])*imag(fingerprintB[k])
		}
		nrmA := math.Sqrt(magSqA)
		nrmB := math.Sqrt(magSqB)

		fingerprintAB := make(numeric.PhaseDial, numeric.NBasis)
		var nrm float64
		for k := 0; k < numeric.NBasis; k++ {
			valA := fingerprintA[k]
			if nrmA > 0 {
				valA /= complex(nrmA, 0)
			}
			valB := fingerprintB[k]
			if nrmB > 0 {
				valB /= complex(nrmB, 0)
			}
			fingerprintAB[k] = valA + valB
			r, im := real(fingerprintAB[k]), imag(fingerprintAB[k])
			nrm += r*r + im*im
		}
		if nrm > 0 {
			nrm = math.Sqrt(nrm)
			for k := 0; k < numeric.NBasis; k++ {
				fingerprintAB[k] = complex(real(fingerprintAB[k])/nrm, imag(fingerprintAB[k])/nrm)
			}
		}

		// --- 1D baselines (same as previous experiments) ---
		base1Best := -1.0
		base2Best := -1.0
		for s := 0; s < 360; s++ {
			alpha := float64(s) * (math.Pi / 180.0)
			f := cmplx.Rect(1.0, alpha)

			// Baseline 1: from A
			rA := make(numeric.PhaseDial, numeric.NBasis)
			for k, v := range fingerprintA {
				rA[k] = v * f
			}
			ranked := torusRank(rA)
			var topIdx int = ranked[0].Idx
			for _, rank := range ranked {
				ct := cleanReadout(string(substrate.Entries[rank.Idx].Readout))
				if ct != seedQuery && ct != textB {
					topIdx = rank.Idx
					break
				}
			}
			fpC := substrate.Entries[topIdx].Fingerprint
			g := math.Min(sim(fpC, fingerprintA), sim(fpC, fingerprintB))
			if g > base1Best {
				base1Best = g
			}

			// Baseline 2: from B
			rB := make(numeric.PhaseDial, numeric.NBasis)
			for k, v := range fingerprintB {
				rB[k] = v * f
			}
			ranked = torusRank(rB)
			topIdx = ranked[0].Idx
			for _, rank := range ranked {
				ct := cleanReadout(string(substrate.Entries[rank.Idx].Readout))
				if ct != seedQuery && ct != textB {
					topIdx = rank.Idx
					break
				}
			}
			fpC = substrate.Entries[topIdx].Fingerprint
			g = math.Min(sim(fpC, fingerprintA), sim(fpC, fingerprintB))
			if g > base2Best {
				base2Best = g
			}
		}

		singleAxisCeiling := math.Max(base1Best, base2Best)
		console.Info(fmt.Sprintf("  1D Baselines: A-only=%.4f  B-only=%.4f  Ceiling=%.4f", base1Best, base2Best, singleAxisCeiling))

		// --- Torus sweep: sweep (α₁_torus, α₂_torus) grid over F_AB ---
		var bestTorusGain float64 = -1.0
		var bestTorusA1, bestTorusA2 float64
		var bestTorusC string
		var bestTorusSimCA, bestTorusSimCB, bestTorusSimCAB float64

		sliceTraces := make([]TorusGridPoint, 0, gridSize*gridSize)

		for i := 0; i < gridSize; i++ {
			a1Deg := float64(i) * stepDeg
			a1Rad := a1Deg * (math.Pi / 180.0)

			for j := 0; j < gridSize; j++ {
				a2Deg := float64(j) * stepDeg
				a2Rad := a2Deg * (math.Pi / 180.0)

				rotatedAB := torusRotate(fingerprintAB, a1Rad, a2Rad)
				ranked := torusRank(rotatedAB)

				var topIdx int = ranked[0].Idx
				for _, rank := range ranked {
					ct := cleanReadout(string(substrate.Entries[rank.Idx].Readout))
					if ct != seedQuery && ct != textB {
						topIdx = rank.Idx
						break
					}
				}
				fpC := substrate.Entries[topIdx].Fingerprint
				textC := cleanReadout(string(substrate.Entries[topIdx].Readout))

				simCA := sim(fpC, fingerprintA)
				simCB := sim(fpC, fingerprintB)
				simCAB := sim(fpC, fingerprintAB)
				gain := math.Min(simCA, simCB)

				point := TorusGridPoint{
					Alpha1: a1Deg,
					Alpha2: a2Deg,
					Gain:   gain,
					SimCA:  simCA,
					SimCB:  simCB,
					SimCAB: simCAB,
				}
				sliceTraces = append(sliceTraces, point)

				if gain > bestTorusGain {
					bestTorusGain = gain
					bestTorusA1 = a1Deg
					bestTorusA2 = a2Deg
					bestTorusC = textC
					bestTorusSimCA = simCA
					bestTorusSimCB = simCB
					bestTorusSimCAB = simCAB
				}
			}
		}

		superAdditive := bestTorusGain > singleAxisCeiling
		delta := bestTorusGain - singleAxisCeiling

		console.Info(fmt.Sprintf("  Torus best:  α₁=%.0f° α₂=%.0f°  gain=%.4f  (C: %s)",
			bestTorusA1, bestTorusA2, bestTorusGain, bestTorusC))
		console.Info(fmt.Sprintf("  simCA=%.4f  simCB=%.4f  simCAB=%.4f", bestTorusSimCA, bestTorusSimCB, bestTorusSimCAB))

		if superAdditive {
			console.Info(fmt.Sprintf("  ✓ SUPER-ADDITIVE: torus gain (%.4f) > 1D ceiling (%.4f)  Δ=+%.4f",
				bestTorusGain, singleAxisCeiling, delta))
		} else {
			console.Warn(fmt.Sprintf("  ✗ No super-additive gain: torus (%.4f) ≤ 1D ceiling (%.4f)  Δ=%.4f",
				bestTorusGain, singleAxisCeiling, delta))
		}

		slice := TorusAlphaSlice{
			HopAlpha1:      hopAlpha1Deg,
			TextB:          textB,
			Base1Gain:      base1Best,
			Base2Gain:      base2Best,
			SingleCeiling:  singleAxisCeiling,
			BestTorusGain:  bestTorusGain,
			BestTorusA1:    bestTorusA1,
			BestTorusA2:    bestTorusA2,
			BestTorusC:     bestTorusC,
			BestSimCA:      bestTorusSimCA,
			BestSimCB:      bestTorusSimCB,
			BestSimCAB:     bestTorusSimCAB,
			SuperAdditive:  superAdditive,
			Delta:          delta,
			Grid:           sliceTraces,
		}
		result.Slices = append(result.Slices, slice)
	}

	// --- Summary ---
	anySuperAdditive := false
	for _, s := range result.Slices {
		if s.SuperAdditive {
			anySuperAdditive = true
			break
		}
	}

	if anySuperAdditive {
		console.Info("\n==> TORUS RESULT: The manifold has >1D exploitable symmetry. Multi-axis rotation achieves super-additive composition.")
	} else {
		console.Warn("\n==> TORUS RESULT: The manifold remains ridge-separated even under 2D torus navigation. Composition must be done outside phase space.")
	}

	result.AnySuperAdditive = anySuperAdditive
	return result
}
