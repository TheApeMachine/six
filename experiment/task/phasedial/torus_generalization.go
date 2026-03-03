package phasedial

import (
	"fmt"
	"math"
	"math/cmplx"
	"math/rand"
	"sort"
	"strings"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/numeric"
)

// SplitConfig defines how to partition the 512 dims into rotational subspaces.
type SplitConfig struct {
	Name    string
	NumAxes int
	DimMap  []int   // DimMap[k] = subspace index for dimension k
	StepDeg float64 // grid step in degrees
}

// contiguousSplit partitions dims into contiguous blocks.
// boundaries are the *end* indices: e.g. [256, 512] → sub0=[0,256), sub1=[256,512).
func contiguousSplit(name string, boundaries []int, stepDeg float64) SplitConfig {
	dimMap := make([]int, numeric.NBasis)
	numAxes := len(boundaries)
	sub := 0
	for k := 0; k < numeric.NBasis; k++ {
		if sub < numAxes-1 && k >= boundaries[sub] {
			sub++
		}
		dimMap[k] = sub
	}
	return SplitConfig{Name: name, NumAxes: numAxes, DimMap: dimMap, StepDeg: stepDeg}
}

// randomSplit assigns dims to subspaces via a deterministic random permutation.
func randomSplit(name string, numAxes, dimsPerAxis int, seed int64, stepDeg float64) SplitConfig {
	rng := rand.New(rand.NewSource(seed))
	perm := rng.Perm(numeric.NBasis)
	dimMap := make([]int, numeric.NBasis)
	for i, dim := range perm {
		sub := i / dimsPerAxis
		if sub >= numAxes {
			sub = numAxes - 1
		}
		dimMap[dim] = sub
	}
	return SplitConfig{Name: name, NumAxes: numAxes, DimMap: dimMap, StepDeg: stepDeg}
}

// energySplit sorts dims by |A[k]|² – |B[k]|² and assigns the bottom half
// (B-dominant) to subspace 0x and the top half (A-dominant) to subspace 1.
func energySplit(name string, fpA, fpB numeric.PhaseDial, stepDeg float64) SplitConfig {
	type dimE struct {
		k    int
		diff float64
	}
	dims := make([]dimE, numeric.NBasis)
	for k := 0; k < numeric.NBasis; k++ {
		eA := real(fpA[k])*real(fpA[k]) + imag(fpA[k])*imag(fpA[k])
		eB := real(fpB[k])*real(fpB[k]) + imag(fpB[k])*imag(fpB[k])
		dims[k] = dimE{k: k, diff: eA - eB}
	}
	sort.Slice(dims, func(i, j int) bool { return dims[i].diff < dims[j].diff })
	dimMap := make([]int, numeric.NBasis)
	half := numeric.NBasis / 2
	for i, d := range dims {
		if i < half {
			dimMap[d.k] = 0 // B-dominant subspace
		} else {
			dimMap[d.k] = 1 // A-dominant subspace
		}
	}
	return SplitConfig{Name: name, NumAxes: 2, DimMap: dimMap, StepDeg: stepDeg}
}

