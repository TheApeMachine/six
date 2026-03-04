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

func TestSnapToSurface(t *testing.T) {
	Convey("Given the aphorism corpus ingested into a PhaseDial substrate", t, func() {
		sub := NewSubstrate()
		seedQuery := "Democracy requires individual sacrifice."
		fingerprintA := geometry.NewPhaseDial().Encode(seedQuery)
		alpha1List := []float64{15.0, 30.0, 45.0, 60.0, 75.0}

		type snapSlice struct {
			Alpha1    float64
			SnapAlpha float64
			SnapScore float64
			SnapGain  float64
			MidptGain float64
			Base1Gain float64
			Base2Gain float64
			SnapC     string
		}
		var slices []snapSlice

		Convey("When snapping the composed midpoint F_AB onto the manifold surface via phase sweep", func() {
			for _, alpha1Deg := range alpha1List {
				hop := sub.FirstHop(fingerprintA, alpha1Deg*(math.Pi/180.0), seedQuery)
				fpA, fpB, fpAB := fingerprintA, hop.FingerprintB, hop.FingerprintAB
				textB := hop.TextB

				So(textB, ShouldNotBeEmpty)
				So(textB, ShouldNotEqual, seedQuery)
				So(fpAB.Similarity(fpA), ShouldBeBetweenOrEqual, -1.0, 1.0)
				So(fpAB.Similarity(fpB), ShouldBeBetweenOrEqual, -1.0, 1.0)

				// Find α* that maximises peak corpus score from F_AB
				var bestSnapAlpha float64
				var bestSnapScore float64 = -math.MaxFloat64
				for s := range 360 {
					alpha := float64(s) * (math.Pi / 180.0)
					ranked := sub.PhaseDialRank(sub.Candidates, fpAB.Rotate(alpha))
					if ranked[0].Score > bestSnapScore {
						bestSnapScore = ranked[0].Score
						bestSnapAlpha = float64(s)
					}
				}
				So(bestSnapScore, ShouldBeGreaterThan, -1.0)

				snappedAB := fpAB.Rotate(bestSnapAlpha * (math.Pi / 180.0))
				snapGain := sub.BestGain(snappedAB, fpA, fpB, seedQuery, textB)

				// Best C under snapped anchor
				var snapBestC string
				var bestG float64 = -1.0
				for s := range 360 {
					rot := snappedAB.Rotate(float64(s) * (math.Pi / 180.0))
					ranked := sub.PhaseDialRank(sub.Candidates, rot)
					topIdx := sub.TopExcluding(ranked, seedQuery, textB)
					fpC := sub.Entries[topIdx].Fingerprint
					if g := math.Min(fpC.Similarity(fpA), fpC.Similarity(fpB)); g > bestG {
						bestG = g
						snapBestC = geometry.ReadoutText(sub.Entries[topIdx].Readout)
					}
				}

				midptGain := sub.BestGain(fpAB, fpA, fpB, seedQuery, textB)
				base1Gain := sub.BestGain(fpA, fpA, fpB, seedQuery, textB)
				base2Gain := sub.BestGain(fpB, fpA, fpB, seedQuery, textB)

				So(snapGain, ShouldBeGreaterThanOrEqualTo, 0)
				So(midptGain, ShouldBeGreaterThanOrEqualTo, 0)
				So(base1Gain, ShouldBeGreaterThanOrEqualTo, 0)
				So(base2Gain, ShouldBeGreaterThanOrEqualTo, 0)
				So(snapBestC, ShouldNotBeEmpty)

				slices = append(slices, snapSlice{
					Alpha1:    alpha1Deg,
					SnapAlpha: bestSnapAlpha,
					SnapScore: bestSnapScore,
					SnapGain:  snapGain,
					MidptGain: midptGain,
					Base1Gain: base1Gain,
					Base2Gain: base2Gain,
					SnapC:     snapBestC,
				})
			}

			Convey("All slices should have valid snap gains and scores", func() {
				So(len(slices), ShouldEqual, len(alpha1List))
				for _, s := range slices {
					So(s.SnapGain, ShouldBeGreaterThanOrEqualTo, 0)
					So(s.SnapScore, ShouldBeGreaterThan, -1.0)
					So(s.SnapC, ShouldNotBeEmpty)
				}
			})

			Convey("Snap gain should match or exceed the raw midpoint gain", func() {
				for _, s := range slices {
					So(s.SnapGain, ShouldBeGreaterThanOrEqualTo, s.MidptGain-1e-9)
				}
			})

			Convey("Artifacts should be written to the paper directory", func() {
				xAxis := make([]string, len(slices))
				snapGains := make([]float64, len(slices))
				midptGains := make([]float64, len(slices))
				base1Gains := make([]float64, len(slices))
				base2Gains := make([]float64, len(slices))
				snapScores := make([]float64, len(slices))
				for i, s := range slices {
					xAxis[i] = fmt.Sprintf("%.0f°", s.Alpha1)
					snapGains[i] = s.SnapGain
					midptGains[i] = s.MidptGain
					base1Gains[i] = s.Base1Gain
					base2Gains[i] = s.Base2Gain
					snapScores[i] = s.SnapScore
				}
				f, _ := os.Create(filepath.Join(PaperDir(), "snap_surface.tex"))
				err := WriteComboChart(xAxis, []projector.ComboSeries{
					{Name: "Snap Gain", Type: "bar", BarWidth: "15%", Data: snapGains},
					{Name: "Midpoint Gain", Type: "bar", BarWidth: "15%", Data: midptGains},
					{Name: "Baseline A", Type: "dashed", Symbol: "diamond", Data: base1Gains},
					{Name: "Baseline B", Type: "dashed", Symbol: "triangle", Data: base2Gains},
					{Name: "Snap Peak Score", Type: "line", Symbol: "circle", Data: snapScores},
				}, "First-Hop Angle α₁", "Gain / Score", -0.15, 0.45,
					"Snap-to-Surface: Gain and Peak Score",
					"Snap gain, midpoint gain, single-anchor baselines (dashed), and snap peak corpus score across first-hop angles α₁.",
					"fig:snap_surface", "snap_surface", f)
				So(err, ShouldBeNil)
				if f != nil {
					f.Close()
				}

				tableRows := make([]map[string]any, len(slices))
				for i, s := range slices {
					tableRows[i] = map[string]any{
						"Alpha1":    fmt.Sprintf("%.0f°", s.Alpha1),
						"SnapAlpha": fmt.Sprintf("%.0f°", s.SnapAlpha),
						"SnapGain":  fmt.Sprintf("%.4f", s.SnapGain),
						"MidptGain": fmt.Sprintf("%.4f", s.MidptGain),
						"Base1":     fmt.Sprintf("%.4f", s.Base1Gain),
						"Base2":     fmt.Sprintf("%.4f", s.Base2Gain),
					}
				}
				So(WriteTable(tableRows, "snap_surface_summary.tex"), ShouldBeNil)
				_, statErr := os.Stat(filepath.Join(PaperDir(), "snap_surface_summary.tex"))
				So(statErr, ShouldBeNil)
			})
		})
	})
}
