package phasedial

import (
	"fmt"
	"math/rand"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/numeric"
)

func (experiment *Experiment) testPermutationInvariance(aphorisms []string) []ScanResult {
	substrate := numeric.NewHybridSubstrate()
	var seedFingerprint numeric.PhaseDial
	var universalFilter numeric.Chord

	shuffled := append([]string{}, aphorisms...)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	console.Info(fmt.Sprintf("Ingesting %d items in random order...", len(shuffled)))
	for i, text := range shuffled {
		fingerprint := numeric.EncodeText(text)
		readout := []byte(fmt.Sprintf("%d: %s", i, text))
		substrate.Add(universalFilter, fingerprint, readout)

		if text == "Democracy requires individual sacrifice." {
			seedFingerprint = append(numeric.PhaseDial{}, fingerprint...)
		}
	}
	return experiment.runGeodesicScan(substrate, seedFingerprint, aphorisms)
}
