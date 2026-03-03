package phasedial

import (
	"fmt"
	"math/rand"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/numeric"
)

func (experiment *Experiment) testPartialDeletion(aphorisms []string) {
	substrate := numeric.NewHybridSubstrate()
	var seedFingerprint numeric.PhaseDial
	var universalFilter numeric.Chord

	// We will drop ~30% of items (7 out of 24), but ensure critical keys aren't dropped
	// to see the topology persist over the remaining items.
	criticalMap := map[string]bool{
		"Democracy requires individual sacrifice.": true,
		"Nature does not hurry, yet everything is accomplished.": true,
		"Authoritarianism emerges from collective self-interest.": true,
		"A rolling stone gathers no moss.": true, // A known semantic neighbor from earlier scans
	}

	dropCount := 0
	targetDrops := 7
	
	console.Info(fmt.Sprintf("Deleting %d items at random...", targetDrops))

	var kept []string
	for _, text := range aphorisms {
		if !criticalMap[text] && dropCount < targetDrops && rand.Float32() < 0.4 {
			dropCount++
			continue
		}
		kept = append(kept, text)
	}

	console.Info(fmt.Sprintf("Ingesting remaining %d items...", len(kept)))

	for i, text := range kept {
		fingerprint := numeric.EncodeText(text)
		readout := []byte(fmt.Sprintf("%d: %s", i, text))
		substrate.Add(universalFilter, fingerprint, readout)

		if text == "Democracy requires individual sacrifice." {
			seedFingerprint = append(numeric.PhaseDial{}, fingerprint...)
		}
	}
	experiment.runGeodesicScan(substrate, seedFingerprint, aphorisms)
}
