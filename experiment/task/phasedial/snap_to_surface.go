package phasedial

import (
	"fmt"
	"math"
	"math/cmplx"
	"strings"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/numeric"
)

// testSnapToSurface implements Experiment 8: Rotational Surface Projection.
// Instead of using the raw midpoint F_AB = Normalize(F_A + F_B) as the hop-2
// anchor, we sweep α over rotate(F_AB, α) and pick α* that maximises the best
// corpus match score. This "snaps" the interior midpoint back onto the nearest
// manifold surface ridge. We then run hop-2 from the snapped anchor and compare
// gain against the raw-midpoint and single-anchor baselines.
func (experiment *Experiment) testSnapToSurface(aphorisms []string) SnapToSurfaceResult {
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

	// Helper: compute gain = min(sim(C_fp, A), sim(C_fp, B)) for best C found
	// by sweeping α₂ over an anchor, excluding A and B text from results.
	sweepBestGain := func(anchor, fpA, fpB numeric.PhaseDial, excludeA, excludeB string) float64 {
		var best float64 = -1.0
		for s := 0; s < 360; s++ {
			alpha2 := float64(s) * (math.Pi / 180.0)
			factor := cmplx.Rect(1.0, alpha2)
			rotated := make(numeric.PhaseDial, numeric.NBasis)
			for k, val := range anchor {
				rotated[k] = val * factor
			}
			ranked := substrate.PhaseDialRank(candidates, rotated)
			var topIdx int = ranked[0].Idx
			for _, rank := range ranked {
				ct := cleanReadout(string(substrate.Entries[rank.Idx].Readout))
				if ct != excludeA && ct != excludeB {
					topIdx = rank.Idx
					break
				}
			}
			fpC := substrate.Entries[topIdx].Fingerprint
			g := math.Min(sim(fpC, fpA), sim(fpC, fpB))
			if g > best {
				best = g
			}
		}
		return best
	}

	var result SnapToSurfaceResult
	alpha1List := []float64{15.0, 30.0, 45.0, 60.0, 75.0}

	for _, alpha1Deg := range alpha1List {
		console.Info(fmt.Sprintf("\n--- Snap-to-Surface: α1 = %.0f° ---", alpha1Deg))

		// --- Hop 1: resolve B ---
		alpha1Rad := alpha1Deg * (math.Pi / 180.0)
		factorA1 := cmplx.Rect(1.0, alpha1Rad)
		rotatedA := make(numeric.PhaseDial, numeric.NBasis)
		for k, val := range fingerprintA {
			rotatedA[k] = val * factorA1
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

		simAB_A := sim(fingerprintAB, fingerprintA)
		simAB_B := sim(fingerprintAB, fingerprintB)
		simA_B := sim(fingerprintA, fingerprintB)
		console.Info(fmt.Sprintf("  cos(F_AB, F_A) = %.4f  cos(F_AB, F_B) = %.4f  cos(F_A, F_B) = %.4f", simAB_A, simAB_B, simA_B))

		// --- Step 1: Snap midpoint to surface ---
		// Sweep α over rotate(F_AB, α) and find α* that maximises the best
		// corpus match score (the top-1 PhaseDialRank score).
		var bestSnapAlpha float64
		var bestSnapScore float64 = -math.MaxFloat64
		for s := 0; s < 360; s++ {
			alpha := float64(s) * (math.Pi / 180.0)
			factor := cmplx.Rect(1.0, alpha)
			rotated := make(numeric.PhaseDial, numeric.NBasis)
			for k, val := range fingerprintAB {
				rotated[k] = val * factor
			}
			ranked := substrate.PhaseDialRank(candidates, rotated)
			if ranked[0].Score > bestSnapScore {
				bestSnapScore = ranked[0].Score
				bestSnapAlpha = float64(s)
			}
		}
		console.Info(fmt.Sprintf("  Snap α* = %.0f°  (corpus peak score: %.4f)", bestSnapAlpha, bestSnapScore))

		// Build the snapped anchor
		snapFactor := cmplx.Rect(1.0, bestSnapAlpha*(math.Pi/180.0))
		snappedAB := make(numeric.PhaseDial, numeric.NBasis)
		for k, val := range fingerprintAB {
			snappedAB[k] = val * snapFactor
		}

		// --- Step 2: Hop-2 from snapped anchor ---
		snapGain := sweepBestGain(snappedAB, fingerprintA, fingerprintB, seedQuery, textB)

		// Also get the specific best C for reporting
		var snapBestC string
		var snapSimCA, snapSimCB, snapSimCAB float64
		{
			var bestG float64 = -1.0
			for s := 0; s < 360; s++ {
				a2 := float64(s) * (math.Pi / 180.0)
				f2 := cmplx.Rect(1.0, a2)
				rot := make(numeric.PhaseDial, numeric.NBasis)
				for k, val := range snappedAB {
					rot[k] = val * f2
				}
				ranked := substrate.PhaseDialRank(candidates, rot)
				var topIdx int = ranked[0].Idx
				for _, rank := range ranked {
					ct := cleanReadout(string(substrate.Entries[rank.Idx].Readout))
					if ct != seedQuery && ct != textB {
						topIdx = rank.Idx
						break
					}
				}
				fpC := substrate.Entries[topIdx].Fingerprint
				ca := sim(fpC, fingerprintA)
				cb := sim(fpC, fingerprintB)
				g := math.Min(ca, cb)
				if g > bestG {
					bestG = g
					snapBestC = cleanReadout(string(substrate.Entries[topIdx].Readout))
					snapSimCA = ca
					snapSimCB = cb
					snapSimCAB = sim(fpC, snappedAB)
				}
			}
		}

		// --- Baselines ---
		midptGain := sweepBestGain(fingerprintAB, fingerprintA, fingerprintB, seedQuery, textB)
		base1Gain := sweepBestGain(fingerprintA, fingerprintA, fingerprintB, seedQuery, textB)
		base2Gain := sweepBestGain(fingerprintB, fingerprintA, fingerprintB, seedQuery, textB)

		snapBalanced := 0.5 * (snapSimCA + snapSimCB)
		snapSep := snapSimCAB - math.Max(snapSimCA, snapSimCB)

		console.Info(fmt.Sprintf("  Snap gain:    %.4f  (C: %s)", snapGain, snapBestC))
		console.Info(fmt.Sprintf("  Midpt gain:   %.4f", midptGain))
		console.Info(fmt.Sprintf("  Baseline 1:   %.4f  (A only)", base1Gain))
		console.Info(fmt.Sprintf("  Baseline 2:   %.4f  (B only)", base2Gain))

		if snapGain > math.Max(base1Gain, base2Gain) {
			console.Info(fmt.Sprintf("  ✓ SUPER-ADDITIVE: snap (%.4f) > max baseline (%.4f)", snapGain, math.Max(base1Gain, base2Gain)))
		} else if snapGain > midptGain {
			console.Info(fmt.Sprintf("  ~ Snap improves over raw midpoint (%.4f > %.4f) but does not exceed single-anchor.", snapGain, midptGain))
		} else {
			console.Warn(fmt.Sprintf("  ✗ No improvement: snap (%.4f) ≤ midpoint (%.4f)", snapGain, midptGain))
		}

		result.Slices = append(result.Slices, SnapAlphaResult{
			Alpha1:       alpha1Deg,
			SnapAlpha:    bestSnapAlpha,
			SnapScore:    bestSnapScore,
			SnapGain:     snapGain,
			MidptGain:    midptGain,
			Base1Gain:    base1Gain,
			Base2Gain:    base2Gain,
			SnapC:        snapBestC,
			SnapSimCA:    snapSimCA,
			SnapSimCB:    snapSimCB,
			SnapBalanced: snapBalanced,
			SnapSep:      snapSep,
			SimAB_A:      simAB_A,
			SimAB_B:      simAB_B,
			SimA_B:       simA_B,
		})
	}

	return result
}