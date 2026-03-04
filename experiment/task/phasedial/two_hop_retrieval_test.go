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
)

func TestTwoHopRetrieval(t *testing.T) {
	Convey("Given a list of aphorisms ingested into a PhaseDial substrate", t, func() {
		sub := NewSubstrate()
		seedQuery := "Democracy requires individual sacrifice."
		fingerprintA := geometry.NewPhaseDial().Encode(seedQuery)
		alpha1List := []float64{15.0, 30.0, 45.0, 60.0, 75.0}

		type TwoHopRow struct {
			Alpha1   float64
			Base1    float64
			Base2    float64
			Composed float64
			Traces   []TwoHopTrace
		}

		var (
			overallBestGain  float64 = -1
			overallBestTrace TwoHopTrace
			overallBestTraceSet []TwoHopTrace
			overallBase1Max  float64
			overallBase2Max  float64
			overallBestB     string
			rows             []TwoHopRow
		)

		Convey("When rotating A and sweeping α₂ over the composed midpoint", func() {
			for _, alpha1Deg := range alpha1List {
				hop := sub.FirstHop(fingerprintA, alpha1Deg*(math.Pi/180.0), seedQuery)
				fpA, fpB, fpAB := fingerprintA, hop.FingerprintB, hop.FingerprintAB
				textB := hop.TextB

				So(textB, ShouldNotBeEmpty)
				So(textB, ShouldNotEqual, seedQuery)
				So(fpAB.Similarity(fpA), ShouldBeBetweenOrEqual, -1.0, 1.0)
				So(fpAB.Similarity(fpB), ShouldBeBetweenOrEqual, -1.0, 1.0)

				// Sweep α₂ over the composed midpoint
				var traces []TwoHopTrace
				var bestGain float64 = -1
				var bestTrace TwoHopTrace

				for s := range 360 {
					alpha2Deg := float64(s)
					rotatedAB := fpAB.Rotate(alpha2Deg * (math.Pi / 180.0))
					ranked := sub.PhaseDialRank(sub.Candidates, rotatedAB)
					topIdx := sub.TopExcluding(ranked, seedQuery, textB)
					fpC := sub.Entries[topIdx].Fingerprint
					textC := geometry.ReadoutText(sub.Entries[topIdx].Readout)

					simCA := fpC.Similarity(fpA)
					simCB := fpC.Similarity(fpB)
					gain := math.Min(simCA, simCB)
					tr := TwoHopTrace{
						Alpha2:      alpha2Deg,
						Gain:        gain,
						SimCA:       simCA,
						SimCB:       simCB,
						MatchText:   textC,
						SimCAB:      fpC.Similarity(fpAB),
						BalancedSum: 0.5 * (simCA + simCB),
						Separation:  fpC.Similarity(fpAB) - math.Max(simCA, simCB),
					}
					traces = append(traces, tr)
					if gain > bestGain {
						bestGain, bestTrace = gain, tr
					}
				}

				So(bestGain, ShouldBeGreaterThanOrEqualTo, 0)

				// 1D baselines (shared BestGain helper)
				base1 := sub.BestGain(fpA, fpA, fpB, seedQuery, textB)
				base2 := sub.BestGain(fpB, fpA, fpB, seedQuery, textB)
				So(math.Max(base1, base2), ShouldBeGreaterThanOrEqualTo, 0)

				rows = append(rows, TwoHopRow{alpha1Deg, base1, base2, bestGain, traces})

				if bestGain > overallBestGain {
					overallBestGain = bestGain
					overallBestTrace = bestTrace
					overallBestTraceSet = traces
					overallBestB = textB
				}
				if base1 > overallBase1Max {
					overallBase1Max = base1
				}
				if base2 > overallBase2Max {
					overallBase2Max = base2
				}
			}

			So(overallBestB, ShouldNotBeEmpty)

			Convey("Artifacts should be written to paper directory", func() {
				// Composition trace line chart
				ts := overallBestTraceSet
				phases := make([]string, len(ts))
				simCA := make([]float64, len(ts))
				simCB := make([]float64, len(ts))
				gains := make([]float64, len(ts))
				for i, tr := range ts {
					phases[i] = fmt.Sprintf("%.0f°", tr.Alpha2)
					simCA[i] = tr.SimCA
					simCB[i] = tr.SimCB
					gains[i] = tr.Gain
				}
				f, _ := os.Create(filepath.Join(PaperDir(), "composition_trace.tex"))
				err := WriteLineChart(phases, []projector.LineSeries{
					{Name: "sim(C,A)", Data: simCA},
					{Name: "sim(C,B)", Data: simCB},
					{Name: "Gain min(CA,CB)", Data: gains},
				}, "Two-Hop Composition Trace",
					"Phase displacement sweep: sim(C,A), sim(C,B), and gain for composed midpoint.",
					"fig:composition_trace", "composition_trace", -1.0, 1.0, f)
				So(err, ShouldBeNil)
				if f != nil {
					f.Close()
				}

				// Gain-by-α₁ bar chart
				xAxis := make([]string, len(rows))
				base1Data := make([]float64, len(rows))
				base2Data := make([]float64, len(rows))
				composedData := make([]float64, len(rows))
				for i, r := range rows {
					xAxis[i] = fmt.Sprintf("%.0f°", r.Alpha1)
					base1Data[i] = r.Base1
					base2Data[i] = r.Base2
					composedData[i] = r.Composed
				}
				f2, _ := os.Create(filepath.Join(PaperDir(), "two_hop_gain_by_alpha1.tex"))
				err2 := WriteBarChart(xAxis, []projector.BarSeries{
					{Name: "Base1", Data: base1Data},
					{Name: "Base2", Data: base2Data},
					{Name: "Composed", Data: composedData},
				}, "Two-Hop Gain by First-Hop Angle",
					"Baseline vs composed gain across α₁.",
					"fig:two_hop_gain_bar", "two_hop_gain_by_alpha1", f2)
				So(err2, ShouldBeNil)
				if f2 != nil {
					f2.Close()
				}

				// Summary table
				tableData := []map[string]any{{
					"SeedQuery":  seedQuery,
					"BestMatchB": overallBestB,
					"BestGain":   overallBestTrace.Gain,
					"Base1Max":   overallBase1Max,
					"Base2Max":   overallBase2Max,
				}}
				So(WriteTable(tableData, "two_hop_summary.tex"), ShouldBeNil)
				_, statErr := os.Stat(filepath.Join(PaperDir(), "two_hop_summary.tex"))
				So(statErr, ShouldBeNil)
			})
		})
	})
}
