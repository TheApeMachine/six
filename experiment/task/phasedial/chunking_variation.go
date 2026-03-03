package phasedial

import (
	"fmt"
	"strings"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/numeric"
)

func (experiment *Experiment) testChunkingVariation(aphorisms []string) {
	substrate := numeric.NewHybridSubstrate()
	var seedFingerprint numeric.PhaseDial
	var universalFilter numeric.Chord

	// Create chunks by joining adjacent aphorisms. This keeps the text in the same order
	// but drastically changes the phase-mapping boundaries because phrases are embedded
	// in larger contexts and shifted within the new chunks.
	var chunks []string
	for i := 0; i < len(aphorisms); i += 2 {
		if i+1 < len(aphorisms) {
			chunks = append(chunks, aphorisms[i]+" "+aphorisms[i+1])
		} else {
			chunks = append(chunks, aphorisms[i])
		}
	}

	console.Info(fmt.Sprintf("Re-chunked 24 aphorisms into %d larger chunks...", len(chunks)))

	for i, text := range chunks {
		fingerprint := numeric.EncodeText(text)
		readout := []byte(fmt.Sprintf("Chunk %d: %s", i, text))
		substrate.Add(universalFilter, fingerprint, readout)

		if strings.Contains(text, "Democracy requires individual sacrifice.") {
			// To isolate the seed query properly mathematically against the new combinations, 
			// we generate its direct fingerprint query explicitly.
			seedFingerprint = numeric.EncodeText("Democracy requires individual sacrifice.")
		}
	}
	experiment.runGeodesicScan(substrate, seedFingerprint, aphorisms)
}
