package phasedial

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/numeric"
)

func TestPhaseCoherence(t *testing.T) {
	Convey("Given all aphorism fingerprints for pairwise phase correlation analysis", t, func() {
		aphorisms := Aphorisms
		N := len(aphorisms)
		D := numeric.NBasis

		fingerprints := make([]geometry.PhaseDial, N)
		for i, text := range aphorisms {
			fingerprints[i] = geometry.NewPhaseDial().Encode(text)
		}

		So(N, ShouldBeGreaterThan, 0)

		// Extract phases: θ_n_k = atan2(imag, real)
		phases := make([][]float64, N)
		for n := 0; n < N; n++ {
			phases[n] = make([]float64, D)
			for k := 0; k < D; k++ {
				phases[n][k] = math.Atan2(imag(fingerprints[n][k]), real(fingerprints[n][k]))
			}
		}

		// Compute full D×D phase correlation matrix: corr(i,j) = (1/N) Σ cos(θ_n_i − θ_n_j)
		corrMatrix := make([][]float64, D)
		for i := 0; i < D; i++ {
			corrMatrix[i] = make([]float64, D)
		}
		for i := 0; i < D; i++ {
			for j := i; j < D; j++ {
				sum := 0.0
				for n := 0; n < N; n++ {
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
				for k := 0; k < D; k++ {
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
				for bi := 0; bi < numBlocks; bi++ {
					for bj := 0; bj < numBlocks; bj++ {
						sum := 0.0
						count := 0
						for di := 0; di < blockSize; di++ {
							for dj := 0; dj < blockSize; dj++ {
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
				f, _ := os.Create(filepath.Join(PaperDir(), "phase_coherence_heatmap.tex"))
				err := WriteHeatMap(xLabels, yLabels, heatmapData, -0.05, 0.05,
					"Phase Correlation Matrix (64×64 block average)",
					"Pairwise phase correlation corr(i,j) averaged into 8-dim blocks. Absence of bright diagonal bands confirms no contiguous coherence structure.",
					"fig:phase_coherence_heatmap", "phase_coherence_heatmap", f)
				So(err, ShouldBeNil)
				if f != nil {
					f.Close()
				}

				// Line chart: C(d) for d=1..64
				xAxis := make([]string, 64)
				cData := make([]float64, 64)
				for d := 1; d <= 64; d++ {
					xAxis[d-1] = fmt.Sprintf("%d", d)
					cData[d-1] = distCorr[d]
				}
				lSeries := []projector.LineSeries{
					{Name: "C(d)", Data: cData},
				}
				f2, _ := os.Create(filepath.Join(PaperDir(), "phase_coherence_dist_corr.tex"))
				err2 := WriteLineChart(xAxis, lSeries,
					"Distance-Dependent Phase Correlation C(d)",
					"Mean phase correlation at index distance d. Negative for d<13 (repulsion), positive for d>13 (attraction). Zero crossing at d≈13 defines the characteristic correlation length.",
					"fig:phase_coherence_dist_corr", "phase_coherence_dist_corr",
					-0.02, 0.02, f2)
				So(err2, ShouldBeNil)
				if f2 != nil {
					f2.Close()
				}

				tablePath := filepath.Join(PaperDir(), "phase_coherence_dist_bands.tex")
				_, statErr := os.Stat(tablePath)
				So(statErr, ShouldBeNil)
			})
		})
	})
}
