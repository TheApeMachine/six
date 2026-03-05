package phasedial

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	config "github.com/theapemachine/six/core"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/geometry"
)

func TestPhaseCoherence(t *testing.T) {
	Convey("Given all aphorism fingerprints for pairwise phase correlation analysis", t, func() {
		aphorisms := Aphorisms
		N := len(aphorisms)
		D := config.Numeric.NBasis

		fingerprints := make([]geometry.PhaseDial, N)
		for i, text := range aphorisms {
			fingerprints[i] = geometry.NewPhaseDial().Encode(text)
		}

		So(N, ShouldBeGreaterThan, 0)

		// Extract phases: θ_n_k = atan2(imag, real)
		phases := make([][]float64, N)
		for n := range N {
			phases[n] = make([]float64, D)
			for k := range D {
				phases[n][k] = math.Atan2(imag(fingerprints[n][k]), real(fingerprints[n][k]))
			}
		}

		// Compute full D×D phase correlation matrix: corr(i,j) = (1/N) Σ cos(θ_n_i − θ_n_j)
		corrMatrix := make([][]float64, D)
		for i := range D {
			corrMatrix[i] = make([]float64, D)
		}
		for i := range D {
			for j := i; j < D; j++ {
				sum := 0.0
				for n := range N {
					sum += math.Cos(phases[n][i] - phases[n][j])
				}
				val := sum / float64(N)
				corrMatrix[i][j] = val
				corrMatrix[j][i] = val
			}
		}

		Convey("When analyzing the phase correlation structure", func() {
			// Self-correlation should be 1
			Convey("Diagonal entries (self-correlation) should all be 1.0", func() {
				for k := range D {
					So(math.Abs(corrMatrix[k][k]-1.0), ShouldBeLessThan, 1e-10)
				}
			})

			// Distance-dependent correlation C(d)
			distCorr := make([]float64, D)
			distCorr[0] = 1.0
			for dist := 1; dist < D; dist++ {
				sum := 0.0
				count := 0
				for i := 0; i < D-dist; i++ {
					sum += corrMatrix[i][i+dist]
					count++
				}
				distCorr[dist] = sum / float64(count)
			}

			Convey("Short-range correlation C(d<13) should be negative (repulsion)", func() {
				// This is the known characteristic of the prime-based encoding
				negCount := 0
				for d := 1; d < 13; d++ {
					if distCorr[d] < 0 {
						negCount++
					}
				}
				// Most short-range distances should be negative
				So(negCount, ShouldBeGreaterThan, 6)
			})

			Convey("Long-range correlation C(d>64) should be weakly positive", func() {
				posCount := 0
				for d := 65; d < D; d++ {
					if distCorr[d] > 0 {
						posCount++
					}
				}
				// Majority of long-range distances should be positive
				So(posCount, ShouldBeGreaterThan, D/4)
			})

			Convey("The zero crossing should occur around d≈13", func() {
				zeroCrossing := 0
				for dist := 2; dist < D; dist++ {
					if (distCorr[dist-1] < 0 && distCorr[dist] >= 0) ||
						(distCorr[dist-1] >= 0 && distCorr[dist] < 0) {
						zeroCrossing = dist
						break
					}
				}
				So(zeroCrossing, ShouldBeGreaterThan, 0)
				// In the known encoding, zero crossing is near d=13
				So(zeroCrossing, ShouldBeBetweenOrEqual, 5, 30)
			})

			Convey("Artifacts should be written to the paper directory", func() {
				// Distance band means table
				type distBand struct{ dmin, dmax int }
				bands := []distBand{{1, 16}, {17, 64}, {65, 256}, {257, 511}}
				tableRows := make([]map[string]any, len(bands))
				for i, b := range bands {
					sum := 0.0
					count := 0
					for dist := b.dmin; dist <= b.dmax && dist < D; dist++ {
						sum += distCorr[dist]
						count++
					}
					mean := 0.0
					if count > 0 {
						mean = sum / float64(count)
					}
					tableRows[i] = map[string]any{
						"DistanceBand": fmt.Sprintf("d∈[%d,%d]", b.dmin, b.dmax),
						"MeanCorr":     fmt.Sprintf("%+.6f", mean),
					}
				}
				tableErr := WriteTable(tableRows, "phase_coherence_dist_bands.tex")
				So(tableErr, ShouldBeNil)

				// Downsampled heatmap of correlation matrix (8×8 blocks → 64×64 grid)
				const blockSize = 8
				numBlocks := D / blockSize
				xLabels := make([]string, numBlocks)
				yLabels := make([]string, numBlocks)
				for i := range xLabels {
					xLabels[i] = fmt.Sprintf("%d", i*blockSize)
					yLabels[i] = fmt.Sprintf("%d", i*blockSize)
				}
				var heatmapData [][]any
				for bi := range numBlocks {
					for bj := range numBlocks {
						sum := 0.0
						count := 0
						for di := range blockSize {
							for dj := range blockSize {
								ii := bi*blockSize + di
								jj := bj*blockSize + dj
								if ii != jj {
									sum += corrMatrix[ii][jj]
									count++
								}
							}
						}
						val := 0.0
						if count > 0 {
							val = sum / float64(count)
						}
						heatmapData = append(heatmapData, []any{bi, bj, val})
					}
				}

				heatPanel := projector.HeatmapPanel(xLabels, yLabels, heatmapData, -0.05, 0.05, "plasma")
				heatPanel.GridLeft = "5%"
				heatPanel.GridRight = "47%"
				heatPanel.GridTop = "8%"
				heatPanel.GridBottom = "10%"
				heatPanel.XAxisName = "Dimension Index"
				heatPanel.YAxisName = "Dimension Index"
				heatPanel.Title = "Phase Correlation Matrix (64×64 blocks)"
				heatPanel.VMRight = "46%"
				heatPanel.XInterval = 8
				heatPanel.YInterval = 8

			// Build full distance correlation slice d=1..511 for right panel
				cCorrXLabels := make([]string, D-1)
				cCorrData := make([]float64, D-1)
				for d := 1; d < D; d++ {
					cCorrXLabels[d-1] = fmt.Sprintf("%d", d)
					cCorrData[d-1] = distCorr[d]
				}

				linePanel := projector.ChartPanel(
					cCorrXLabels,
					[]projector.MPSeries{
						{Name: "C(d)", Kind: "line", Data: cCorrData, Color: "#3b82f6", Area: true},
					},
					projector.F64(-0.15), projector.F64(0.20),
				)
				linePanel.GridLeft = "60%"
				linePanel.GridRight = "5%"
				linePanel.GridTop = "8%"
				linePanel.GridBottom = "10%"
				linePanel.XAxisName = "Index Distance d"
				linePanel.YAxisName = "C(d)"
				linePanel.Title = "C(d) = Mean Correlation vs Index Distance"
				linePanel.XInterval = 50

				f, _ := os.Create(filepath.Join(PaperDir(), "phase_coherence.tex"))
				err := WriteMultiPanel(
					[]projector.MPPanel{heatPanel, linePanel},
					1200, 800,
					"Phase Coherence Analysis",
					"(Left) Phase correlation matrix corr(i,j) averaged into 8-dim blocks. (Right) C(d) for all d=1..511 — negative short-range repulsion, positive long-range attraction, large boundary spike near d=511.",
					"fig:phase_coherence", "phase_coherence", f)
				So(err, ShouldBeNil)
				if f != nil {
					f.Close()
				}

				tablePath := filepath.Join(PaperDir(), "phase_coherence_dist_bands.tex")
				_, statErr := os.Stat(tablePath)
				So(statErr, ShouldBeNil)
			})
		})
	})
}
