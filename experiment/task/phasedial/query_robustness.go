package phasedial

import (
	"fmt"
	"math/rand"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/numeric"
)

func (experiment *Experiment) testQueryRobustness(aphorisms []string) {
	substrate := numeric.NewHybridSubstrate()
	var seedFingerprint numeric.PhaseDial
	var universalFilter numeric.Chord

	for i, text := range aphorisms {
		fingerprint := numeric.EncodeText(text)
		readout := []byte(fmt.Sprintf("%d: %s", i, text))
		substrate.Add(universalFilter, fingerprint, readout)
	}

	// 30% dropout on query
	rawQuery := "Democracy requires individual sacrifice."
	queryBytes := []byte(rawQuery)
	var maskedQuery []byte
	
	for _, b := range queryBytes {
		if rand.Float32() > 0.3 {
			maskedQuery = append(maskedQuery, b)
		} else {
			maskedQuery = append(maskedQuery, '_') // corrupt standard token identity
		}
	}

	console.Info(fmt.Sprintf("Original Query: %s", rawQuery))
	console.Info(fmt.Sprintf("Corrupted Query (30%% drops): %s", string(maskedQuery)))

	seedFingerprint = numeric.EncodeText(string(maskedQuery))
	experiment.runGeodesicScan(substrate, seedFingerprint, aphorisms)
}
