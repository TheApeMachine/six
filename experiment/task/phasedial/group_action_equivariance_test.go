package phasedial

import (
	"math"
	"math/cmplx"
	"os"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	config "github.com/theapemachine/six/core"
	"github.com/theapemachine/six/geometry"
)

func TestGroupActionEquivariance(t *testing.T) {
	Convey("Given the aphorism corpus ingested into a PhaseDial substrate", t, func() {
		sub := NewSubstrate()
		seedQuery := "Democracy requires individual sacrifice."

		var seedFP geometry.PhaseDial
		for _, e := range sub.Entries {
			if geometry.ReadoutText(e.Readout) == seedQuery {
				seedFP = append(geometry.PhaseDial{}, e.Fingerprint...)
				break
			}
		}
		So(seedFP, ShouldNotBeNil)
		So(len(seedFP), ShouldEqual, config.Numeric.NBasis)

		alphaDeg, betaDeg := 45.0, 90.0
		alphaRad := alphaDeg * (math.Pi / 180.0)
		betaRad := betaDeg * (math.Pi / 180.0)

		Convey("When applying α=45° then β=90° sequentially vs α+β=135° combined", func() {
			seqDial := make(geometry.PhaseDial, config.Numeric.NBasis)
			combDial := make(geometry.PhaseDial, config.Numeric.NBasis)
			fa, fb := cmplx.Rect(1.0, alphaRad), cmplx.Rect(1.0, betaRad)
			fab := cmplx.Rect(1.0, alphaRad+betaRad)
			for k, v := range seedFP {
				seqDial[k] = v * fa * fb
				combDial[k] = v * fab
			}
			rSeq := sub.PhaseDialRank(sub.Candidates, seqDial)
			rComb := sub.PhaseDialRank(sub.Candidates, combDial)

			Convey("Sequential and combined rotations must retrieve the same target", func() {
				So(string(sub.Entries[rSeq[0].Idx].Readout), ShouldEqual, string(sub.Entries[rComb[0].Idx].Readout))
			})

			Convey("Scores from both rotations must be identical (within floating-point tolerance)", func() {
				So(math.Abs(rSeq[0].Score-rComb[0].Score), ShouldBeLessThan, 1e-10)
			})

			Convey("Both rankings must agree on all top-5 positions", func() {
				top := min(5, len(rSeq))
				for i := 0; i < top; i++ {
					So(rSeq[i].Idx, ShouldEqual, rComb[i].Idx)
				}
			})

			Convey("The group action must be exact for several other (α, β) pairs", func() {
				pairs := [][2]float64{{30, 60}, {90, 180}, {180, 45}, {270, 90}}
				maxDev := 0.0
				for _, pair := range pairs {
					aRad := pair[0] * (math.Pi / 180.0)
					bRad := pair[1] * (math.Pi / 180.0)
					fa2 := cmplx.Rect(1.0, aRad)
					fb2 := cmplx.Rect(1.0, bRad)
					fab2 := cmplx.Rect(1.0, aRad+bRad)
					sd := make(geometry.PhaseDial, config.Numeric.NBasis)
					cd := make(geometry.PhaseDial, config.Numeric.NBasis)
					for k, v := range seedFP {
						sd[k] = v * fa2 * fb2
						cd[k] = v * fab2
					}
					rs := sub.PhaseDialRank(sub.Candidates, sd)
					rc := sub.PhaseDialRank(sub.Candidates, cd)
					So(rs[0].Idx, ShouldEqual, rc[0].Idx)
					dev := math.Abs(rs[0].Score - rc[0].Score)
					So(dev, ShouldBeLessThan, 1e-10)
					if dev > maxDev {
						maxDev = dev
					}
				}

				Convey("Artifact: write equivariance subsection prose", func() {
					tmpl, err := os.ReadFile("prose/equivariance.tex.tmpl")
					So(err, ShouldBeNil)
					So(WriteProse(string(tmpl), map[string]any{
						"Alpha":         alphaDeg,
						"Beta":          betaDeg,
						"AlphaPlusBeta": alphaDeg + betaDeg,
						"MaxDev":        maxDev,
						"Tolerance":     1e-10,
					}, "equivariance.tex"), ShouldBeNil)
				})
			})
		})
	})
}
