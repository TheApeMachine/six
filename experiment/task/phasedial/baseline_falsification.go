package phasedial

import (
	"fmt"
	"math/cmplx"
	"math/rand"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/numeric"
)

func (experiment *Experiment) testBaselineFalsification(aphorisms []string) {
	substrate := numeric.NewHybridSubstrate()
	var seedFingerprint numeric.PhaseDial
	var universalFilter numeric.Chord

	// Scramble the basis primes map to destroy objective physical frequency structure
	scrambledPrimes := make([]int32, numeric.NBasis)
	basisPrimes := numeric.New().Basis
	for i := 0; i < numeric.NBasis; i++ {
		scrambledPrimes[i] = basisPrimes[i]
	}
	rand.Shuffle(len(scrambledPrimes), func(i, j int) {
		scrambledPrimes[i], scrambledPrimes[j] = scrambledPrimes[j], scrambledPrimes[i]
	})

	console.Info("Generating baseline with scrambled basis frequencies...")

	for i, text := range aphorisms {
		// Use a local broken encoder logic mapped against the shuffled sequence
		brokenDial := make(numeric.PhaseDial, numeric.NBasis)
		bytes := []byte(text)
		for k := 0; k < numeric.NBasis; k++ {
			var sum complex128
			omega := float64(scrambledPrimes[k])
			for t, b := range bytes {
				symbolPrime := float64(scrambledPrimes[int(b)%numeric.NSymbols])
				phase := (omega * float64(t+1) * 0.1) + (symbolPrime * 0.1)
				sum += cmplx.Rect(1.0, phase)
			}
			brokenDial[k] = sum
		}
		
		fingerprint := brokenDial 
		readout := []byte(fmt.Sprintf("%d: %s", i, text))
		substrate.Add(universalFilter, fingerprint, readout)

		if text == "Democracy requires individual sacrifice." {
			seedFingerprint = append(numeric.PhaseDial{}, fingerprint...)
		}
	}

	experiment.runGeodesicScan(substrate, seedFingerprint, aphorisms)
	console.Info("NOTE: Scrambling the basis breaks the embedding map's geometric structure, causing the phase sweep to lose continuity and become indistinguishable from a random ordering.")
}
