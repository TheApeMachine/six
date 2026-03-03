package phasedial

import (
	"fmt"
	"math"
	"math/cmplx"
	"strings"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/numeric"
)

// testAdaptiveSplit implements Experiment 13: Adaptive Split Selection.
//
// Part A: Boundary scoring from the A–B residual
// For each candidate boundary b (step 8), computes:
//   - S(b): differential energy balance of the residual across the split
//   - K_left, K_right: phase concentration (steerability) per side
//   - Combined score: min(K_left, K_right) weighted by balance
//
// Picks the best boundary and runs a torus sweep to test if the
// data-driven split matches or beats the hand-picked 256/256.
//
// Part B: Gap experiment
// Tests block0=[0,256), gap=[256,272), block1=[272,512) to determine
// whether the boundary zone itself carries critical discriminative dims.
func (experiment *Experiment) testAdaptiveSplit(aphorisms []string) AdaptiveSplitResult {
	substrate := numeric.NewHybridSubstrate()
	var universalFilter numeric.Chord

	for i, text := range aphorisms {
		fingerprint := numeric.EncodeText(text)
		readout := []byte(fmt.Sprintf("%d: %s", i, text))
		substrate.Add(universalFilter, fingerprint, readout)
	}

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

	composeMidpoint := func(fpA, fpB numeric.PhaseDial) numeric.PhaseDial {
		var magSqA, magSqB float64
		for k := 0; k < numeric.NBasis; k++ {
			magSqA += real(fpA[k])*real(fpA[k]) + imag(fpA[k])*imag(fpA[k])
			magSqB += real(fpB[k])*real(fpB[k]) + imag(fpB[k])*imag(fpB[k])
		}
		nA := math.Sqrt(magSqA)
		nB := math.Sqrt(magSqB)
		out := make(numeric.PhaseDial, numeric.NBasis)
		var n float64
		for k := 0; k < numeric.NBasis; k++ {
			vA := fpA[k]
			if nA > 0 {
				vA /= complex(nA, 0)
			}
			vB := fpB[k]
			if nB > 0 {
				vB /= complex(nB, 0)
			}
			out[k] = vA + vB
			r, im := real(out[k]), imag(out[k])
			n += r*r + im*im
		}
		if n > 0 {
			n = math.Sqrt(n)
			for k := 0; k < numeric.NBasis; k++ {
				out[k] = complex(real(out[k])/n, imag(out[k])/n)
			}
		}
		return out
	}

	// Torus sweep helper: given a rotation function, sweep (α₁,α₂) grid
	type torusSweepResult struct {
		bestGain  float64
		bestA1    float64
		bestA2    float64
		bestC     string
	}

	torusSweep := func(fpAB numeric.PhaseDial, rotateFn func(numeric.PhaseDial, float64, float64) numeric.PhaseDial, fpA, fpB numeric.PhaseDial, excludeA, excludeB string, stepDeg float64) torusSweepResult {
		gridSize := int(360.0 / stepDeg)
		var best torusSweepResult
		best.bestGain = -1.0

		for i := 0; i < gridSize; i++ {
			a1 := float64(i) * stepDeg * (math.Pi / 180.0)
			for j := 0; j < gridSize; j++ {
				a2 := float64(j) * stepDeg * (math.Pi / 180.0)
				rotated := rotateFn(fpAB, a1, a2)

				ranked := substrate.PhaseDialRank(candidates, rotated)
				topIdx := ranked[0].Idx
				for _, rank := range ranked {
					ct := cleanReadout(string(substrate.Entries[rank.Idx].Readout))
					if ct != excludeA && ct != excludeB {
						topIdx = rank.Idx
						break
					}
				}
				fpC := substrate.Entries[topIdx].Fingerprint
				textC := cleanReadout(string(substrate.Entries[topIdx].Readout))

				gain := math.Min(sim(fpC, fpA), sim(fpC, fpB))
				if gain > best.bestGain {
					best.bestGain = gain
					best.bestA1 = float64(i) * stepDeg
					best.bestA2 = float64(j) * stepDeg
					best.bestC = textC
				}
			}
		}
		return best
	}

	// 1D ceiling helper
	compute1DCeiling := func(fpA, fpB numeric.PhaseDial, excludeA, excludeB string) float64 {
		ceiling := -1.0
		for s := 0; s < 360; s++ {
			alpha := float64(s) * (math.Pi / 180.0)
			f := cmplx.Rect(1.0, alpha)

			for _, anchor := range []numeric.PhaseDial{fpA, fpB} {
				rotated := make(numeric.PhaseDial, numeric.NBasis)
				for k, v := range anchor {
					rotated[k] = v * f
				}
				ranked := substrate.PhaseDialRank(candidates, rotated)
				topIdx := ranked[0].Idx
				for _, rank := range ranked {
					ct := cleanReadout(string(substrate.Entries[rank.Idx].Readout))
					if ct != excludeA && ct != excludeB {
						topIdx = rank.Idx
						break
					}
				}
				g := math.Min(sim(substrate.Entries[topIdx].Fingerprint, fpA),
					sim(substrate.Entries[topIdx].Fingerprint, fpB))
				if g > ceiling {
					ceiling = g
				}
			}
		}
		return ceiling
	}

	D := numeric.NBasis // 512

	// Use "Democracy" seed
	seedQuery := "Democracy requires individual sacrifice."
	fpA := numeric.EncodeText(seedQuery)

	// Hop 1: B at α₁=45°
	hopFactor := cmplx.Rect(1.0, 45.0*(math.Pi/180.0))
	rotatedA := make(numeric.PhaseDial, D)
	for k, val := range fpA {
		rotatedA[k] = val * hopFactor
	}
	ranked := substrate.PhaseDialRank(candidates, rotatedA)
	bestMatchB := ranked[0]
	for _, rank := range ranked {
		if cleanReadout(string(substrate.Entries[rank.Idx].Readout)) != seedQuery {
			bestMatchB = rank
			break
		}
	}
	fpB := substrate.Entries[bestMatchB.Idx].Fingerprint
	textB := cleanReadout(string(substrate.Entries[bestMatchB.Idx].Readout))
	fpAB := composeMidpoint(fpA, fpB)
	ceiling := compute1DCeiling(fpA, fpB, seedQuery, textB)

	console.Info(fmt.Sprintf("  B = %s", textB))
	console.Info(fmt.Sprintf("  1D Ceiling: %.4f", ceiling))

	// ===== PART A: Boundary scoring from the A–B residual =====
	console.Info("\n  ── Part A: Residual-Based Boundary Scoring ──")

	// Compute residual R = Normalize(F_A - F_B)
	residual := make(numeric.PhaseDial, D)
	var rNorm float64
	for k := 0; k < D; k++ {
		residual[k] = fpA[k] - fpB[k]
		rNorm += real(residual[k])*real(residual[k]) + imag(residual[k])*imag(residual[k])
	}
	rNorm = math.Sqrt(rNorm)
	if rNorm > 0 {
		for k := 0; k < D; k++ {
			residual[k] /= complex(rNorm, 0)
		}
	}

	// For each boundary b, compute scores
	type boundaryScore struct {
		b        int
		sBalance float64 // |left_mass - right_mass| (lower = more balanced)
		kLeft    float64 // phase concentration left
		kRight   float64 // phase concentration right
		combined float64 // min(kLeft, kRight) * (1 - sBalance)
	}

	var scores []boundaryScore
	var bestScore boundaryScore

	for b := 16; b <= D-16; b += 8 {
		// S(b): differential energy balance
		var leftMass, rightMass float64
		for k := 0; k < b; k++ {
			leftMass += cmplx.Abs(residual[k])
		}
		for k := b; k < D; k++ {
			rightMass += cmplx.Abs(residual[k])
		}
		totalMass := leftMass + rightMass
		sBalance := 0.0
		if totalMass > 0 {
			sBalance = math.Abs(leftMass-rightMass) / totalMass
		}

		// K: phase concentration per side
		var sumLeft, sumRight complex128
		var nLeft, nRight int
		for k := 0; k < b; k++ {
			mag := cmplx.Abs(residual[k])
			if mag > 0 {
				sumLeft += residual[k] / complex(mag, 0)
				nLeft++
			}
		}
		for k := b; k < D; k++ {
			mag := cmplx.Abs(residual[k])
			if mag > 0 {
				sumRight += residual[k] / complex(mag, 0)
				nRight++
			}
		}

		kLeft := 0.0
		if nLeft > 0 {
			kLeft = cmplx.Abs(sumLeft) / float64(nLeft)
		}
		kRight := 0.0
		if nRight > 0 {
			kRight = cmplx.Abs(sumRight) / float64(nRight)
		}

		combined := math.Min(kLeft, kRight) * (1.0 - sBalance)

		s := boundaryScore{b, sBalance, kLeft, kRight, combined}
		scores = append(scores, s)

		if combined > bestScore.combined {
			bestScore = s
		}
	}

	// Report top 5 boundaries by combined score
	// Sort by combined score descending
	for i := 0; i < len(scores); i++ {
		for j := i + 1; j < len(scores); j++ {
			if scores[j].combined > scores[i].combined {
				scores[i], scores[j] = scores[j], scores[i]
			}
		}
	}

	console.Info("  Top-5 boundaries by combined score:")
	topN := 5
	if len(scores) < topN {
		topN = len(scores)
	}
	for i := 0; i < topN; i++ {
		s := scores[i]
		console.Info(fmt.Sprintf("    b=%3d  S=%.4f  K_L=%.4f  K_R=%.4f  combined=%.4f",
			s.b, s.sBalance, s.kLeft, s.kRight, s.combined))
	}

	// Run torus sweep at best boundary
	bestB := bestScore.b
	console.Info(fmt.Sprintf("\n  Selected boundary: b=%d", bestB))

	hardRotate := func(fp numeric.PhaseDial, a1, a2 float64) numeric.PhaseDial {
		f1 := cmplx.Rect(1.0, a1)
		f2 := cmplx.Rect(1.0, a2)
		rotated := make(numeric.PhaseDial, D)
		for k := 0; k < D; k++ {
			if k < bestB {
				rotated[k] = fp[k] * f1
			} else {
				rotated[k] = fp[k] * f2
			}
		}
		return rotated
	}

	adaptiveResult := torusSweep(fpAB, hardRotate, fpA, fpB, seedQuery, textB, 5.0)
	adaptiveDelta := adaptiveResult.bestGain - ceiling
	adaptiveSA := adaptiveResult.bestGain > ceiling

	console.Info(fmt.Sprintf("  Best: %.4f at (%.0f°, %.0f°)  C: %s", adaptiveResult.bestGain, adaptiveResult.bestA1, adaptiveResult.bestA2, adaptiveResult.bestC))
	if adaptiveSA {
		console.Info(fmt.Sprintf("  └─ ✓ SUPER-ADDITIVE  Δ=+%.4f", adaptiveDelta))
	} else {
		console.Warn(fmt.Sprintf("  └─ ✗ No super-additive  Δ=%.4f", adaptiveDelta))
	}

	// Also run at 256 for comparison
	refRotate := func(fp numeric.PhaseDial, a1, a2 float64) numeric.PhaseDial {
		f1 := cmplx.Rect(1.0, a1)
		f2 := cmplx.Rect(1.0, a2)
		rotated := make(numeric.PhaseDial, D)
		for k := 0; k < D; k++ {
			if k < 256 {
				rotated[k] = fp[k] * f1
			} else {
				rotated[k] = fp[k] * f2
			}
		}
		return rotated
	}

	refResult := torusSweep(fpAB, refRotate, fpA, fpB, seedQuery, textB, 5.0)
	console.Info(fmt.Sprintf("\n  Reference (256/256): %.4f at (%.0f°, %.0f°)  Δ=%.4f",
		refResult.bestGain, refResult.bestA1, refResult.bestA2, refResult.bestGain-ceiling))

	// ===== PART B: Gap experiment =====
	console.Info("\n  ── Part B: Gap Experiment ──")

	gapSizes := []int{16, 32, 64}
	var gapResults []GapTestResult

	for _, gapSize := range gapSizes {
		mid := 256
		gapStart := mid
		gapEnd := mid + gapSize

		gapRotate := func(fp numeric.PhaseDial, a1, a2 float64) numeric.PhaseDial {
			f1 := cmplx.Rect(1.0, a1)
			f2 := cmplx.Rect(1.0, a2)
			rotated := make(numeric.PhaseDial, D)
			for k := 0; k < D; k++ {
				if k < gapStart {
					rotated[k] = fp[k] * f1 // block 0
				} else if k >= gapEnd {
					rotated[k] = fp[k] * f2 // block 1
				} else {
					rotated[k] = fp[k] // gap: no rotation
				}
			}
			return rotated
		}

		gapResult := torusSweep(fpAB, gapRotate, fpA, fpB, seedQuery, textB, 5.0)
		delta := gapResult.bestGain - ceiling
		sa := gapResult.bestGain > ceiling

		console.Info(fmt.Sprintf("\n  ┌─ Gap=%d: [0,%d) ∪ gap ∪ [%d,512)", gapSize, gapStart, gapEnd))
		console.Info(fmt.Sprintf("  │  Best: %.4f at (%.0f°, %.0f°)  C: %s", gapResult.bestGain, gapResult.bestA1, gapResult.bestA2, gapResult.bestC))
		if sa {
			console.Info(fmt.Sprintf("  └─ ✓ SUPER-ADDITIVE  Δ=+%.4f", delta))
		} else {
			console.Warn(fmt.Sprintf("  └─ ✗ No super-additive  Δ=%.4f", delta))
		}

		gapResults = append(gapResults, GapTestResult{
			GapSize:       gapSize,
			Block0End:     gapStart,
			Block1Start:   gapEnd,
			BestGain:      gapResult.bestGain,
			Delta:         delta,
			SuperAdditive: sa,
			BestA1:        gapResult.bestA1,
			BestA2:        gapResult.bestA2,
			BestC:         gapResult.bestC,
		})
	}

	// Build top-5 boundary scores for the result
	var topBoundaries []BoundaryScoreResult
	for i := 0; i < topN; i++ {
		s := scores[i]
		topBoundaries = append(topBoundaries, BoundaryScoreResult{
			Boundary: s.b,
			SBalance: s.sBalance,
			KLeft:    s.kLeft,
			KRight:   s.kRight,
			Combined: s.combined,
		})
	}

	return AdaptiveSplitResult{
		SelectedBoundary: bestB,
		TopBoundaries:    topBoundaries,
		AdaptiveGain:     adaptiveResult.bestGain,
		AdaptiveDelta:    adaptiveDelta,
		AdaptiveSA:       adaptiveSA,
		AdaptiveA1:       adaptiveResult.bestA1,
		AdaptiveA2:       adaptiveResult.bestA2,
		AdaptiveC:        adaptiveResult.bestC,
		ReferenceGain:    refResult.bestGain,
		ReferenceDelta:   refResult.bestGain - ceiling,
		Ceiling:          ceiling,
		GapTests:         gapResults,
	}
}
