package phasedial

import (
	"fmt"
	"math"
	"math/cmplx"
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	config "github.com/theapemachine/six/core"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/geometry"
)

func TestAdaptiveSplit(t *testing.T) {
	Convey("Given the aphorism corpus and the Democracy seed", t, func() {
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

		D := config.Numeric.NBasis
		seedQuery := "Democracy requires individual sacrifice."
		fpA := geometry.NewPhaseDial().Encode(seedQuery)

		rotatedA := fpA.Rotate(45.0 * (math.Pi / 180.0))
		ranked := substrate.PhaseDialRank(candidates, rotatedA)
		bestMatchB := ranked[0]
		for _, rank := range ranked {
			if geometry.ReadoutText(substrate.Entries[rank.Idx].Readout) != seedQuery {
				bestMatchB = rank
				break
			}
		}
		fpB := substrate.Entries[bestMatchB.Idx].Fingerprint
		textB := geometry.ReadoutText(substrate.Entries[bestMatchB.Idx].Readout)
		fpAB := fpA.ComposeMidpoint(fpB)

		So(textB, ShouldNotBeEmpty)
		So(textB, ShouldNotEqual, seedQuery)

		// 1D ceiling
		ceiling := -1.0
		for s := 0; s < 360; s++ {
			alpha := float64(s) * (math.Pi / 180.0)
			for _, anchor := range []geometry.PhaseDial{fpA, fpB} {
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
				g := math.Min(efp.Similarity(fpA), efp.Similarity(fpB))
				if g > ceiling {
					ceiling = g
				}
			}
		}
		So(ceiling, ShouldBeGreaterThanOrEqualTo, 0)

		// Torus sweep at a given boundary
		torusSweep := func(boundary int) (bestGain, bestA1, bestA2 float64, bestC string) {
			bestGain = -1.0
			const stepDeg = 5.0
			gridSize := int(360.0 / stepDeg)
			f1 := func(a float64) complex128 { return cmplx.Rect(1.0, a) }
			_ = f1
			for i := 0; i < gridSize; i++ {
				a1 := float64(i) * stepDeg * (math.Pi / 180.0)
				a1f := cmplx.Rect(1.0, a1)
				for j := 0; j < gridSize; j++ {
					a2 := float64(j) * stepDeg * (math.Pi / 180.0)
					a2f := cmplx.Rect(1.0, a2)
					rotated := make(geometry.PhaseDial, D)
					for k := 0; k < D; k++ {
						if k < boundary {
							rotated[k] = fpAB[k] * a1f
						} else {
							rotated[k] = fpAB[k] * a2f
						}
					}
					rnk := substrate.PhaseDialRank(candidates, rotated)
					topIdx := rnk[0].Idx
					for _, r := range rnk {
						ct := geometry.ReadoutText(substrate.Entries[r.Idx].Readout)
						if ct != seedQuery && ct != textB {
							topIdx = r.Idx
							break
						}
					}
					efp := substrate.Entries[topIdx].Fingerprint
					textC := geometry.ReadoutText(substrate.Entries[topIdx].Readout)
					gain := math.Min(efp.Similarity(fpA), efp.Similarity(fpB))
					if gain > bestGain {
						bestGain = gain
						bestA1 = float64(i) * stepDeg
						bestA2 = float64(j) * stepDeg
						bestC = textC
					}
				}
			}
			return
		}

		Convey("When computing residual-based boundary scores (Part A)", func() {
			// Compute residual R = Normalize(F_A - F_B)
			residual := make(geometry.PhaseDial, D)
			var rNorm float64
			for k := 0; k < D; k++ {
				residual[k] = fpA[k] - fpB[k]
				rNorm += real(residual[k])*real(residual[k]) + imag(residual[k])*imag(residual[k])
			}
			rNorm = math.Sqrt(rNorm)
			if rNorm > 0 {
				for k := 0; k < D; k++ {
					residual[k] /= complex(rNorm, 0)
				}
			}
			So(rNorm, ShouldBeGreaterThan, 0)

			type boundaryScore struct {
				b        int
				sBalance float64
				kLeft    float64
				kRight   float64
				combined float64
			}

			var scores []boundaryScore
			var bestScore boundaryScore

			for b := 16; b <= D-16; b += 8 {
				var leftMass, rightMass float64
				for k := 0; k < b; k++ {
					leftMass += cmplx.Abs(residual[k])
				}
				for k := b; k < D; k++ {
					rightMass += cmplx.Abs(residual[k])
				}
				totalMass := leftMass + rightMass
				sBalance := 0.0
				if totalMass > 0 {
					sBalance = math.Abs(leftMass-rightMass) / totalMass
				}

				var sumLeft, sumRight complex128
				var nLeft, nRight int
				for k := 0; k < b; k++ {
					mag := cmplx.Abs(residual[k])
					if mag > 0 {
						sumLeft += residual[k] / complex(mag, 0)
						nLeft++
					}
				}
				for k := b; k < D; k++ {
					mag := cmplx.Abs(residual[k])
					if mag > 0 {
						sumRight += residual[k] / complex(mag, 0)
						nRight++
					}
				}
				kLeft := 0.0
				if nLeft > 0 {
					kLeft = cmplx.Abs(sumLeft) / float64(nLeft)
				}
				kRight := 0.0
				if nRight > 0 {
					kRight = cmplx.Abs(sumRight) / float64(nRight)
				}
				combined := math.Min(kLeft, kRight) * (1.0 - sBalance)

				s := boundaryScore{b, sBalance, kLeft, kRight, combined}
				scores = append(scores, s)
				if combined > bestScore.combined {
					bestScore = s
				}
			}

			Convey("The residual scoring should find a best boundary", func() {
				So(len(scores), ShouldBeGreaterThan, 0)
				So(bestScore.b, ShouldBeBetweenOrEqual, 16, D-16)
				So(bestScore.combined, ShouldBeGreaterThan, 0)
				So(bestScore.kLeft, ShouldBeBetweenOrEqual, 0, 1)
				So(bestScore.kRight, ShouldBeBetweenOrEqual, 0, 1)
			})

			Convey("The reference 256/256 split should be super-additive regardless of heuristic choice", func() {
				refGain, _, _, refC := torusSweep(256)
				So(refGain, ShouldBeGreaterThan, ceiling)
				So(refC, ShouldNotBeEmpty)
			})

			Convey("The adaptive boundary should produce a valid torus sweep result", func() {
				adaptGain, adaptA1, adaptA2, adaptC := torusSweep(bestScore.b)
				So(adaptGain, ShouldBeGreaterThanOrEqualTo, 0)
				So(adaptC, ShouldNotBeEmpty)
				So(adaptA1, ShouldBeBetweenOrEqual, 0, 360)
				So(adaptA2, ShouldBeBetweenOrEqual, 0, 360)

				Convey("Gap experiment (Part B): gap=64 should be super-additive", func() {
					type gapResult struct {
						gapSize int
						gain    float64
						superAdd bool
					}
					const mid = 256
					var gapResults []gapResult
					for _, gapSize := range []int{16, 32, 64} {
						gapEnd := mid + gapSize
						var bestGain float64 = -1.0
						const stepDeg = 5.0
						gridSize := int(360.0 / stepDeg)
						for i := 0; i < gridSize; i++ {
							a1 := float64(i) * stepDeg * (math.Pi / 180.0)
							a1f := cmplx.Rect(1.0, a1)
							for j := 0; j < gridSize; j++ {
								a2 := float64(j) * stepDeg * (math.Pi / 180.0)
								a2f := cmplx.Rect(1.0, a2)
								rotated := make(geometry.PhaseDial, D)
								for k := 0; k < D; k++ {
									if k < mid {
										rotated[k] = fpAB[k] * a1f
									} else if k >= gapEnd {
										rotated[k] = fpAB[k] * a2f
									} else {
										rotated[k] = fpAB[k] // gap: no rotation
									}
								}
								rnk := substrate.PhaseDialRank(candidates, rotated)
								topIdx := rnk[0].Idx
								for _, r := range rnk {
									ct := geometry.ReadoutText(substrate.Entries[r.Idx].Readout)
									if ct != seedQuery && ct != textB {
										topIdx = r.Idx
										break
									}
								}
								efp := substrate.Entries[topIdx].Fingerprint
								g := math.Min(efp.Similarity(fpA), efp.Similarity(fpB))
								if g > bestGain {
									bestGain = g
								}
							}
						}
						gapResults = append(gapResults, gapResult{gapSize, bestGain, bestGain > ceiling})
					}

					So(len(gapResults), ShouldEqual, 3)
					// gap=64 acts as a 256/192 hard split — should be super-additive
					var gap64 *gapResult
					for i := range gapResults {
						if gapResults[i].gapSize == 64 {
							gap64 = &gapResults[i]
						}
					}
					So(gap64, ShouldNotBeNil)
					So(gap64.superAdd, ShouldBeTrue)

					Convey("Artifacts should be written to the paper directory", func() {
						// Sort boundary scores descending for top-5 table
						sorted := make([]boundaryScore, len(scores))
						copy(sorted, scores)
						for i := 0; i < len(sorted); i++ {
							for j := i + 1; j < len(sorted); j++ {
								if sorted[j].combined > sorted[i].combined {
									sorted[i], sorted[j] = sorted[j], sorted[i]
								}
							}
						}
						top := sorted
						if len(top) > 5 {
							top = sorted[:5]
						}
						tableRows := make([]map[string]any, len(top))
						for i, s := range top {
							tableRows[i] = map[string]any{
								"Boundary": s.b,
								"SBalance": fmt.Sprintf("%.4f", s.sBalance),
								"KLeft":    fmt.Sprintf("%.4f", s.kLeft),
								"KRight":   fmt.Sprintf("%.4f", s.kRight),
								"Combined": fmt.Sprintf("%.4f", s.combined),
							}
						}
						tableErr := WriteTable(tableRows, "adaptive_split_boundaries.tex")
						So(tableErr, ShouldBeNil)

						// Gap experiment bar chart
						xAxis := make([]string, len(gapResults))
						gainData := make([]float64, len(gapResults))
						for i, r := range gapResults {
							xAxis[i] = fmt.Sprintf("Gap=%d", r.gapSize)
							gainData[i] = r.gain
						}
						barSeries := []projector.BarSeries{
							{Name: "Best Gain", Data: gainData},
						}
						f, _ := os.Create(filepath.Join(PaperDir(), "adaptive_split_gap.tex"))
						err := WriteBarChart(xAxis, barSeries,
							"Adaptive Split: Gap Experiment Results",
							"Best gain for each gap size; gap=64 is equivalent to the 256/192 hard split.",
							"fig:adaptive_split_gap", "adaptive_split_gap", f)
						So(err, ShouldBeNil)
						if f != nil {
							f.Close()
						}

						// Adaptive vs reference summary table
						refGain, refA1, refA2, _ := torusSweep(256)
						summaryRows := []map[string]any{
							{
								"Split":         fmt.Sprintf("Adaptive (b=%d)", bestScore.b),
								"BestGain":       fmt.Sprintf("%.4f", adaptGain),
								"Delta":          fmt.Sprintf("%+.4f", adaptGain-ceiling),
								"SuperAdditive":  adaptGain > ceiling,
								"BestA1":         fmt.Sprintf("%.0f°", adaptA1),
								"BestA2":         fmt.Sprintf("%.0f°", adaptA2),
							},
							{
								"Split":         "Reference (b=256)",
								"BestGain":       fmt.Sprintf("%.4f", refGain),
								"Delta":          fmt.Sprintf("%+.4f", refGain-ceiling),
								"SuperAdditive":  refGain > ceiling,
								"BestA1":         fmt.Sprintf("%.0f°", refA1),
								"BestA2":         fmt.Sprintf("%.0f°", refA2),
							},
						}
						summaryErr := WriteTable(summaryRows, "adaptive_split_summary.tex")
						So(summaryErr, ShouldBeNil)

						tablePath := filepath.Join(PaperDir(), "adaptive_split_summary.tex")
						_, statErr := os.Stat(tablePath)
						So(statErr, ShouldBeNil)
					})
				})
			})
		})
	})
}
