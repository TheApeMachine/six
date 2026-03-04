package phasedial

import (
	"fmt"
	"math"
	"math/cmplx"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/numeric"
)

func TestGroupActionEquivariance(t *testing.T) {
	Convey("Given the aphorism corpus ingested into a PhaseDial substrate", t, func() {
		aphorisms := Aphorisms
		substrate := geometry.NewHybridSubstrate()
		var seedFingerprint geometry.PhaseDial
		var universalFilter data.Chord

		for i, text := range aphorisms {
			fingerprint := geometry.NewPhaseDial().Encode(text)
			readout := []byte(fmt.Sprintf("%d: %s", i, text))
			substrate.Add(universalFilter, fingerprint, readout)
			if text == "Democracy requires individual sacrifice." {
				seedFingerprint = append(geometry.PhaseDial{}, fingerprint...)
			}
		}

		candidates := make([]int, len(substrate.Entries))
		for i := range candidates {
			candidates[i] = i
		}

		So(seedFingerprint, ShouldNotBeNil)
		So(len(seedFingerprint), ShouldEqual, numeric.NBasis)

		alphaDegrees := 45.0
		alphaRadians := alphaDegrees * (math.Pi / 180.0)
		betaDegrees := 90.0
		betaRadians := betaDegrees * (math.Pi / 180.0)

		Convey("When applying α=45° then β=90° sequentially vs α+β=135° combined", func() {
			// Action 1: sequential rotation (α then β)
			factorAlpha := cmplx.Rect(1.0, alphaRadians)
			factorBeta := cmplx.Rect(1.0, betaRadians)
			dialSequential := make(geometry.PhaseDial, numeric.NBasis)
			for k, val := range seedFingerprint {
				dialSequential[k] = val * factorAlpha * factorBeta
			}
			rankedSeq := substrate.PhaseDialRank(candidates, dialSequential)

			// Action 2: combined rotation (α + β)
			factorCombined := cmplx.Rect(1.0, alphaRadians+betaRadians)
			dialCombined := make(geometry.PhaseDial, numeric.NBasis)
			for k, val := range seedFingerprint {
				dialCombined[k] = val * factorCombined
			}
			rankedComb := substrate.PhaseDialRank(candidates, dialCombined)

			bestSeqReadout := substrate.Entries[rankedSeq[0].Idx].Readout
			bestCombReadout := substrate.Entries[rankedComb[0].Idx].Readout

			Convey("Sequential and combined rotations must retrieve the same target", func() {
				So(string(bestSeqReadout), ShouldEqual, string(bestCombReadout))
			})

			Convey("Scores from both rotations must be identical (within floating-point tolerance)", func() {
				So(math.Abs(rankedSeq[0].Score-rankedComb[0].Score), ShouldBeLessThan, 1e-10)
			})

			Convey("Both rankings must agree on all top-5 positions", func() {
				top := 5
				if len(rankedSeq) < top {
					top = len(rankedSeq)
				}
				for i := 0; i < top; i++ {
					So(rankedSeq[i].Idx, ShouldEqual, rankedComb[i].Idx)
				}
			})

			Convey("The group action must be exact for several other (α, β) pairs", func() {
				pairs := [][2]float64{
					{30, 60},
					{90, 180},
					{180, 45},
					{270, 90},
				}
				for _, pair := range pairs {
					aRad := pair[0] * (math.Pi / 180.0)
					bRad := pair[1] * (math.Pi / 180.0)
					fa := cmplx.Rect(1.0, aRad)
					fb := cmplx.Rect(1.0, bRad)
					fab := cmplx.Rect(1.0, aRad+bRad)

					seqDial := make(geometry.PhaseDial, numeric.NBasis)
					combDial := make(geometry.PhaseDial, numeric.NBasis)
					for k, v := range seedFingerprint {
						seqDial[k] = v * fa * fb
						combDial[k] = v * fab
					}
					rSeq := substrate.PhaseDialRank(candidates, seqDial)
					rComb := substrate.PhaseDialRank(candidates, combDial)
					So(rSeq[0].Idx, ShouldEqual, rComb[0].Idx)
					So(math.Abs(rSeq[0].Score-rComb[0].Score), ShouldBeLessThan, 1e-10)
				}
			})
		})
	})
}
