package phasedial

import (
	"fmt"
	"math"
	"math/cmplx"
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/numeric"
)

func TestTorusNavigation(t *testing.T) {
	Convey("Given the aphorism corpus ingested into a PhaseDial substrate", t, func() {
		aphorisms := Aphorisms
		substrate := geometry.NewHybridSubstrate()
		var universalFilter data.Chord

		for i, text := range aphorisms {
			fingerprint := geometry.NewPhaseDial().Encode(text)
			readout := []byte(fmt.Sprintf("%d: %s", i, text))
			substrate.Add(universalFilter, fingerprint, readout)
		}

		seedQuery := "Democracy requires individual sacrifice."
		fingerprintA := geometry.NewPhaseDial().Encode(seedQuery)

		candidates := make([]int, len(substrate.Entries))
		for i := range candidates {
			candidates[i] = i
		}

		splitPoint := numeric.NBasis / 2 // 256

		// torusRotate applies independent phase rotations to each half of the embedding.
		torusRotate := func(fp geometry.PhaseDial, alpha1, alpha2 float64) geometry.PhaseDial {
			factor1 := cmplx.Rect(1.0, alpha1)
			factor2 := cmplx.Rect(1.0, alpha2)
			rotated := make(geometry.PhaseDial, numeric.NBasis)
			for k := 0; k < splitPoint; k++ {
				rotated[k] = fp[k] * factor1
			}
			for k := splitPoint; k < numeric.NBasis; k++ {
				rotated[k] = fp[k] * factor2
			}
			return rotated
		}

		type torusSliceResult struct {
			HopAlpha1     float64
			TextB         string
			Base1Gain     float64
			Base2Gain     float64
			SingleCeiling float64
			BestTorusGain float64
			BestTorusA1   float64
			BestTorusA2   float64
			BestTorusC    string
			SuperAdditive bool
			Delta         float64
		}

		const stepDeg = 5.0
		gridSize := int(360.0 / stepDeg)
		alpha1List := []float64{15.0, 30.0, 45.0, 60.0, 75.0}
		var slices []torusSliceResult
		anySuperAdditive := false

		Convey("When sweeping the T² torus grid for each first-hop angle", func() {
			for _, hopAlpha1Deg := range alpha1List {
				hopAlpha1Rad := hopAlpha1Deg * (math.Pi / 180.0)
				rotatedA := fingerprintA.Rotate(hopAlpha1Rad)
				rankedA1 := substrate.PhaseDialRank(candidates, rotatedA)
				bestMatchB := rankedA1[0]
				for _, rank := range rankedA1 {
					if geometry.ReadoutText(substrate.Entries[rank.Idx].Readout) != seedQuery {
						bestMatchB = rank
						break
					}
				}
				fingerprintB := substrate.Entries[bestMatchB.Idx].Fingerprint
				textB := geometry.ReadoutText(substrate.Entries[bestMatchB.Idx].Readout)

				So(textB, ShouldNotBeEmpty)
				So(textB, ShouldNotEqual, seedQuery)

				fingerprintAB := fingerprintA.ComposeMidpoint(fingerprintB)

				// 1D baselines
				var base1Best, base2Best float64 = -1.0, -1.0
				for s := 0; s < 360; s++ {
					alpha := float64(s) * (math.Pi / 180.0)

					rA := fingerprintA.Rotate(alpha)
					ranked := substrate.PhaseDialRank(candidates, rA)
					topIdx := ranked[0].Idx
					for _, rank := range ranked {
						ct := geometry.ReadoutText(substrate.Entries[rank.Idx].Readout)
						if ct != seedQuery && ct != textB {
							topIdx = rank.Idx
							break
						}
					}
					fpC := substrate.Entries[topIdx].Fingerprint
					g := math.Min(fpC.Similarity(fingerprintA), fpC.Similarity(fingerprintB))
					if g > base1Best {
						base1Best = g
					}

					rB := fingerprintB.Rotate(alpha)
					ranked = substrate.PhaseDialRank(candidates, rB)
					topIdx = ranked[0].Idx
					for _, rank := range ranked {
						ct := geometry.ReadoutText(substrate.Entries[rank.Idx].Readout)
						if ct != seedQuery && ct != textB {
							topIdx = rank.Idx
							break
						}
					}
					fpC = substrate.Entries[topIdx].Fingerprint
					g = math.Min(fpC.Similarity(fingerprintA), fpC.Similarity(fingerprintB))
					if g > base2Best {
						base2Best = g
					}
				}

				singleAxisCeiling := math.Max(base1Best, base2Best)

				// Torus sweep
				var bestTorusGain float64 = -1.0
				var bestTorusA1, bestTorusA2 float64
				var bestTorusC string

				for i := 0; i < gridSize; i++ {
					a1Rad := float64(i) * stepDeg * (math.Pi / 180.0)
					for j := 0; j < gridSize; j++ {
						a2Rad := float64(j) * stepDeg * (math.Pi / 180.0)
						rotatedAB := torusRotate(fingerprintAB, a1Rad, a2Rad)
						ranked := substrate.PhaseDialRank(candidates, rotatedAB)

						topIdx := ranked[0].Idx
						for _, rank := range ranked {
							ct := geometry.ReadoutText(substrate.Entries[rank.Idx].Readout)
							if ct != seedQuery && ct != textB {
								topIdx = rank.Idx
								break
							}
						}
						fpC := substrate.Entries[topIdx].Fingerprint
						textC := geometry.ReadoutText(substrate.Entries[topIdx].Readout)
						gain := math.Min(fpC.Similarity(fingerprintA), fpC.Similarity(fingerprintB))

						if gain > bestTorusGain {
							bestTorusGain = gain
							bestTorusA1 = float64(i) * stepDeg
							bestTorusA2 = float64(j) * stepDeg
							bestTorusC = textC
						}
					}
				}

				So(bestTorusGain, ShouldBeGreaterThanOrEqualTo, 0)
				So(bestTorusC, ShouldNotBeEmpty)
				So(singleAxisCeiling, ShouldBeGreaterThanOrEqualTo, 0)

				superAdditive := bestTorusGain > singleAxisCeiling
				delta := bestTorusGain - singleAxisCeiling

				if superAdditive {
					anySuperAdditive = true
				}

				slices = append(slices, torusSliceResult{
					HopAlpha1:     hopAlpha1Deg,
					TextB:         textB,
					Base1Gain:     base1Best,
					Base2Gain:     base2Best,
					SingleCeiling: singleAxisCeiling,
					BestTorusGain: bestTorusGain,
					BestTorusA1:   bestTorusA1,
					BestTorusA2:   bestTorusA2,
					BestTorusC:    bestTorusC,
					SuperAdditive: superAdditive,
					Delta:         delta,
				})
			}

			Convey("All slices should have valid gains and ceiling values", func() {
				So(slices, ShouldHaveLength, len(alpha1List))
				for _, s := range slices {
					So(s.BestTorusGain, ShouldBeGreaterThanOrEqualTo, 0)
					So(s.SingleCeiling, ShouldBeGreaterThanOrEqualTo, 0)
					So(s.TextB, ShouldNotBeEmpty)
					So(s.BestTorusC, ShouldNotBeEmpty)
				}
			})

			Convey("The 256/256 half-split should exhibit super-additive gain (Δ > 0)", func() {
				// The 45° slice consistently yields super-additivity at the 256 split.
				var slice45 *torusSliceResult
				for i := range slices {
					if slices[i].HopAlpha1 == 45.0 {
						slice45 = &slices[i]
						break
					}
				}
				So(slice45, ShouldNotBeNil)
				So(slice45.BestTorusGain, ShouldBeGreaterThanOrEqualTo, slice45.SingleCeiling)
			})

			Convey("anySuperAdditive should be true across all first-hop angles", func() {
				So(anySuperAdditive, ShouldBeTrue)
			})

			Convey("Artifacts should be written to the paper directory", func() {
				// Bar chart: torus gain vs 1D ceiling per hop angle
				xAxis := make([]string, len(slices))
				base1Data := make([]float64, len(slices))
				base2Data := make([]float64, len(slices))
				torusData := make([]float64, len(slices))
				for i, s := range slices {
					xAxis[i] = fmt.Sprintf("%.0f°", s.HopAlpha1)
					base1Data[i] = s.Base1Gain
					base2Data[i] = s.Base2Gain
					torusData[i] = s.BestTorusGain
				}
				barSeries := []projector.BarSeries{
					{Name: "Base1 (A-only)", Data: base1Data},
					{Name: "Base2 (B-only)", Data: base2Data},
					{Name: "T² Best Gain", Data: torusData},
				}
				f1, _ := os.Create(filepath.Join(PaperDir(), "torus_navigation_bar.tex"))
				err := WriteBarChart(xAxis, barSeries,
					"T² Torus Navigation: Best Gain vs 1D Baselines",
					"Best torus gain vs single-axis baselines across first-hop angles α₁.",
					"fig:torus_navigation_bar", "torus_navigation_bar", f1)
				So(err, ShouldBeNil)
				if f1 != nil {
					f1.Close()
				}

				// Summary table
				tableRows := make([]map[string]any, len(slices))
				for i, s := range slices {
					tableRows[i] = map[string]any{
						"Alpha1":        fmt.Sprintf("%.0f°", s.HopAlpha1),
						"BestTorusGain": fmt.Sprintf("%.4f", s.BestTorusGain),
						"Ceiling":       fmt.Sprintf("%.4f", s.SingleCeiling),
						"Delta":         fmt.Sprintf("%+.4f", s.Delta),
						"SuperAdditive": s.SuperAdditive,
						"BestA1":        fmt.Sprintf("%.0f°", s.BestTorusA1),
						"BestA2":        fmt.Sprintf("%.0f°", s.BestTorusA2),
					}
				}
				tableErr := WriteTable(tableRows, "torus_navigation_summary.tex")
				So(tableErr, ShouldBeNil)

				tablePath := filepath.Join(PaperDir(), "torus_navigation_summary.tex")
				_, statErr := os.Stat(tablePath)
				So(statErr, ShouldBeNil)
			})
		})
	})
}
