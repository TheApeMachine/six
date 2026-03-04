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

func TestCorrelationLength(t *testing.T) {
	Convey("Given the aphorism corpus and the Democracy seed with a known super-additive split", t, func() {
		aphorisms := Aphorisms
		substrate := geometry.NewHybridSubstrate()
		var universalFilter data.Chord

		for i, text := range aphorisms {
			fingerprint := geometry.NewPhaseDial().Encode(text)
			readout := []byte(fmt.Sprintf("%d: %s", i, text))
			substrate.Add(universalFilter, fingerprint, readout)
		}

		candidates := make([]int, len(substrate.Entries))
		for i := range candidates {
			candidates[i] = i
		}

		seedQuery := "Democracy requires individual sacrifice."
		fingerprintA := geometry.NewPhaseDial().Encode(seedQuery)

		// Hop 1: resolve B at α₁=45°
		rotatedA := fingerprintA.Rotate(45.0 * (math.Pi / 180.0))
		ranked := substrate.PhaseDialRank(candidates, rotatedA)
		bestMatchB := ranked[0]
		for _, rank := range ranked {
			if geometry.ReadoutText(substrate.Entries[rank.Idx].Readout) != seedQuery {
				bestMatchB = rank
				break
			}
		}
		fingerprintB := substrate.Entries[bestMatchB.Idx].Fingerprint
		textB := geometry.ReadoutText(substrate.Entries[bestMatchB.Idx].Readout)
		fingerprintAB := fingerprintA.ComposeMidpoint(fingerprintB)

		So(textB, ShouldNotBeEmpty)
		So(textB, ShouldNotEqual, seedQuery)

		// 1D ceiling
		ceiling := -1.0
		for s := 0; s < 360; s++ {
			alpha := float64(s) * (math.Pi / 180.0)
			for _, anchor := range []geometry.PhaseDial{fingerprintA, fingerprintB} {
				rot := anchor.Rotate(alpha)
				rnk := substrate.PhaseDialRank(candidates, rot)
				topIdx := rnk[0].Idx
				for _, r := range rnk {
					ct := geometry.ReadoutText(substrate.Entries[r.Idx].Readout)
					if ct != seedQuery && ct != textB {
						topIdx = r.Idx
						break
					}
				}
				efp := substrate.Entries[topIdx].Fingerprint
				g := math.Min(efp.Similarity(fingerprintA), efp.Similarity(fingerprintB))
				if g > ceiling {
					ceiling = g
				}
			}
		}
		So(ceiling, ShouldBeGreaterThanOrEqualTo, 0)

		// Hard-partition rotation: dims [0, boundary) get α₁, [boundary, 512) get α₂.
		hardRotate := func(fp geometry.PhaseDial, boundary int, a1, a2 float64) geometry.PhaseDial {
			f1 := cmplx.Rect(1.0, a1)
			f2 := cmplx.Rect(1.0, a2)
			rotated := make(geometry.PhaseDial, numeric.NBasis)
			for k := 0; k < numeric.NBasis; k++ {
				if k < boundary {
					rotated[k] = fp[k] * f1
				} else {
					rotated[k] = fp[k] * f2
				}
			}
			return rotated
		}

		// Overlapping rotation: linear phase blend in the shared region.
		overlapRotate := func(fp geometry.PhaseDial, b0End, b1Start int, a1, a2 float64) geometry.PhaseDial {
			rotated := make(geometry.PhaseDial, numeric.NBasis)
			overlapLen := float64(b0End - b1Start)
			for k := 0; k < numeric.NBasis; k++ {
				var angle float64
				switch {
				case k < b1Start:
					angle = a1
				case k >= b0End:
					angle = a2
				default:
					w := float64(k-b1Start) / overlapLen
					angle = (1.0-w)*a1 + w*a2
				}
				rotated[k] = fp[k] * cmplx.Rect(1.0, angle)
			}
			return rotated
		}

		type splitDef struct {
			name       string
			b0End      int  // hard: boundary; overlap: block0 end
			b1Start    int  // hard: same as b0End; overlap: block1 start
			hasOverlap bool
		}

		splits := []splitDef{
			{"192/320", 192, 192, false},
			{"224/288", 224, 224, false},
			{"256/256", 256, 256, false},
			{"288/224", 288, 288, false},
			{"320/192", 320, 320, false},
			{"320∩192 (overlap 128)", 320, 192, true},
		}

		const stepDeg = 5.0
		gridSize := int(360.0 / stepDeg)

		type splitResult struct {
			Name          string
			Block0Size    int
			SuperAdditive bool
			BestGain      float64
			Delta         float64
			BestA1        float64
			BestA2        float64
			BestC         string
			CorrLenRatio  float64
		}
		var results []splitResult

		Convey("When sweeping the torus grid over hard and overlapping block partitions", func() {
			for _, s := range splits {
				var bestGain float64 = -1.0
				var bestA1, bestA2 float64
				var bestC string

				for i := 0; i < gridSize; i++ {
					a1Rad := float64(i) * stepDeg * (math.Pi / 180.0)
					for j := 0; j < gridSize; j++ {
						a2Rad := float64(j) * stepDeg * (math.Pi / 180.0)

						var rotatedAB geometry.PhaseDial
						if s.hasOverlap {
							rotatedAB = overlapRotate(fingerprintAB, s.b0End, s.b1Start, a1Rad, a2Rad)
						} else {
							rotatedAB = hardRotate(fingerprintAB, s.b0End, a1Rad, a2Rad)
						}

						rnk := substrate.PhaseDialRank(candidates, rotatedAB)
						topIdx := rnk[0].Idx
						for _, rank := range rnk {
							ct := geometry.ReadoutText(substrate.Entries[rank.Idx].Readout)
							if ct != seedQuery && ct != textB {
								topIdx = rank.Idx
								break
							}
						}
						fpC := substrate.Entries[topIdx].Fingerprint
						textC := geometry.ReadoutText(substrate.Entries[topIdx].Readout)
						gain := math.Min(fpC.Similarity(fingerprintA), fpC.Similarity(fingerprintB))

						if gain > bestGain {
							bestGain = gain
							bestA1 = float64(i) * stepDeg
							bestA2 = float64(j) * stepDeg
							bestC = textC
						}
					}
				}

				So(bestGain, ShouldBeGreaterThanOrEqualTo, 0)
				So(bestC, ShouldNotBeEmpty)

				block0 := s.b0End
				if s.hasOverlap {
					block0 = s.b0End - s.b1Start // overlap size
				}
				results = append(results, splitResult{
					Name:          s.name,
					Block0Size:    block0,
					SuperAdditive: bestGain > ceiling,
					BestGain:      bestGain,
					Delta:         bestGain - ceiling,
					BestA1:        bestA1,
					BestA2:        bestA2,
					BestC:         bestC,
					CorrLenRatio:  float64(s.b0End) / 13.0,
				})
			}

			Convey("All splits should produce valid gain measurements", func() {
				So(len(results), ShouldEqual, len(splits))
				for _, r := range results {
					So(r.BestGain, ShouldBeGreaterThanOrEqualTo, 0)
					So(r.BestC, ShouldNotBeEmpty)
				}
			})

			Convey("The 256/256 split should be super-additive (the known result)", func() {
				var r256 *splitResult
				for i := range results {
					if results[i].Name == "256/256" {
						r256 = &results[i]
						break
					}
				}
				So(r256, ShouldNotBeNil)
				So(r256.SuperAdditive, ShouldBeTrue)
			})

			Convey("The overlapping partition should NOT be super-additive (soft blend destroys independence)", func() {
				var rOverlap *splitResult
				for i := range results {
					if results[i].Name == "320∩192 (overlap 128)" {
						rOverlap = &results[i]
						break
					}
				}
				So(rOverlap, ShouldNotBeNil)
				So(rOverlap.SuperAdditive, ShouldBeFalse)
			})

			Convey("Artifacts should be written to the paper directory", func() {
				xAxis := make([]string, len(results))
				gainData := make([]float64, len(results))
				for i, r := range results {
					xAxis[i] = r.Name
					gainData[i] = r.BestGain
				}
				barSeries := []projector.BarSeries{
					{Name: "Best Gain", Data: gainData},
				}
				f, _ := os.Create(filepath.Join(PaperDir(), "corr_length_bar.tex"))
				err := WriteBarChart(xAxis, barSeries,
					"Correlation Length Exploitation: Gain by Split",
					"Best torus gain for each block-size partition. SA requires a hard boundary.",
					"fig:corr_length_bar", "corr_length_bar", f)
				So(err, ShouldBeNil)
				if f != nil {
					f.Close()
				}

				tableRows := make([]map[string]any, len(results))
				for i, r := range results {
					tableRows[i] = map[string]any{
						"Split":         r.Name,
						"BestGain":      fmt.Sprintf("%.4f", r.BestGain),
						"Delta":         fmt.Sprintf("%+.4f", r.Delta),
						"SuperAdditive": r.SuperAdditive,
						"CorrLenRatio":  fmt.Sprintf("%.1f×ℓ", r.CorrLenRatio),
					}
				}
				tableErr := WriteTable(tableRows, "corr_length_summary.tex")
				So(tableErr, ShouldBeNil)

				tablePath := filepath.Join(PaperDir(), "corr_length_summary.tex")
				_, statErr := os.Stat(tablePath)
				So(statErr, ShouldBeNil)
			})
		})
	})
}
