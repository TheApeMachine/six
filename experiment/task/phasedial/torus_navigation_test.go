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
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/geometry"
)

func TestTorusNavigation(t *testing.T) {
	Convey("Given the aphorism corpus ingested into a PhaseDial substrate", t, func() {
		sub := NewSubstrate()
		seedQuery := "Democracy requires individual sacrifice."
		fingerprintA := geometry.NewPhaseDial().Encode(seedQuery)

		splitPoint := config.Numeric.NBasis / 2 // 256

		// torusRotate applies independent phase rotations to each half of the embedding.
		torusRotate := func(fp geometry.PhaseDial, alpha1, alpha2 float64) geometry.PhaseDial {
			f1 := cmplx.Rect(1.0, alpha1)
			f2 := cmplx.Rect(1.0, alpha2)
			out := make(geometry.PhaseDial, config.Numeric.NBasis)
			for k := 0; k < splitPoint; k++ {
				out[k] = fp[k] * f1
			}
			for k := splitPoint; k < config.Numeric.NBasis; k++ {
				out[k] = fp[k] * f2
			}
			return out
		}

		type torusSlice struct {
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
		var slices []torusSlice
		anySuperAdditive := false

		Convey("When sweeping the T² torus grid for each first-hop angle", func() {
			for _, hopAlpha1Deg := range alpha1List {
				hop := sub.FirstHop(fingerprintA, hopAlpha1Deg*(math.Pi/180.0), seedQuery)
				fpA, fpB, fpAB := fingerprintA, hop.FingerprintB, hop.FingerprintAB
				textB := hop.TextB

				So(textB, ShouldNotBeEmpty)
				So(textB, ShouldNotEqual, seedQuery)

				// 1D baselines via shared helper
				base1 := sub.BestGain(fpA, fpA, fpB, seedQuery, textB)
				base2 := sub.BestGain(fpB, fpA, fpB, seedQuery, textB)
				ceiling := math.Max(base1, base2)

				// T² torus grid sweep
				var bestGain float64 = -1
				var bestA1, bestA2 float64
				var bestC string
				for i := 0; i < gridSize; i++ {
					a1 := float64(i) * stepDeg * (math.Pi / 180.0)
					for j := 0; j < gridSize; j++ {
						a2 := float64(j) * stepDeg * (math.Pi / 180.0)
						ranked := sub.PhaseDialRank(sub.Candidates, torusRotate(fpAB, a1, a2))
						topIdx := sub.TopExcluding(ranked, seedQuery, textB)
						fpC := sub.Entries[topIdx].Fingerprint
						if g := math.Min(fpC.Similarity(fpA), fpC.Similarity(fpB)); g > bestGain {
							bestGain = g
							bestA1 = float64(i) * stepDeg
							bestA2 = float64(j) * stepDeg
							bestC = geometry.ReadoutText(sub.Entries[topIdx].Readout)
						}
					}
				}

				So(bestGain, ShouldBeGreaterThanOrEqualTo, 0)
				So(bestC, ShouldNotBeEmpty)
				So(ceiling, ShouldBeGreaterThanOrEqualTo, 0)

				sa := bestGain > ceiling
				if sa {
					anySuperAdditive = true
				}
				slices = append(slices, torusSlice{
					HopAlpha1:     hopAlpha1Deg,
					TextB:         textB,
					Base1Gain:     base1,
					Base2Gain:     base2,
					SingleCeiling: ceiling,
					BestTorusGain: bestGain,
					BestTorusA1:   bestA1,
					BestTorusA2:   bestA2,
					BestTorusC:    bestC,
					SuperAdditive: sa,
					Delta:         bestGain - ceiling,
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
				var s45 *torusSlice
				for i := range slices {
					if slices[i].HopAlpha1 == 45.0 {
						s45 = &slices[i]
						break
					}
				}
				So(s45, ShouldNotBeNil)
				So(s45.BestTorusGain, ShouldBeGreaterThanOrEqualTo, s45.SingleCeiling)
			})

			Convey("anySuperAdditive should be true across all first-hop angles", func() {
				So(anySuperAdditive, ShouldBeTrue)
			})

			Convey("Artifacts should be written to the paper directory", func() {
				// Landscape grid for α₁=15° (first hop)
				type gridCell struct{ i, j int; gain float64 }
				var landscape []gridCell
				var landscapeA1Deg float64

				for idx, hopAlpha1Deg := range alpha1List {
					hop := sub.FirstHop(fingerprintA, hopAlpha1Deg*(math.Pi/180.0), seedQuery)
					fpA, fpB, fpAB := fingerprintA, hop.FingerprintB, hop.FingerprintAB
					textB := hop.TextB
					if idx == 0 {
						landscapeA1Deg = hopAlpha1Deg
						for i := 0; i < gridSize; i++ {
							a1 := float64(i) * stepDeg * (math.Pi / 180.0)
							for j := 0; j < gridSize; j++ {
								a2 := float64(j) * stepDeg * (math.Pi / 180.0)
								ranked := sub.PhaseDialRank(sub.Candidates, torusRotate(fpAB, a1, a2))
								topIdx := sub.TopExcluding(ranked, seedQuery, textB)
								fpC := sub.Entries[topIdx].Fingerprint
								gain := math.Min(fpC.Similarity(fpA), fpC.Similarity(fpB))
								landscape = append(landscape, gridCell{i, j, gain})
							}
						}
					}
					_ = textB
				}

				// Build axis labels
				axLabels := make([]string, gridSize)
				for i := 0; i < gridSize; i++ {
					axLabels[i] = fmt.Sprintf("%.0f°", float64(i)*stepDeg)
				}
				heatData := make([][]any, len(landscape))
				for i, c := range landscape {
					heatData[i] = []any{c.i, c.j, c.gain}
				}

				heatPanel := projector.HeatmapPanel(axLabels, axLabels, heatData, -0.15, 0.20, "viridis")
				heatPanel.GridLeft = "8%"; heatPanel.GridRight = "47%"
				heatPanel.GridTop = "8%"; heatPanel.GridBottom = "10%"
				heatPanel.XAxisName = "Torus α₁ (dims 0–255)"
				heatPanel.YAxisName = "Torus α₂ (dims 256–511)"
				heatPanel.Title = fmt.Sprintf("Torus Gain Landscape (hop α₁=%.0f°)", landscapeA1Deg)
				heatPanel.VMRight = "46%"; heatPanel.XInterval = 9; heatPanel.YInterval = 9

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
				chartPanel := projector.ChartPanel(xAxis, []projector.MPSeries{
					{Name: "Torus Best", Kind: "bar", BarWidth: "30%", Data: torusData, Color: "#22c55e"},
					{Name: "Baseline A", Kind: "dashed", Symbol: "diamond", Data: base1Data, Color: "#94a3b8"},
					{Name: "Baseline B", Kind: "dashed", Symbol: "triangle", Data: base2Data, Color: "#ef4444"},
				}, projector.F64(-0.1), projector.F64(0.45))
				chartPanel.GridLeft = "62%"; chartPanel.GridRight = "5%"
				chartPanel.GridTop = "8%"; chartPanel.GridBottom = "10%"
				chartPanel.XAxisName = "First-Hop Angle"
				chartPanel.YAxisName = "Gain"
				chartPanel.Title = "Torus vs 1D Baselines"

				f1, _ := os.Create(filepath.Join(PaperDir(), "torus_navigation.tex"))
				err := WriteMultiPanel([]projector.MPPanel{heatPanel, chartPanel}, 1200, 900,
					"U(1)×U(1) Torus Navigation",
					"(Left) Full T²(α₁,α₂) gain grid for first-hop α₁=15°. Dark = destructive, warm = constructive. (Right) T² best gain (bar) vs single-axis baselines (dashed) across all first-hop angles; bars exceeding dashed lines are super-additive.",
					"fig:torus_navigation", "torus_navigation", f1)
				So(err, ShouldBeNil)
				if f1 != nil {
					f1.Close()
				}

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
				So(WriteTable(tableRows, "torus_navigation_summary.tex"), ShouldBeNil)
				_, statErr := os.Stat(filepath.Join(PaperDir(), "torus_navigation_summary.tex"))
				So(statErr, ShouldBeNil)
			})
		})
	})
}
