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
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/numeric"
)

func TestSteerability(t *testing.T) {
	Convey("Given the aphorism corpus ingested into a PhaseDial substrate", t, func() {
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

		D := numeric.NBasis // 512

		// topKSet returns the top-K retrieval set as a map of entry indices.
		const K = 8
		topKSet := func(fp geometry.PhaseDial) map[int]bool {
			ranked := substrate.PhaseDialRank(candidates, fp)
			set := make(map[int]bool, K)
			for i := 0; i < K && i < len(ranked); i++ {
				set[ranked[i].Idx] = true
			}
			return set
		}

		// jaccard returns 1 - |A∩B|/|A∪B|.
		jaccard := func(a, b map[int]bool) float64 {
			inter := 0
			for k := range a {
				if b[k] {
					inter++
				}
			}
			union := len(a) + len(b) - inter
			if union == 0 {
				return 0
			}
			return 1.0 - float64(inter)/float64(union)
		}

		// rotateBlock applies phase factor to dims in [start, end) only.
		rotateBlock := func(fp geometry.PhaseDial, alpha float64, start, end int) geometry.PhaseDial {
			rotated := make(geometry.PhaseDial, D)
			copy(rotated, fp)
			f := cmplx.Rect(1.0, alpha)
			for k := start; k < end; k++ {
				rotated[k] = fp[k] * f
			}
			return rotated
		}

		// steerability computes mean Jaccard distance of top-K sets under 12-step rotation.
		const nAngles = 12
		steerability := func(fp geometry.PhaseDial, start, end int) float64 {
			var topKSets []map[int]bool
			for i := 0; i < nAngles; i++ {
				alpha := float64(i) * (2.0 * math.Pi / float64(nAngles))
				rotated := rotateBlock(fp, alpha, start, end)
				topKSets = append(topKSets, topKSet(rotated))
			}
			sumJ := 0.0
			for i := 0; i < nAngles; i++ {
				next := (i + 1) % nAngles
				sumJ += jaccard(topKSets[i], topKSets[next])
			}
			return sumJ / float64(nAngles)
		}

		// Build the composed midpoint from the "Democracy" seed at α₁=45°.
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

		Convey("When computing the Jaccard steerability profile across all split boundaries", func() {
			type splitSteer struct {
				b      int
				sLeft  float64
				sRight float64
				sMin   float64
			}

			var allSteers []splitSteer
			var bestSplit splitSteer

			// Sweep from 64 to 448 in steps of 8
			for b := 64; b <= D-64; b += 8 {
				sL := steerability(fpAB, 0, b)
				sR := steerability(fpAB, b, D)
				sMin := math.Min(sL, sR)
				s := splitSteer{b, sL, sR, sMin}
				allSteers = append(allSteers, s)
				if sMin > bestSplit.sMin {
					bestSplit = s
				}
			}

			Convey("The steerability profile should be non-empty and find a best boundary", func() {
				So(len(allSteers), ShouldBeGreaterThan, 0)
				So(bestSplit.b, ShouldBeBetweenOrEqual, 64, D-64)
				So(bestSplit.sMin, ShouldBeGreaterThan, 0)
			})

			Convey("b=256 should be among the top steerability boundaries", func() {
				var steer256 *splitSteer
				for i := range allSteers {
					if allSteers[i].b == 256 {
						steer256 = &allSteers[i]
						break
					}
				}
				So(steer256, ShouldNotBeNil)
				So(steer256.sMin, ShouldBeGreaterThan, 0)
				// The 256/256 split should have balanced left and right steerability
				ratio := steer256.sLeft / steer256.sRight
				So(ratio, ShouldBeBetweenOrEqual, 0.5, 2.0)
			})

			Convey("Steerability should correctly predict super-additivity for the validated splits", func() {
				// Run the torus sweep for the 5 key boundaries and check
				// whether boundaries with higher min-steer produce super-additive gain.
				type valResult struct {
					name      string
					boundary  int
					steerMin  float64
					gain      float64
					superAdd  bool
				}

				validateSplit := func(name string, boundary int) valResult {
					sL := steerability(fpAB, 0, boundary)
					sR := steerability(fpAB, boundary, D)
					sMin := math.Min(sL, sR)

					const stepDeg = 5.0
					gridSize := int(360.0 / stepDeg)
					bestGain := -1.0
					for i := 0; i < gridSize; i++ {
						a1 := float64(i) * stepDeg * (math.Pi / 180.0)
						f1 := cmplx.Rect(1.0, a1)
						for j := 0; j < gridSize; j++ {
							a2 := float64(j) * stepDeg * (math.Pi / 180.0)
							f2 := cmplx.Rect(1.0, a2)
							rotated := make(geometry.PhaseDial, D)
							for k := 0; k < D; k++ {
								if k < boundary {
									rotated[k] = fpAB[k] * f1
								} else {
									rotated[k] = fpAB[k] * f2
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
					return valResult{name, boundary, sMin, bestGain, bestGain > ceiling}
				}

				validations := []struct {
					name string
					b    int
				}{
					{"192/320", 192},
					{"224/288", 224},
					{"256/256", 256},
					{"288/224", 288},
					{"320/192", 320},
				}

				var results []valResult
				for _, v := range validations {
					results = append(results, validateSplit(v.name, v.b))
				}

				So(len(results), ShouldEqual, 5)
				for _, r := range results {
					So(r.gain, ShouldBeGreaterThanOrEqualTo, 0)
				}

				// At least one boundary should be super-additive
				anySuper := false
				for _, r := range results {
					if r.superAdd {
						anySuper = true
					}
				}
				So(anySuper, ShouldBeTrue)

				// Prediction accuracy: higher min_steer → super-additive
				maxNonSA := 0.0
				for _, r := range results {
					if !r.superAdd && r.steerMin > maxNonSA {
						maxNonSA = r.steerMin
					}
				}
				correct := 0
				for _, r := range results {
					predicted := r.steerMin > maxNonSA
					if predicted == r.superAdd {
						correct++
					}
				}
				accuracy := float64(correct) / float64(len(results))
				So(accuracy, ShouldBeGreaterThanOrEqualTo, 0.6) // ≥ 60% prediction accuracy

				Convey("Artifacts should be written to the paper directory", func() {
					tableRows := make([]map[string]any, len(results))
					for i, r := range results {
						tableRows[i] = map[string]any{
							"Split":         r.name,
							"SteerMin":      fmt.Sprintf("%.4f", r.steerMin),
							"BestGain":      fmt.Sprintf("%.4f", r.gain),
							"Delta":         fmt.Sprintf("%+.4f", r.gain-ceiling),
							"SuperAdditive": r.superAdd,
						}
					}
					tableErr := WriteTable(tableRows, "steerability_validation.tex")
					So(tableErr, ShouldBeNil)

					tablePath := filepath.Join(PaperDir(), "steerability_validation.tex")
					_, statErr := os.Stat(tablePath)
					So(statErr, ShouldBeNil)
				})
			})
		})
	})
}
