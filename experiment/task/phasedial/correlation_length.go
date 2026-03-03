package phasedial

import (
	"fmt"
	"math"
	"math/cmplx"
	"strings"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/numeric"
)

// testCorrelationLength implements Experiment 12: Correlation Length Exploitation.
//
// Part A: Block Size Sweep
// Tests asymmetric splits to find where gain peaks relative to the measured
// correlation length (≈13 indices). Larger blocks contain more multiples
// of the correlation length and therefore more exploitable "phase texture."
//
// Part B: Overlapping Blocks (Soft Partition)
// Tests whether overlapping blocks (shared dims with blended rotation)
// preserve more distance structure than hard partitions.
func (experiment *Experiment) testCorrelationLength(aphorisms []string) CorrLenResult {
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

	// Split definition: block boundaries + optional overlap blend
	type splitDef struct {
		name       string
		b0Start    int // block 0 range
		b0End      int
		b1Start    int // block 1 range
		b1End      int
		hasOverlap bool
	}

	// Part A: Hard partition sweep (b0End == b1Start, no overlap)
	// Part B: Overlapping partition (b0End > b1Start)
	splits := []splitDef{
		{"192/320", 0, 192, 192, 512, false},
		{"224/288", 0, 224, 224, 512, false},
		{"256/256", 0, 256, 256, 512, false},
		{"288/224", 0, 288, 288, 512, false},
		{"320/192", 0, 320, 320, 512, false},
		{"320∩192 (overlap 128)", 0, 320, 192, 512, true},
	}

	// Rotation function for overlapping blocks.
	// Dims in [b0Start, overlapStart): pure α₁
	// Dims in [overlapStart, overlapEnd): blended
	// Dims in [overlapEnd, b1End): pure α₂
	overlapRotate := func(fp numeric.PhaseDial, s splitDef, a1, a2 float64) numeric.PhaseDial {
		rotated := make(numeric.PhaseDial, numeric.NBasis)

		overlapStart := s.b1Start // where block 1 begins (and overlap starts)
		overlapEnd := s.b0End     // where block 0 ends (and overlap ends)
		overlapLen := float64(overlapEnd - overlapStart)

		for k := 0; k < numeric.NBasis; k++ {
			var angle float64
			if k < overlapStart {
				angle = a1 // pure block 0
			} else if k >= overlapEnd {
				angle = a2 // pure block 1
			} else {
				// Linear blend in overlap region
				w := float64(k-overlapStart) / overlapLen
				angle = (1.0-w)*a1 + w*a2
			}
			rotated[k] = fp[k] * cmplx.Rect(1.0, angle)
		}
		return rotated
	}

	// Hard partition rotation
	hardRotate := func(fp numeric.PhaseDial, boundary int, a1, a2 float64) numeric.PhaseDial {
		f1 := cmplx.Rect(1.0, a1)
		f2 := cmplx.Rect(1.0, a2)
		rotated := make(numeric.PhaseDial, numeric.NBasis)
		for k := 0; k < numeric.NBasis; k++ {
			if k < boundary {
				rotated[k] = fp[k] * f1
			} else {
				rotated[k] = fp[k] * f2
			}
		}
		return rotated
	}

	// 1D ceiling
	compute1DCeiling := func(fpA, fpB numeric.PhaseDial, excludeA, excludeB string) float64 {
		ceiling := -1.0
		for s := 0; s < 360; s++ {
			alpha := float64(s) * (math.Pi / 180.0)
			f := cmplx.Rect(1.0, alpha)

			rA := make(numeric.PhaseDial, numeric.NBasis)
			for k, v := range fpA {
				rA[k] = v * f
			}
			ranked := substrate.PhaseDialRank(candidates, rA)
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

			rB := make(numeric.PhaseDial, numeric.NBasis)
			for k, v := range fpB {
				rB[k] = v * f
			}
			ranked = substrate.PhaseDialRank(candidates, rB)
			topIdx = ranked[0].Idx
			for _, rank := range ranked {
				ct := cleanReadout(string(substrate.Entries[rank.Idx].Readout))
				if ct != excludeA && ct != excludeB {
					topIdx = rank.Idx
					break
				}
			}
			g = math.Min(sim(substrate.Entries[topIdx].Fingerprint, fpA),
				sim(substrate.Entries[topIdx].Fingerprint, fpB))
			if g > ceiling {
				ceiling = g
			}
		}
		return ceiling
	}

	// Use "Democracy" seed (the one that showed super-additivity)
	seedQuery := "Democracy requires individual sacrifice."
	fingerprintA := numeric.EncodeText(seedQuery)

	// Hop 1: get B at α₁=45°
	hopFactor := cmplx.Rect(1.0, 45.0*(math.Pi/180.0))
	rotatedA := make(numeric.PhaseDial, numeric.NBasis)
	for k, val := range fingerprintA {
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
	fingerprintB := substrate.Entries[bestMatchB.Idx].Fingerprint
	textB := cleanReadout(string(substrate.Entries[bestMatchB.Idx].Readout))
	console.Info(fmt.Sprintf("  B = %s", textB))

	// Compose midpoint
	fingerprintAB := composeMidpoint(fingerprintA, fingerprintB)

	// 1D ceiling
	ceiling := compute1DCeiling(fingerprintA, fingerprintB, seedQuery, textB)
	console.Info(fmt.Sprintf("  1D Ceiling: %.4f", ceiling))

	// Grid sweep each split config
	const stepDeg = 5.0
	gridSize := int(360.0 / stepDeg)

	var result CorrLenResult

	for _, s := range splits {
		var bestGain float64 = -1.0
		var bestA1, bestA2 float64
		var bestC string

		for i := 0; i < gridSize; i++ {
			a1Rad := float64(i) * stepDeg * (math.Pi / 180.0)
			for j := 0; j < gridSize; j++ {
				a2Rad := float64(j) * stepDeg * (math.Pi / 180.0)

				var rotatedAB numeric.PhaseDial
				if s.hasOverlap {
					rotatedAB = overlapRotate(fingerprintAB, s, a1Rad, a2Rad)
				} else {
					rotatedAB = hardRotate(fingerprintAB, s.b0End, a1Rad, a2Rad)
				}

				ranked := substrate.PhaseDialRank(candidates, rotatedAB)
				topIdx := ranked[0].Idx
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
				gain := math.Min(simCA, simCB)

				if gain > bestGain {
					bestGain = gain
					bestA1 = float64(i) * stepDeg
					bestA2 = float64(j) * stepDeg
					bestC = textC
				}
			}
		}

		superAdditive := bestGain > ceiling
		delta := bestGain - ceiling

		console.Info(fmt.Sprintf("\n  ┌─ %s", s.name))
		console.Info(fmt.Sprintf("  │  Block 0: [%d, %d)  Block 1: [%d, %d)", s.b0Start, s.b0End, s.b1Start, s.b1End))
		if s.hasOverlap {
			console.Info(fmt.Sprintf("  │  Overlap: [%d, %d) = %d dims", s.b1Start, s.b0End, s.b0End-s.b1Start))
		}
		console.Info(fmt.Sprintf("  │  Best: %.4f at (%.0f°, %.0f°)  C: %s", bestGain, bestA1, bestA2, bestC))
		if superAdditive {
			console.Info(fmt.Sprintf("  └─ ✓ SUPER-ADDITIVE  Δ=+%.4f  (%.1f× corr length per block)", delta, float64(s.b0End-s.b0Start)/13.0))
		} else {
			console.Warn(fmt.Sprintf("  └─ ✗ No super-additive  Δ=%.4f  (%.1f× corr length per block)", delta, float64(s.b0End-s.b0Start)/13.0))
		}

		result.Splits = append(result.Splits, CorrLenSplitResult{
			Name:          s.name,
			Block0Size:    s.b0End - s.b0Start,
			Block1Size:    s.b1End - s.b1Start,
			Overlap:       0,
			BestGain:      bestGain,
			SingleCeiling: ceiling,
			Delta:         delta,
			SuperAdditive: superAdditive,
			BestA1:        bestA1,
			BestA2:        bestA2,
			BestC:         bestC,
			CorrLenRatio:  float64(s.b0End-s.b0Start) / 13.0,
		})
		if s.hasOverlap {
			result.Splits[len(result.Splits)-1].Overlap = s.b0End - s.b1Start
		}
	}

	return result
}
