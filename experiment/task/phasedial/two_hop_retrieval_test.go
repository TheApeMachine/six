package phasedial

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/geometry"
)

func TestTwoHopRetrieval(t *testing.T) {
	Convey("Given a list of aphorisms", t, func() {
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

		twoHopResTotal := TwoHopResult{
			SeedQuery: seedQuery,
			Traces:    []TwoHopTrace{},
		}

		var bestTraceSet []TwoHopTrace
		alpha1List := []float64{15.0, 30.0, 45.0, 60.0, 75.0}
		type alpha1Row struct {
			Alpha1   float64
			Base1    float64
			Base2    float64
			Composed float64
			Traces   []TwoHopTrace
		}
		var alpha1Rows []alpha1Row

		Convey("When rotating the fingerprint A and finding the best match B", func() {
			for _, alpha1Degrees := range alpha1List {
				alpha1Radians := alpha1Degrees * (math.Pi / 180.0)
				rotatedA := fingerprintA.Rotate(alpha1Radians)

				rankedA1 := substrate.PhaseDialRank(candidates, rotatedA)
				bestMatchB := rankedA1[0]
				for _, rank := range rankedA1 {
					candText := geometry.ReadoutText(substrate.Entries[rank.Idx].Readout)
					if candText != seedQuery {
						bestMatchB = rank
						break
					}
				}
				fingerprintB := substrate.Entries[bestMatchB.Idx].Fingerprint
				textB := geometry.ReadoutText(substrate.Entries[bestMatchB.Idx].Readout)

				fingerprintAB := fingerprintA.ComposeMidpoint(fingerprintB)

				So(textB, ShouldNotBeEmpty)
				So(textB, ShouldNotEqual, seedQuery)

				var bestGain float64 = -1.0
				var bestC string

				twoHopRes := TwoHopResult{Traces: []TwoHopTrace{}}

				var bestTrace TwoHopTrace

				for s := range 360 {
					alpha2Degrees := float64(s)
					alpha2Radians := alpha2Degrees * (math.Pi / 180.0)
					rotatedAB := fingerprintAB.Rotate(alpha2Radians)

					rankedA2 := substrate.PhaseDialRank(candidates, rotatedAB)

					var topCIdx int = rankedA2[0].Idx
					for _, rank := range rankedA2 {
						cText := geometry.ReadoutText(substrate.Entries[rank.Idx].Readout)
						if cText != seedQuery && cText != textB {
							topCIdx = rank.Idx
							break
						}
					}
					fingerprintC := substrate.Entries[topCIdx].Fingerprint
					textC := geometry.ReadoutText(substrate.Entries[topCIdx].Readout)

					simCA := fingerprintC.Similarity(fingerprintA)
					simCB := fingerprintC.Similarity(fingerprintB)
					simCAB := fingerprintC.Similarity(fingerprintAB)
					gain := math.Min(simCA, simCB)

					trace := TwoHopTrace{
						Alpha2:      alpha2Degrees,
						Gain:        gain,
						SimCA:       simCA,
						SimCB:       simCB,
						MatchText:   textC,
						SimCAB:      simCAB,
						BalancedSum: 0.5 * (simCA + simCB),
						Separation:  simCAB - math.Max(simCA, simCB),
					}
					twoHopRes.Traces = append(twoHopRes.Traces, trace)

					if gain > bestGain {
						bestGain = gain
						bestC = textC
						bestTrace = trace
					}
				}

				simAB_A := fingerprintAB.Similarity(fingerprintA)
				simAB_B := fingerprintAB.Similarity(fingerprintB)
				simA_B := fingerprintA.Similarity(fingerprintB)

				So(simAB_A, ShouldBeBetweenOrEqual, -1.0, 1.0)
				So(simAB_B, ShouldBeBetweenOrEqual, -1.0, 1.0)
				So(simA_B, ShouldBeBetweenOrEqual, -1.0, 1.0)
				So(bestGain, ShouldBeGreaterThanOrEqualTo, 0)
				So(bestC, ShouldNotBeEmpty)

				var base1BestGain float64 = -1.0
				for s := range 360 {
					alpha2Radians := float64(s) * (math.Pi / 180.0)
					rotatedA2 := fingerprintA.Rotate(alpha2Radians)
					rankedA2 := substrate.PhaseDialRank(candidates, rotatedA2)
					var topCIdx int = rankedA2[0].Idx
					for _, rank := range rankedA2 {
						cText := geometry.ReadoutText(substrate.Entries[rank.Idx].Readout)
						if cText != seedQuery && cText != textB {
							topCIdx = rank.Idx
							break
						}
					}
					fingerprintC := substrate.Entries[topCIdx].Fingerprint
					gain := math.Min(fingerprintC.Similarity(fingerprintA), fingerprintC.Similarity(fingerprintB))
					if gain > base1BestGain {
						base1BestGain = gain
					}
				}

				var base2BestGain float64 = -1.0
				for s := range 360 {
					alpha2Radians := float64(s) * (math.Pi / 180.0)
					rotatedB := fingerprintB.Rotate(alpha2Radians)
					rankedB := substrate.PhaseDialRank(candidates, rotatedB)
					var topCIdx int = rankedB[0].Idx
					for _, rank := range rankedB {
						cText := geometry.ReadoutText(substrate.Entries[rank.Idx].Readout)
						if cText != seedQuery && cText != textB {
							topCIdx = rank.Idx
							break
						}
					}
					fingerprintC := substrate.Entries[topCIdx].Fingerprint
					gain := math.Min(fingerprintC.Similarity(fingerprintA), fingerprintC.Similarity(fingerprintB))
					if gain > base2BestGain {
						base2BestGain = gain
					}
				}

				cutoffGain := math.Max(base1BestGain, base2BestGain)
				So(bestGain, ShouldBeGreaterThanOrEqualTo, 0)
				So(cutoffGain, ShouldBeGreaterThanOrEqualTo, 0)

				if bestGain > twoHopResTotal.BestComposed.Gain {
					twoHopResTotal.BestMatchB = textB
					twoHopResTotal.BestComposed = bestTrace
					bestTraceSet = twoHopRes.Traces
				}
				if bestGain > twoHopResTotal.Base1MaxGain {
					twoHopResTotal.Base1MaxGain = base1BestGain
				}
				if bestGain > twoHopResTotal.Base2MaxGain {
					twoHopResTotal.Base2MaxGain = base2BestGain
				}
				twoHopResTotal.Traces = append(twoHopResTotal.Traces, twoHopRes.Traces...)

				tracesCopy := make([]TwoHopTrace, len(twoHopRes.Traces))
				copy(tracesCopy, twoHopRes.Traces)
				alpha1Rows = append(alpha1Rows, alpha1Row{
					Alpha1:   alpha1Degrees,
					Base1:    base1BestGain,
					Base2:    base2BestGain,
					Composed: bestGain,
					Traces:   tracesCopy,
				})
			}

			So(twoHopResTotal.SeedQuery, ShouldNotBeEmpty)
			So(twoHopResTotal.BestMatchB, ShouldNotBeEmpty)

			Convey("Artifacts should be written to paper directory", func() {
				if len(bestTraceSet) == 0 {
					bestTraceSet = twoHopResTotal.Traces
				}
				if len(bestTraceSet) > 0 {
					phases := make([]string, len(bestTraceSet))
					simCA := make([]float64, len(bestTraceSet))
					simCB := make([]float64, len(bestTraceSet))
					gain := make([]float64, len(bestTraceSet))
					for i, tr := range bestTraceSet {
						phases[i] = fmt.Sprintf("%.0f°", tr.Alpha2)
						simCA[i] = tr.SimCA
						simCB[i] = tr.SimCB
						gain[i] = tr.Gain
					}
					series := []projector.LineSeries{
						{Name: "sim(C,A)", Data: simCA},
						{Name: "sim(C,B)", Data: simCB},
						{Name: "Gain min(CA, CB)", Data: gain},
					}
					f, _ := os.Create(filepath.Join(PaperDir(), "composition_trace.tex"))
					err := WriteLineChart(phases, series, "Two-Hop Composition Trace",
						"Phase displacement sweep: sim(C,A), sim(C,B), and gain for composed midpoint.",
						"fig:composition_trace", "composition_trace", -1.0, 1.0, f)
					So(err, ShouldBeNil)
					if f != nil {
						f.Close()
					}
				}

				if len(alpha1Rows) > 0 {
					xAxis := make([]string, len(alpha1Rows))
					base1Data := make([]float64, len(alpha1Rows))
					base2Data := make([]float64, len(alpha1Rows))
					composedData := make([]float64, len(alpha1Rows))
					for i, r := range alpha1Rows {
						xAxis[i] = fmt.Sprintf("%.0f°", r.Alpha1)
						base1Data[i] = r.Base1
						base2Data[i] = r.Base2
						composedData[i] = r.Composed
					}
					barSeries := []projector.BarSeries{
						{Name: "Base1", Data: base1Data},
						{Name: "Base2", Data: base2Data},
						{Name: "Composed", Data: composedData},
					}
					f, _ := os.Create(filepath.Join(PaperDir(), "two_hop_gain_by_alpha1.tex"))
					err := WriteBarChart(xAxis, barSeries, "Two-Hop Gain by First-Hop Angle",
						"Baseline vs composed gain across α₁.",
						"fig:two_hop_gain_bar", "two_hop_gain_by_alpha1", f)
					So(err, ShouldBeNil)
					if f != nil {
						f.Close()
					}
				}

				if len(alpha1Rows) > 0 {
					const alpha2Step = 18
					var xLabels []string
					for a := 0; a < 360; a += alpha2Step {
						xLabels = append(xLabels, fmt.Sprintf("%d°", a))
					}
					yLabels := make([]string, len(alpha1Rows))
					for i, r := range alpha1Rows {
						yLabels[i] = fmt.Sprintf("%.0f°", r.Alpha1)
					}
					heatmapData := [][]any{}
					for yIdx, r := range alpha1Rows {
						for xIdx := 0; xIdx < len(xLabels); xIdx++ {
							trIdx := xIdx * alpha2Step
							if trIdx >= len(r.Traces) {
								trIdx = len(r.Traces) - 1
							}
							val := r.Traces[trIdx].Gain
							heatmapData = append(heatmapData, []any{xIdx, yIdx, val})
						}
					}
					var minV, maxV float64 = -1, 1
					f, _ := os.Create(filepath.Join(PaperDir(), "two_hop_heatmap.tex"))
					err := WriteHeatMap(xLabels, yLabels, heatmapData, minV, maxV,
						"Two-Hop Gain Heatmap", "Gain vs (α₁, α₂).",
						"fig:two_hop_heatmap", "two_hop_heatmap", f)
					So(err, ShouldBeNil)
					if f != nil {
						f.Close()
					}
				}

				tableData := []map[string]any{
					{"SeedQuery": twoHopResTotal.SeedQuery, "BestMatchB": twoHopResTotal.BestMatchB,
						"BestGain": twoHopResTotal.BestComposed.Gain, "Base1Max": twoHopResTotal.Base1MaxGain, "Base2Max": twoHopResTotal.Base2MaxGain},
				}
				err := WriteTable(tableData, "two_hop_summary.tex")
				So(err, ShouldBeNil)

				tablePath := filepath.Join(PaperDir(), "two_hop_summary.tex")
				_, statErr := os.Stat(tablePath)
				So(statErr, ShouldBeNil)
			})
		})
	})
}
