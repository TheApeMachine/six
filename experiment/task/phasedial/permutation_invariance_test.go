package phasedial

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/geometry"
)

func TestPermutationInvariance(t *testing.T) {
	Convey("Given the aphorism corpus and a fixed seed", t, func() {
		seed := int64(42)
		aphorisms := Aphorisms

		Convey("When shuffling and ingesting into a PhaseDial substrate, then running geodesic scan", func() {
			substrate := geometry.NewHybridSubstrate()
			var seedFP geometry.PhaseDial
			var filter data.Chord

			shuffled := append([]string{}, aphorisms...)
			rng := rand.New(rand.NewSource(seed))
			rng.Shuffle(len(shuffled), func(i, j int) {
				shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
			})

			for i, text := range shuffled {
				dial := geometry.NewPhaseDial().Encode(text)
				substrate.Add(filter, dial, []byte(fmt.Sprintf("%d: %s", i, text)))
				if text == "Democracy requires individual sacrifice." {
					seedFP = append(geometry.PhaseDial{}, dial...)
				}
			}

			results := substrate.GeodesicScan(seedFP, 72, 5.0)

			Convey("The geodesic scan should produce 73 steps (0° to 360° in 5° steps)", func() {
				So(len(results), ShouldEqual, 73)
			})
			Convey("Each step should have a valid margin", func() {
				for _, r := range results {
					So(r.Margin, ShouldBeGreaterThanOrEqualTo, 0)
				}
			})
			Convey("Best readout should resolve to a corpus item", func() {
				for _, r := range results {
					text := geometry.ReadoutText(r.BestReadout)
					So(text, ShouldNotBeEmpty)
				}
			})
		Convey("Artifacts should be written to paper directory", func() {
				// Phase sweep parameters — 1° resolution for smooth figures
				const sweepSteps = 360
				const sweepDeg = 1.0

				// Build per-item fingerprints and define named items for the trace chart
				type namedFP struct {
					Name string
					FP   geometry.PhaseDial
					Idx  int
				}
				named := []namedFP{
					{"Seed (Democracy)", seedFP, -1},
					{"Antipode (Nature)", geometry.NewPhaseDial().Encode("Nature does not hurry, yet everything is accomplished."), -1},
					{"Complement (Authority)", geometry.NewPhaseDial().Encode("Authoritarianism emerges from collective self-interest."), -1},
				}

				// Build candidate index list from substrate
				cands := make([]int, len(substrate.Entries))
				for i := range cands {
					cands[i] = i
				}

				// Score heatmap: rows = corpus items (shortened), cols = phase 0°..359°
				// Each cell is the PhaseDialRank score of that item at that rotation
				corpusFPs := make([]geometry.PhaseDial, len(substrate.Entries))
				corpusLabels := make([]string, len(substrate.Entries))
				for i, e := range substrate.Entries {
					corpusFPs[i] = e.Fingerprint
					label := geometry.ReadoutText(e.Readout)
					if len(label) > 30 {
						label = label[:30] + "…"
					}
					corpusLabels[i] = label
					// Find substrate index for named items
					full := geometry.ReadoutText(e.Readout)
					for ni := range named {
						if full == "Democracy requires individual sacrifice." && named[ni].Name == "Seed (Democracy)" {
							named[ni].Idx = i
						}
						if full == "Nature does not hurry, yet everything is accomplished." && named[ni].Name == "Antipode (Nature)" {
							named[ni].Idx = i
						}
						if full == "Authoritarianism emerges from collective self-interest." && named[ni].Name == "Complement (Authority)" {
							named[ni].Idx = i
						}
					}
				}

				xLabels := make([]string, sweepSteps)
				for s := 0; s < sweepSteps; s++ {
					xLabels[s] = fmt.Sprintf("%.0f°", float64(s)*sweepDeg)
				}

				// Build score matrix and novelty series in one sweep
				scoreMatrix := make([][]float64, len(substrate.Entries))
				for i := range scoreMatrix {
					scoreMatrix[i] = make([]float64, sweepSteps)
				}
				novelty := make([]float64, sweepSteps)
				prevTopIdx := -1

				for s := 0; s < sweepSteps; s++ {
					alpha := float64(s) * sweepDeg * (math.Pi / 180.0)
					rotated := seedFP.Rotate(alpha)
					ranked := substrate.PhaseDialRank(cands, rotated)

					// Fill row scores
					scoreMap := make(map[int]float64, len(ranked))
					for _, r := range ranked {
						scoreMap[r.Idx] = r.Score
					}
					for i := range substrate.Entries {
						scoreMatrix[i][s] = scoreMap[i]
					}

					// Novelty: 1 if top match changed, else 0 (smoothed)
					topIdx := ranked[0].Idx
					if topIdx != prevTopIdx {
						novelty[s] = 1.0
					}
					prevTopIdx = topIdx
				}

				// Smooth novelty with a 5-step moving average
				smoothed := make([]float64, sweepSteps)
				for s := 0; s < sweepSteps; s++ {
					sum := 0.0
					count := 0
					for w := -4; w <= 4; w++ {
						idx := s + w
						if idx >= 0 && idx < sweepSteps {
							sum += novelty[idx]
							count++
						}
					}
					smoothed[s] = sum / float64(count)
				}

				// Score heatmap data (corpus items × phase rotation)
				heatRows := make([][]any, 0, len(substrate.Entries)*sweepSteps)
				for rowIdx, row := range scoreMatrix {
					for colIdx, val := range row {
						heatRows = append(heatRows, []any{colIdx, rowIdx, val})
					}
				}

				// Named-item cosine trajectories
				traceMPSeries := make([]projector.MPSeries, 0, len(named))
				traceColors := []string{"#334155", "#ef4444", "#eab308"}
				for ni, nm := range named {
					traceData := make([]float64, sweepSteps)
					for s := 0; s < sweepSteps; s++ {
						alpha := float64(s) * sweepDeg * (math.Pi / 180.0)
						rotated := seedFP.Rotate(alpha)
						traceData[s] = nm.FP.Similarity(rotated)
					}
					color := traceColors[ni%len(traceColors)]
					traceMPSeries = append(traceMPSeries, projector.MPSeries{
						Name: nm.Name, Kind: "line", Data: traceData, Color: color,
					})
				}

				// 3-panel MultiPanel: heatmap (top-left) + novelty (bottom-left) + traces (right)
				heatPanel := projector.HeatmapPanel(xLabels, corpusLabels, heatRows, -1.0, 1.0, "viridis")
				heatPanel.GridLeft = "16%"
				heatPanel.GridRight = "36%"
				heatPanel.GridTop = "5%"
				heatPanel.GridBottom = "33%"
				heatPanel.XAxisName = "Phase Rotation (Degrees)"
				heatPanel.YAxisName = ""
				heatPanel.Title = "Geodesic Scan (Permutation-Shuffled Corpus)"
				heatPanel.VMRight = "34%"
				heatPanel.XInterval = 25
				heatPanel.YInterval = 1

				noveltyPanel := projector.ChartPanel(
					xLabels,
					[]projector.MPSeries{
						{Name: "Novelty Resonance", Kind: "line", Data: smoothed, Color: "#38bdf8"},
					},
					projector.F64(0.0), projector.F64(1.0),
				)
				noveltyPanel.GridLeft = "16%"
				noveltyPanel.GridRight = "36%"
				noveltyPanel.GridTop = "73%"
				noveltyPanel.GridBottom = "5%"
				noveltyPanel.XAxisName = ""
				noveltyPanel.YAxisName = "Novelty\nResonance"
				noveltyPanel.XInterval = 25

				tracesPanel := projector.ChartPanel(
					xLabels,
					traceMPSeries,
					projector.F64(-1.0), projector.F64(1.0),
				)
				tracesPanel.GridLeft = "70%"
				tracesPanel.GridRight = "3%"
				tracesPanel.GridTop = "5%"
				tracesPanel.GridBottom = "33%"
				tracesPanel.XAxisName = "Phase"
				tracesPanel.YAxisName = ""
				tracesPanel.XInterval = 40

				fm, _ := os.Create(filepath.Join(PaperDir(), "permutation_metrics_chart.tex"))
				mpErr := WriteMultiPanel(
					[]projector.MPPanel{heatPanel, noveltyPanel, tracesPanel},
					1200, 800,
					"PhaseDial Geodesic Scan Metrics",
					"(Left, Top) Semantic geodesic matrix mapping geometric score across corpus as query phase rotates 360°. (Left, Bottom) Novelty resonance showing where top match changes. (Right) Spectral complementarity: cosine similarity of the rotating seed to named items, showing pure U(1) sinusoidal structure.",
					"fig:permutation_metrics", "permutation_metrics_chart", fm)
				So(mpErr, ShouldBeNil)
				if fm != nil {
					fm.Close()
				}

				// Minimal metrics table (original)
				tableData := []map[string]any{
					{"Step": 0, "Phase": "0°", "Margin": results[0].Margin, "Entropy": results[0].Entropy},
					{"Step": 36, "Phase": "180°", "Margin": results[36].Margin, "Entropy": results[36].Entropy},
					{"Step": 72, "Phase": "360°", "Margin": results[72].Margin, "Entropy": results[72].Entropy},
				}
				tableErr := WriteTable(tableData, "permutation_metrics.tex")
				So(tableErr, ShouldBeNil)

				tablePath := filepath.Join(PaperDir(), "permutation_metrics.tex")
				_, statErr := os.Stat(tablePath)
				So(statErr, ShouldBeNil)
			})

		})
	})
}
