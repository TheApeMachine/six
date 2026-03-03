package phasedial

import (
	"fmt"
	"math"
	"math/cmplx"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/numeric"
)

func (experiment *Experiment) testGroupActionEquivariance(aphorisms []string) {
	substrate := numeric.NewHybridSubstrate()
	var seedFingerprint numeric.PhaseDial
	var universalFilter numeric.Chord

	for i, text := range aphorisms {
		fingerprint := numeric.EncodeText(text)
		readout := []byte(fmt.Sprintf("%d: %s", i, text))
		substrate.Add(universalFilter, fingerprint, readout)

		if text == "Democracy requires individual sacrifice." {
			seedFingerprint = append(numeric.PhaseDial{}, fingerprint...)
		}
	}

	candidates := make([]int, len(substrate.Entries))
	for i := range candidates {
		candidates[i] = i
	}

	alphaDegrees := float64(45)
	alphaRadians := alphaDegrees * (math.Pi / 180.0)
	betaDegrees := float64(90)
	betaRadians := betaDegrees * (math.Pi / 180.0)

	// Action 1: Rotate by alpha, then by beta
	factorAlpha := cmplx.Rect(1.0, alphaRadians)
	factorBeta := cmplx.Rect(1.0, betaRadians)
	
	dialSequential := make(numeric.PhaseDial, numeric.NBasis)
	for k, val := range seedFingerprint {
		dialAlpha := val * factorAlpha
		dialSequential[k] = dialAlpha * factorBeta
	}
	rankedSeq := substrate.PhaseDialRank(candidates, dialSequential)
	bestSeqReadout := substrate.Entries[rankedSeq[0].Idx].Readout

	// Action 2: Rotate directly by (alpha + beta)
	factorCombined := cmplx.Rect(1.0, alphaRadians+betaRadians)
	dialCombined := make(numeric.PhaseDial, numeric.NBasis)
	for k, val := range seedFingerprint {
		dialCombined[k] = val * factorCombined
	}
	rankedComb := substrate.PhaseDialRank(candidates, dialCombined)
	bestCombReadout := substrate.Entries[rankedComb[0].Idx].Readout

	console.Info(fmt.Sprintf("Sweep sequential (α=45° + β=90°): Score: %.3f | Target: %s", rankedSeq[0].Score, string(bestSeqReadout)))
	console.Info(fmt.Sprintf("Sweep combined   (α+β = 135°):     Score: %.3f | Target: %s", rankedComb[0].Score, string(bestCombReadout)))

	if string(bestSeqReadout) == string(bestCombReadout) && math.Abs(rankedSeq[0].Score - rankedComb[0].Score) < 1e-5 {
		console.Info("SUCCESS: Retrieval matches showing explicit global phase rotation symmetry (U(1) action).")
	} else {
		console.Warn("FAILED: Equivariance boundary broke down.")
	}
}
