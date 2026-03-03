package phasedial

import (
	"fmt"
	"math"
	"math/cmplx"
	"strings"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/numeric"
)

// testSteerability implements Experiment 14: Steerability Index.
//
// Computes the Jaccard-based steerability metric for each candidate split boundary:
//   Steer(S, F) = mean Jaccard distance of top-K sets under block rotation
// Then picks the boundary that maximizes min(Steer_left, Steer_right).
//
// Also validates the metric against known-good (256/256, 320/192) and
// known-bad (224/288) splits, and computes steerability for gap=64 blocks.
func (experiment *Experiment) testSteerability(aphorisms []string) SteerResult {
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

	D := numeric.NBasis // 512

	// Top-K retrieval as a set of indices
	const K = 8
	topKSet := func(fp numeric.PhaseDial) map[int]bool {
		ranked := substrate.PhaseDialRank(candidates, fp)
		set := make(map[int]bool, K)
		for i := 0; i < K && i < len(ranked); i++ {
			set[ranked[i].Idx] = true
		}
		return set
	}

	// Jaccard distance between two sets
	jaccard := func(a, b map[int]bool) float64 {
		inter := 0
		for k := range a {
			if b[k] {
				inter++
			}
		}
		union := len(a) + len(b) - inter
		if union == 0 {
			return 0
		}
		return 1.0 - float64(inter)/float64(union)
	}

	// Rotate only dims in [start, end) by angle alpha
	rotateBlock := func(fp numeric.PhaseDial, alpha float64, start, end int) numeric.PhaseDial {
		rotated := make(numeric.PhaseDial, D)
		copy(rotated, fp)
		f := cmplx.Rect(1.0, alpha)
		for k := start; k < end; k++ {
			rotated[k] = fp[k] * f
		}
		return rotated
	}

	// Steerability: mean Jaccard distance of consecutive top-K sets
	// under 12-step rotation of [start, end)
	const nAngles = 12
	steerability := func(fp numeric.PhaseDial, start, end int) float64 {
		var topKSets []map[int]bool
		for i := 0; i < nAngles; i++ {
			alpha := float64(i) * (2.0 * math.Pi / float64(nAngles))
			rotated := rotateBlock(fp, alpha, start, end)
			topKSets = append(topKSets, topKSet(rotated))
		}

		sumJ := 0.0
		for i := 0; i < nAngles; i++ {
			next := (i + 1) % nAngles
			sumJ += jaccard(topKSets[i], topKSets[next])
		}
		return sumJ / float64(nAngles)
	}

	// Compose midpoint using Democracy seed
	seedQuery := "Democracy requires individual sacrifice."
	fpA := numeric.EncodeText(seedQuery)

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

	// Compose midpoint
	var magSqA, magSqB float64
	for k := 0; k < D; k++ {
		magSqA += real(fpA[k])*real(fpA[k]) + imag(fpA[k])*imag(fpA[k])
		magSqB += real(fpB[k])*real(fpB[k]) + imag(fpB[k])*imag(fpB[k])
	}
	nA := math.Sqrt(magSqA)
	nB := math.Sqrt(magSqB)
	fpAB := make(numeric.PhaseDial, D)
	var norm float64
	for k := 0; k < D; k++ {
		vA := fpA[k]
		if nA > 0 {
			vA /= complex(nA, 0)
		}
		vB := fpB[k]
		if nB > 0 {
			vB /= complex(nB, 0)
		}
		fpAB[k] = vA + vB
		r, im := real(fpAB[k]), imag(fpAB[k])
		norm += r*r + im*im
	}
	norm = math.Sqrt(norm)
	for k := 0; k < D; k++ {
		fpAB[k] = complex(real(fpAB[k])/norm, imag(fpAB[k])/norm)
	}

	console.Info(fmt.Sprintf("  B = %s", textB))

	// 1D ceiling
	ceiling := -1.0
	for s := 0; s < 360; s++ {
		alpha := float64(s) * (math.Pi / 180.0)
		f := cmplx.Rect(1.0, alpha)
		for _, anchor := range []numeric.PhaseDial{fpA, fpB} {
			rot := make(numeric.PhaseDial, D)
			for k, v := range anchor {
				rot[k] = v * f
			}
			rnk := substrate.PhaseDialRank(candidates, rot)
			topIdx := rnk[0].Idx
			for _, r := range rnk {
				ct := cleanReadout(string(substrate.Entries[r.Idx].Readout))
				if ct != seedQuery && ct != textB {
					topIdx = r.Idx
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
	console.Info(fmt.Sprintf("  1D Ceiling: %.4f", ceiling))

	// ===== Part A: Steerability profile across all boundaries =====
	console.Info("\n  ── Steerability Profile ──")

	type splitSteer struct {
		b      int
		sLeft  float64
		sRight float64
		sMin   float64
	}

	var allSteers []splitSteer
	var bestSplit splitSteer

	// Sweep boundaries from 64 to 448 in steps of 8
	for b := 64; b <= D-64; b += 8 {
		sL := steerability(fpAB, 0, b)
		sR := steerability(fpAB, b, D)
		sMin := math.Min(sL, sR)

		s := splitSteer{b, sL, sR, sMin}
		allSteers = append(allSteers, s)

		if sMin > bestSplit.sMin {
			bestSplit = s
		}
	}

	// Report key boundaries
	keyBounds := []int{192, 224, 256, 288, 320}
	console.Info("  Key boundary steerability:")
	for _, kb := range keyBounds {
		for _, s := range allSteers {
			if s.b == kb {
				marker := "  "
				if kb == bestSplit.b {
					marker = "★ "
				}
				console.Info(fmt.Sprintf("    %sb=%3d  Steer_L=%.4f  Steer_R=%.4f  min=%.4f",
					marker, s.b, s.sLeft, s.sRight, s.sMin))
				break
			}
		}
	}

	// Report best
	console.Info(fmt.Sprintf("\n  Best split: b=%d  (min steer=%.4f)", bestSplit.b, bestSplit.sMin))

	// ===== Part B: Validate steerability predicts super-additivity =====
	console.Info("\n  ── Validation: Steerability vs Super-Additive Gain ──")

	type validationResult struct {
		name      string
		b         int
		sLeft     float64
		sRight    float64
		sMin      float64
		gain      float64
		delta     float64
		sa        bool
	}

	validateSplit := func(name string, boundary int) validationResult {
		sL := steerability(fpAB, 0, boundary)
		sR := steerability(fpAB, boundary, D)
		sMin := math.Min(sL, sR)

		// Run torus sweep
		const stepDeg = 5.0
		gridSize := int(360.0 / stepDeg)
		bestGain := -1.0
		for i := 0; i < gridSize; i++ {
			a1 := float64(i) * stepDeg * (math.Pi / 180.0)
			for j := 0; j < gridSize; j++ {
				a2 := float64(j) * stepDeg * (math.Pi / 180.0)

				f1 := cmplx.Rect(1.0, a1)
				f2 := cmplx.Rect(1.0, a2)
				rotated := make(numeric.PhaseDial, D)
				for k := 0; k < D; k++ {
					if k < boundary {
						rotated[k] = fpAB[k] * f1
					} else {
						rotated[k] = fpAB[k] * f2
					}
				}

				rnk := substrate.PhaseDialRank(candidates, rotated)
				topIdx := rnk[0].Idx
				for _, r := range rnk {
					ct := cleanReadout(string(substrate.Entries[r.Idx].Readout))
					if ct != seedQuery && ct != textB {
						topIdx = r.Idx
						break
					}
				}
				g := math.Min(sim(substrate.Entries[topIdx].Fingerprint, fpA),
					sim(substrate.Entries[topIdx].Fingerprint, fpB))
				if g > bestGain {
					bestGain = g
				}
			}
		}

		delta := bestGain - ceiling
		sa := bestGain > ceiling
		return validationResult{name, boundary, sL, sR, sMin, bestGain, delta, sa}
	}

	validations := []struct{ name string; b int }{
		{"192/320", 192},
		{"224/288", 224},
		{"256/256", 256},
		{"288/224", 288},
		{"320/192", 320},
	}

	var valResults []SteerValidation
	for _, v := range validations {
		r := validateSplit(v.name, v.b)
		saMarker := "✗"
		if r.sa {
			saMarker = "✓"
		}
		console.Info(fmt.Sprintf("    %s  b=%d  Steer_L=%.4f  Steer_R=%.4f  min=%.4f  gain=%.4f  Δ=%+.4f  %s",
			r.name, r.b, r.sLeft, r.sRight, r.sMin, r.gain, r.delta, saMarker))

		valResults = append(valResults, SteerValidation{
			Name:          r.name,
			Boundary:      r.b,
			SteerLeft:     r.sLeft,
			SteerRight:    r.sRight,
			SteerMin:      r.sMin,
			Gain:          r.gain,
			Delta:         r.delta,
			SuperAdditive: r.sa,
		})
	}

	// ===== Part C: Gap=64 steerability =====
	console.Info("\n  ── Gap=64 Block Steerability ──")
	gapSL := steerability(fpAB, 0, 256)
	gapSR := steerability(fpAB, 320, D)
	console.Info(fmt.Sprintf("    Block [0, 256):   Steer=%.4f", gapSL))
	console.Info(fmt.Sprintf("    Block [320, 512): Steer=%.4f", gapSR))
	console.Info(fmt.Sprintf("    min(Steer)=%.4f", math.Min(gapSL, gapSR)))

	// Build top-5 boundaries by min steer for result
	// Sort by sMin descending
	sorted := make([]splitSteer, len(allSteers))
	copy(sorted, allSteers)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].sMin > sorted[i].sMin {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	var topBoundaries []SteerBoundary
	topN := 5
	if len(sorted) < topN {
		topN = len(sorted)
	}
	for i := 0; i < topN; i++ {
		s := sorted[i]
		topBoundaries = append(topBoundaries, SteerBoundary{
			Boundary:   s.b,
			SteerLeft:  s.sLeft,
			SteerRight: s.sRight,
			SteerMin:   s.sMin,
		})
	}

	// Check if steerability correctly predicts super-additivity
	// (higher min-steer → super-additive)
	console.Info("\n  ── Prediction Check ──")
	correctPredictions := 0
	steerThreshold := 0.0
	for _, v := range valResults {
		if v.SuperAdditive && v.SteerMin > steerThreshold {
			steerThreshold = v.SteerMin
		}
	}
	// Find the max min-steer among non-SA results
	maxNonSA := 0.0
	for _, v := range valResults {
		if !v.SuperAdditive && v.SteerMin > maxNonSA {
			maxNonSA = v.SteerMin
		}
	}

	separable := steerThreshold > maxNonSA
	for _, v := range valResults {
		predicted := v.SteerMin > maxNonSA
		actual := v.SuperAdditive
		if predicted == actual {
			correctPredictions++
		}
	}

	console.Info(fmt.Sprintf("    SA threshold: min_steer > %.4f", maxNonSA))
	console.Info(fmt.Sprintf("    Correct predictions: %d/%d", correctPredictions, len(valResults)))
	console.Info(fmt.Sprintf("    Cleanly separable: %v", separable))

	return SteerResult{
		SelectedBoundary: bestSplit.b,
		TopBoundaries:    topBoundaries,
		Validations:      valResults,
		GapSteerLeft:     gapSL,
		GapSteerRight:    gapSR,
		Ceiling:          ceiling,
		Separable:        separable,
		Accuracy:         float64(correctPredictions) / float64(len(valResults)),
	}
}
