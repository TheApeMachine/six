package phasedial

import (
	"fmt"
	"math"
	"math/cmplx"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	config "github.com/theapemachine/six/core"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/geometry"
)

// localContiguousSplit builds a DimMap assigning dims to contiguous subspaces.
// boundaries are end-indices: e.g. [256, 512] → sub0=[0,256), sub1=[256,512).
func localContiguousSplit(numAxes int, boundaries []int) []int {
	dimMap := make([]int, config.Numeric.NBasis)
	sub := 0
	for k := 0; k < config.Numeric.NBasis; k++ {
		if sub < numAxes-1 && k >= boundaries[sub] {
			sub++
		}
		dimMap[k] = sub
	}
	return dimMap
}

// localRandomSplit builds a DimMap via a deterministic random permutation.
func localRandomSplit(numAxes, dimsPerAxis int, seed int64) []int {
	rng := rand.New(rand.NewSource(seed))
	perm := rng.Perm(config.Numeric.NBasis)
	dimMap := make([]int, config.Numeric.NBasis)
	for i, dim := range perm {
		sub := i / dimsPerAxis
		if sub >= numAxes {
			sub = numAxes - 1
		}
		dimMap[dim] = sub
	}
	return dimMap
}

// localEnergySplit sorts dims by |A[k]|²−|B[k]|² and splits at the median.
func localEnergySplit(fpA, fpB geometry.PhaseDial) []int {
	type dimE struct {
		k    int
		diff float64
	}
	dims := make([]dimE, config.Numeric.NBasis)
	for k := 0; k < config.Numeric.NBasis; k++ {
		eA := real(fpA[k])*real(fpA[k]) + imag(fpA[k])*imag(fpA[k])
		eB := real(fpB[k])*real(fpB[k]) + imag(fpB[k])*imag(fpB[k])
		dims[k] = dimE{k: k, diff: eA - eB}
	}
	sort.Slice(dims, func(i, j int) bool { return dims[i].diff < dims[j].diff })
	dimMap := make([]int, config.Numeric.NBasis)
	half := config.Numeric.NBasis / 2
	for i, d := range dims {
		if i < half {
			dimMap[d.k] = 0
		} else {
			dimMap[d.k] = 1
		}
	}
	return dimMap
}