// testTorusGeneralization implements Experiment 10: Split Robustness,
// Spectral Energy Analysis, and Overfitting Check.
//
// Tests whether the T² super-additive result generalises across:
//   - Different split configurations (128/384, 384/128, 4×128, random, energy)
//   - Different seed queries (overfitting check)
//   - Computes per-subspace spectral energy for each (A, B) pair
func (experiment *Experiment) testTorusGeneralization(aphorisms []string) GenResult {
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

	// General rotation: apply independent phase to each subspace.
	generalRotate := func(fp numeric.PhaseDial, cfg SplitConfig, angles []float64) numeric.PhaseDial {
		factors := make([]complex128, cfg.NumAxes)
		for i, a := range angles {
			factors[i] = cmplx.Rect(1.0, a)
		}
		rotated := make(numeric.PhaseDial, numeric.NBasis)
		for k := 0; k < numeric.NBasis; k++ {
			rotated[k] = fp[k] * factors[cfg.DimMap[k]]
		}
		return rotated
	}

	// Spectral energy fraction per subspace.
	spectralEnergy := func(fp numeric.PhaseDial, cfg SplitConfig) []float64 {
		energies := make([]float64, cfg.NumAxes)
		total := 0.0
		for k := 0; k < numeric.NBasis; k++ {
			e := real(fp[k])*real(fp[k]) + imag(fp[k])*imag(fp[k])
			energies[cfg.DimMap[k]] += e
			total += e
		}
		if total > 0 {
			for i := range energies {
				energies[i] /= total
			}
		}
		return energies
	}

	// composeMidpoint builds F_AB = Normalize(Â + B̂).
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

	// compute1DCeiling sweeps 1D rotation from both A and B, returns max gain.
	compute1DCeiling := func(fpA, fpB numeric.PhaseDial, excludeA, excludeB string) float64 {
		ceiling := -1.0
		for s := 0; s < 360; s++ {
			alpha := float64(s) * (math.Pi / 180.0)
			f := cmplx.Rect(1.0, alpha)

			// From A
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

			// From B
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

	// --- Seed queries for overfitting check ---
	seedQueries := []string{
		"Democracy requires individual sacrifice.",
		"Knowledge is power.",
		"Nature does not hurry, yet everything is accomplished.",
	}

	var result GenResult

	for _, seedQuery := range seedQueries {
		fingerprintA := numeric.EncodeText(seedQuery)

		console.Info(fmt.Sprintf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"))
		console.Info(fmt.Sprintf("SEED: \"%s\"", seedQuery))
		console.Info(fmt.Sprintf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"))

		// Hop 1: resolve B at α₁=45° (fixed for consistency)
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

		// Build split configs — energy split depends on (A, B) so built per-seed
		configs := []SplitConfig{
			contiguousSplit("T²-256/256", []int{256, 512}, 5.0),
			contiguousSplit("T²-128/384", []int{128, 512}, 5.0),
			contiguousSplit("T²-384/128", []int{384, 512}, 5.0),
			contiguousSplit("T⁴-4×128", []int{128, 256, 384, 512}, 30.0),
			randomSplit("T²-random", 2, 256, 42, 5.0),
			energySplit("T²-energy", fingerprintA, fingerprintB, 5.0),
		}

		seedResult := GenSeedResult{
			SeedQuery:     seedQuery,
			TextB:         textB,
			SingleCeiling: ceiling,
		}

		for _, cfg := range configs {
			// --- Spectral energy analysis ---
			eA := spectralEnergy(fingerprintA, cfg)
			eB := spectralEnergy(fingerprintB, cfg)

			console.Info(fmt.Sprintf("\n  ┌─ %s (n=%d, step=%.0f°)", cfg.Name, cfg.NumAxes, cfg.StepDeg))
			for sub := 0; sub < cfg.NumAxes; sub++ {
				console.Info(fmt.Sprintf("  │  Sub %d: E_A=%.3f  E_B=%.3f  ΔE=%+.3f", sub, eA[sub], eB[sub], eA[sub]-eB[sub]))
			}

			// --- Grid sweep ---
			stepRad := cfg.StepDeg * (math.Pi / 180.0)
			gridSize := int(360.0 / cfg.StepDeg)
			totalPoints := 1
			for i := 0; i < cfg.NumAxes; i++ {
				totalPoints *= gridSize
			}

			var bestGain float64 = -1.0
			bestAngles := make([]float64, cfg.NumAxes)
			var bestC string
			var bestSimCA, bestSimCB float64

			for flat := 0; flat < totalPoints; flat++ {
				angles := make([]float64, cfg.NumAxes)
				remainder := flat
				for axis := cfg.NumAxes - 1; axis >= 0; axis-- {
					idx := remainder % gridSize
					remainder /= gridSize
					angles[axis] = float64(idx) * stepRad
				}

				rotatedAB := generalRotate(fingerprintAB, cfg, angles)
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
					for i, a := range angles {
						bestAngles[i] = a * (180.0 / math.Pi)
					}
					bestC = textC
					bestSimCA = simCA
					bestSimCB = simCB
				}
			}

			superAdditive := bestGain > ceiling
			delta := bestGain - ceiling

			angStr := ""
			for i, a := range bestAngles {
				if i > 0 {
					angStr += ", "
				}
				angStr += fmt.Sprintf("%.0f°", a)
			}

			console.Info(fmt.Sprintf("  │  Best: %.4f at (%s)  C: %s", bestGain, angStr, bestC))
			if superAdditive {
				console.Info(fmt.Sprintf("  └─ ✓ SUPER-ADDITIVE  Δ=+%.4f", delta))
			} else {
				console.Warn(fmt.Sprintf("  └─ ✗ No super-additive  Δ=%.4f", delta))
			}

			splitResult := GenSplitResult{
				SplitName:     cfg.Name,
				NumAxes:       cfg.NumAxes,
				StepDeg:       cfg.StepDeg,
				BestGain:      bestGain,
				SingleCeiling: ceiling,
				Delta:         delta,
				SuperAdditive: superAdditive,
				BestAngles:    bestAngles,
				BestC:         bestC,
				BestSimCA:     bestSimCA,
				BestSimCB:     bestSimCB,
				EnergyA:       eA,
				EnergyB:       eB,
			}
			seedResult.Splits = append(seedResult.Splits, splitResult)
		}

		result.Seeds = append(result.Seeds, seedResult)
	}

	// --- Summary ---
	superCount := 0
	totalCount := 0
	for _, s := range result.Seeds {
		for _, sp := range s.Splits {
			totalCount++
			if sp.SuperAdditive {
				superCount++
				result.AnySuperAdditive = true
			}
		}
	}

	console.Info(fmt.Sprintf("\n━━━ GENERALIZATION SUMMARY ━━━"))
	console.Info(fmt.Sprintf("  Super-additive configs: %d / %d", superCount, totalCount))

	if result.AnySuperAdditive {
		console.Info("  ==> Multi-axis composition generalises.")
	} else {
		console.Warn("  ==> No generalised super-additive gain.")
	}

	return result
}
