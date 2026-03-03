package phasedial

import (
	"fmt"
	"math"
	"math/cmplx"
	"strings"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/numeric"
)

func (experiment *Experiment) testTwoHopRetrieval(aphorisms []string) TwoHopResult {
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

	twoHopResTotal := TwoHopResult{
		SeedQuery: seedQuery,
		Traces: []TwoHopTrace{},
	}

	alpha1List := []float64{15.0, 30.0, 45.0, 60.0, 75.0}

	for _, alpha1Degrees := range alpha1List {
		alpha1Radians := alpha1Degrees * (math.Pi / 180.0)
		factorA1 := cmplx.Rect(1.0, alpha1Radians)
		rotatedA := make(numeric.PhaseDial, numeric.NBasis)
		for k, val := range fingerprintA {
			rotatedA[k] = val * factorA1
		}

	// Retrieve B (ensure B != A)
	rankedA1 := substrate.PhaseDialRank(candidates, rotatedA)
	bestMatchB := rankedA1[0]
	for _, rank := range rankedA1 {
		candText := cleanReadout(string(substrate.Entries[rank.Idx].Readout))
		if candText != seedQuery {
			bestMatchB = rank
			break
		}
	}
	fingerprintB := substrate.Entries[bestMatchB.Idx].Fingerprint
	textB := string(substrate.Entries[bestMatchB.Idx].Readout)

	// Compose F_AB = Normalize(A/|A| + B/|B|) so neither dominates.
	magSqA := 0.0
	magSqB := 0.0
	for k := 0; k < numeric.NBasis; k++ {
		magSqA += real(fingerprintA[k])*real(fingerprintA[k]) + imag(fingerprintA[k])*imag(fingerprintA[k])
		magSqB += real(fingerprintB[k])*real(fingerprintB[k]) + imag(fingerprintB[k])*imag(fingerprintB[k])
	}
	normA := math.Sqrt(magSqA)
	normB := math.Sqrt(magSqB)

	fingerprintAB := make(numeric.PhaseDial, numeric.NBasis)
	var norm float64
	for k := 0; k < numeric.NBasis; k++ {
		valA := fingerprintA[k]
		if normA > 0 { valA /= complex(normA, 0) }
		valB := fingerprintB[k]
		if normB > 0 { valB /= complex(normB, 0) }
		
		fingerprintAB[k] = valA + valB
		r, i := real(fingerprintAB[k]), imag(fingerprintAB[k])
		norm += r*r + i*i
	}
	if norm > 0 {
		norm = math.Sqrt(norm)
		for k := 0; k < numeric.NBasis; k++ {
			fingerprintAB[k] = complex(real(fingerprintAB[k])/norm, imag(fingerprintAB[k])/norm)
		}
	}


	sim := func(x, y numeric.PhaseDial) float64 {
		var dot complex128
		var nx, ny float64
		for i := range x {
			dot += cmplx.Conj(x[i]) * y[i]
			nx += real(x[i])*real(x[i]) + imag(x[i])*imag(x[i])
			ny += real(y[i])*real(y[i]) + imag(y[i])*imag(y[i])
		}
		if nx == 0 || ny == 0 { return 0 }
		return real(dot) / (math.Sqrt(nx) * math.Sqrt(ny))
	}

	console.Info(fmt.Sprintf("Hop 1: A rotated by %.0f° -> Best Match B: %s", alpha1Degrees, cleanReadout(textB)))

	console.Info("\nSweeping α2 on composed F_AB...")
	var bestGain float64 = -1.0
	var bestAlpha2 float64 = -1.0
	var bestC string
	
	twoHopRes := TwoHopResult{
		SeedQuery: seedQuery,
		BestMatchB: cleanReadout(textB),
		Traces: []TwoHopTrace{},
	}

	var bestTrace TwoHopTrace

	for s := 0; s < 360; s++ {
		alpha2Degrees := float64(s)
		alpha2Radians := alpha2Degrees * (math.Pi / 180.0)
		factorA2 := cmplx.Rect(1.0, alpha2Radians)
		rotatedAB := make(numeric.PhaseDial, numeric.NBasis)
		for k, val := range fingerprintAB {
			rotatedAB[k] = val * factorA2
		}

		rankedA2 := substrate.PhaseDialRank(candidates, rotatedAB)
		
		var topCIdx int = rankedA2[0].Idx
		for _, rank := range rankedA2 {
			cText := cleanReadout(string(substrate.Entries[rank.Idx].Readout))
			if cText != seedQuery && cText != cleanReadout(textB) {
				topCIdx = rank.Idx
				break
			}
		}
		fingerprintC := substrate.Entries[topCIdx].Fingerprint
		textC := cleanReadout(string(substrate.Entries[topCIdx].Readout))

		simCA := sim(fingerprintC, fingerprintA)
		simCB := sim(fingerprintC, fingerprintB)
		simCAB := sim(fingerprintC, fingerprintAB)
		gain := math.Min(simCA, simCB)
		balancedSum := 0.5 * (simCA + simCB)
		separation := simCAB - math.Max(simCA, simCB)

		trace := TwoHopTrace{
			Alpha2: alpha2Degrees,
			Gain: gain,
			SimCA: simCA,
			SimCB: simCB,
			MatchText: textC,
			SimCAB: simCAB,
			BalancedSum: balancedSum,
			Separation: separation,
		}
		twoHopRes.Traces = append(twoHopRes.Traces, trace)

		// Print trace every 30 degrees
		if s % 30 == 0 {
			truncC := textC
			if len(truncC) > 35 { truncC = truncC[:32] + "..." }
			console.Info(fmt.Sprintf("  α2 = %3.0f° -> C: %-35s | simCA: %.3f | simCB: %.3f | gain: %.3f | sep: %.3f", alpha2Degrees, truncC, simCA, simCB, gain, separation))
		}

		if gain > bestGain {
			bestGain = gain
			bestAlpha2 = alpha2Degrees
			bestC = textC
			bestTrace = trace
		}
	}

	console.Info(fmt.Sprintf("\nDiagnostic Baselines before full trace evaluation:"))
	simAB := sim(fingerprintA, fingerprintB)
	simAB_A := sim(fingerprintAB, fingerprintA)
	simAB_B := sim(fingerprintAB, fingerprintB)
	console.Info(fmt.Sprintf("  cos(F_AB, F_A) = %.4f", simAB_A))
	console.Info(fmt.Sprintf("  cos(F_AB, F_B) = %.4f", simAB_B))
	console.Info(fmt.Sprintf("  cos(F_A,  F_B) = %.4f\n", simAB))

	console.Info(fmt.Sprintf("\n=> Best composed hop: α2 = %3.0f° -> C: %s (Gain: %.3f)\n", bestAlpha2, bestC, bestGain))

	// Baseline 1: second hop from A only
	console.Info("Baseline 1: Sweeping α2 from A only...")
	var base1BestGain float64 = -1.0
	for s := 0; s < 360; s++ {
		alpha2Degrees := float64(s)
		alpha2Radians := alpha2Degrees * (math.Pi / 180.0)
		factorA2 := cmplx.Rect(1.0, alpha2Radians)
		rotatedA2 := make(numeric.PhaseDial, numeric.NBasis)
		for k, val := range fingerprintA {
			rotatedA2[k] = val * factorA2
		}

		rankedA2 := substrate.PhaseDialRank(candidates, rotatedA2)
		var topCIdx int = rankedA2[0].Idx
		for _, rank := range rankedA2 {
			cText := cleanReadout(string(substrate.Entries[rank.Idx].Readout))
			if cText != seedQuery && cText != cleanReadout(textB) {
				topCIdx = rank.Idx
				break
			}
		}
		fingerprintC := substrate.Entries[topCIdx].Fingerprint

		gain := math.Min(sim(fingerprintC, fingerprintA), sim(fingerprintC, fingerprintB))
		if gain > base1BestGain { base1BestGain = gain }
	}
	console.Info(fmt.Sprintf("=> Baseline 1 Best Gain: %.3f\n", base1BestGain))

	// Baseline 2: second hop from B only
	console.Info("Baseline 2: Sweeping α2 from B only...")
	var base2BestGain float64 = -1.0
	for s := 0; s < 360; s++ {
		alpha2Degrees := float64(s)
		alpha2Radians := alpha2Degrees * (math.Pi / 180.0)
		factorA2 := cmplx.Rect(1.0, alpha2Radians)
		rotatedB := make(numeric.PhaseDial, numeric.NBasis)
		for k, val := range fingerprintB {
			rotatedB[k] = val * factorA2
		}

		rankedB := substrate.PhaseDialRank(candidates, rotatedB)
		var topCIdx int = rankedB[0].Idx
		for _, rank := range rankedB {
			cText := cleanReadout(string(substrate.Entries[rank.Idx].Readout))
			if cText != seedQuery && cText != cleanReadout(textB) {
				topCIdx = rank.Idx
				break
			}
		}
		fingerprintC := substrate.Entries[topCIdx].Fingerprint

		gain := math.Min(sim(fingerprintC, fingerprintA), sim(fingerprintC, fingerprintB))
		if gain > base2BestGain { base2BestGain = gain }
	}
	console.Info(fmt.Sprintf("=> Baseline 2 Best Gain: %.3f\n", base2BestGain))

	cutoffGain := math.Max(base1BestGain, base2BestGain)
	if bestGain > cutoffGain {
		console.Info(fmt.Sprintf("SUCCESS: Composition logic (F_AB) yields higher cross-consistency (%.3f) than isolated geometric bounds (%.3f).", bestGain, cutoffGain))
	} else {
		console.Warn(fmt.Sprintf("NOTE: Composition formulation (%.3f) did not beat single-anchor geometric consistency (%.3f). Reassess.", bestGain, cutoffGain))
	}

	if bestGain > twoHopResTotal.Base1MaxGain { twoHopResTotal.Base1MaxGain = base1BestGain }
	if bestGain > twoHopResTotal.Base2MaxGain { twoHopResTotal.Base2MaxGain = base2BestGain }
	twoHopResTotal.Traces = append(twoHopResTotal.Traces, twoHopRes.Traces...)
	
	// Assuming best traces can just be replaced globally if improved:
	if bestGain > twoHopResTotal.BestComposed.Gain {
		twoHopResTotal.BestMatchB = cleanReadout(textB)
		twoHopResTotal.BestComposed = bestTrace
	}

	} // end outer loop over α1

	return twoHopResTotal
}