func TestTorusGeneralization(t *testing.T) {
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

		// generalRotate applies per-subspace angles according to a dimMap.
		generalRotate := func(fp geometry.PhaseDial, numAxes int, dimMap []int, angles []float64) geometry.PhaseDial {
			factors := make([]complex128, numAxes)
			for i, a := range angles {
				factors[i] = cmplx.Rect(1.0, a)
			}
			rotated := make(geometry.PhaseDial, config.Numeric.NBasis)
			for k := 0; k < config.Numeric.NBasis; k++ {
				rotated[k] = fp[k] * factors[dimMap[k]]
			}
			return rotated
		}

		// compute1DCeiling sweeps 1D phase from both A and B and returns max gain.
		compute1DCeiling := func(fpA, fpB geometry.PhaseDial, excludeA, excludeB string) float64 {
			ceiling := -1.0
			for s := 0; s < 360; s++ {
				alpha := float64(s) * (math.Pi / 180.0)
				for _, anchor := range []geometry.PhaseDial{fpA, fpB} {
					rot := anchor.Rotate(alpha)
					rnk := substrate.PhaseDialRank(candidates, rot)
					topIdx := rnk[0].Idx
					for _, r := range rnk {
						ct := geometry.ReadoutText(substrate.Entries[r.Idx].Readout)
						if ct != excludeA && ct != excludeB {
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
			return ceiling
		}

		type splitConfig struct {
			name    string
			numAxes int
			dimMap  []int
			stepDeg float64
		}

		type splitResult struct {
			SplitName     string
			BestGain      float64
			SingleCeiling float64
			Delta         float64
			SuperAdditive bool
		}
		type seedResult struct {
			SeedQuery string
			TextB     string
			Splits    []splitResult
		}

		seedQueries := []string{
			"Democracy requires individual sacrifice.",
			"Knowledge is power.",
			"Nature does not hurry, yet everything is accomplished.",
		}

		var allSeeds []seedResult
		anySuperAdditive := false

		Convey("When sweeping multiple split configurations across three seed queries", func() {
			for _, seedQuery := range seedQueries {
				fingerprintA := geometry.NewPhaseDial().Encode(seedQuery)

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

				ceiling := compute1DCeiling(fingerprintA, fingerprintB, seedQuery, textB)
				So(ceiling, ShouldBeGreaterThanOrEqualTo, 0)

				configs := []splitConfig{
					{"T²-256/256", 2, localContiguousSplit(2, []int{256, 512}), 5.0},
					{"T²-128/384", 2, localContiguousSplit(2, []int{128, 512}), 5.0},
					{"T²-384/128", 2, localContiguousSplit(2, []int{384, 512}), 5.0},
					{"T²-random", 2, localRandomSplit(2, 256, 42), 5.0},
					{"T²-energy", 2, localEnergySplit(fingerprintA, fingerprintB), 5.0},
				}

				sr := seedResult{SeedQuery: seedQuery, TextB: textB}

				for _, cfg := range configs {
					stepRad := cfg.stepDeg * (math.Pi / 180.0)
					gridSize := int(360.0 / cfg.stepDeg)
					totalPoints := gridSize
					for i := 1; i < cfg.numAxes; i++ {
						totalPoints *= gridSize
					}

					var bestGain float64 = -1.0

					for flat := 0; flat < totalPoints; flat++ {
						angles := make([]float64, cfg.numAxes)
						remainder := flat
						for axis := cfg.numAxes - 1; axis >= 0; axis-- {
							idx := remainder % gridSize
							remainder /= gridSize
							angles[axis] = float64(idx) * stepRad
						}

						rotatedAB := generalRotate(fingerprintAB, cfg.numAxes, cfg.dimMap, angles)
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
						gain := math.Min(fpC.Similarity(fingerprintA), fpC.Similarity(fingerprintB))
						if gain > bestGain {
							bestGain = gain
						}
					}

					So(bestGain, ShouldBeGreaterThanOrEqualTo, 0)

					superAdditive := bestGain > ceiling
					if superAdditive {
						anySuperAdditive = true
					}

					sr.Splits = append(sr.Splits, splitResult{
						SplitName:     cfg.name,
						BestGain:      bestGain,
						SingleCeiling: ceiling,
						Delta:         bestGain - ceiling,
						SuperAdditive: superAdditive,
					})
				}

				allSeeds = append(allSeeds, sr)
			}

			Convey("All seeds should produce valid split results", func() {
				So(len(allSeeds), ShouldEqual, len(seedQueries))
				for _, s := range allSeeds {
					So(len(s.Splits), ShouldBeGreaterThan, 0)
					So(s.TextB, ShouldNotBeEmpty)
					for _, sp := range s.Splits {
						So(sp.BestGain, ShouldBeGreaterThanOrEqualTo, 0)
					}
				}
			})

			Convey("At least one split configuration should be super-additive across the seeds", func() {
				So(anySuperAdditive, ShouldBeTrue)
			})

			Convey("T²-random split should be consistently non-super-additive (it destroys phase texture)", func() {
				for _, s := range allSeeds {
					for _, sp := range s.Splits {
						if sp.SplitName == "T²-random" {
							// random split should not exceed the ceiling
							So(sp.Delta, ShouldBeLessThanOrEqualTo, 0.02) // small tolerance
						}
					}
				}
			})

			Convey("Artifacts should be written to the paper directory", func() {
				// Combo chart: per-seed bars + 1D ceiling dashed line (matching original figure)
				xAxis := make([]string, len(allSeeds[0].Splits))
				for i, sp := range allSeeds[0].Splits {
					xAxis[i] = sp.SplitName
				}
				// Compute mean ceiling across seeds (ceiling is per-seed, use max)
				ceilingData := make([]float64, len(allSeeds[0].Splits))
				for i := range ceilingData {
					maxCeiling := 0.0
					for _, s := range allSeeds {
						if s.Splits[i].SingleCeiling > maxCeiling {
							maxCeiling = s.Splits[i].SingleCeiling
						}
					}
					ceilingData[i] = maxCeiling
				}
				var comboSeries []projector.ComboSeries
				for _, s := range allSeeds {
					gainData := make([]float64, len(s.Splits))
					for i, sp := range s.Splits {
						gainData[i] = sp.BestGain
					}
					label := s.SeedQuery
					if len(label) > 20 {
						label = label[:20] + "…"
					}
					comboSeries = append(comboSeries, projector.ComboSeries{
						Name: label, Type: "bar", BarWidth: "12%", Data: gainData,
					})
				}
				comboSeries = append(comboSeries, projector.ComboSeries{
					Name: "1D Ceiling", Type: "dashed", Symbol: "circle", Data: ceilingData,
				})
				f, _ := os.Create(filepath.Join(PaperDir(), "torus_generalization.tex"))
				err := WriteComboChart(xAxis, comboSeries,
					"Split Configuration", "Best Gain",
					0, 0.25,
					"Torus Generalization: Gain by Split and Seed Query",
					"Best torus gain for each split configuration across three seed queries. Bars exceeding the dashed 1D ceiling demonstrate super-additive composition.",
					"fig:torus_generalization", "torus_generalization", f)
				So(err, ShouldBeNil)
				if f != nil {
					f.Close()
				}

				// Flat summary table
				var tableRows []map[string]any
				for _, s := range allSeeds {
					seed := s.SeedQuery
					if len(seed) > 30 {
						seed = seed[:30] + "…"
					}
					for _, sp := range s.Splits {
						tableRows = append(tableRows, map[string]any{
							"Seed":          seed,
							"Split":         sp.SplitName,
							"BestGain":      fmt.Sprintf("%.4f", sp.BestGain),
							"Ceiling":       fmt.Sprintf("%.4f", sp.SingleCeiling),
							"Delta":         fmt.Sprintf("%+.4f", sp.Delta),
							"SuperAdditive": sp.SuperAdditive,
						})
					}
				}
				tableErr := WriteTable(tableRows, "torus_generalization_summary.tex")
				So(tableErr, ShouldBeNil)

				tablePath := filepath.Join(PaperDir(), "torus_generalization_summary.tex")
				_, statErr := os.Stat(tablePath)
				So(statErr, ShouldBeNil)
			})
		})
	})
}
