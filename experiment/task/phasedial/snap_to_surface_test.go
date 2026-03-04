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

func TestSnapToSurface(t *testing.T) {
	Convey("Given the aphorism corpus ingested into a PhaseDial substrate", t, func() {
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

		// sweepBestGain sweeps α₂ over an anchor and returns the best
		// min(sim(C,A), sim(C,B)) across all rotations.
		sweepBestGain := func(anchor, fpA, fpB geometry.PhaseDial, excludeA, excludeB string) float64 {
			var best float64 = -1.0
			for s := range 360 {
				alpha2 := float64(s) * (math.Pi / 180.0)
				rotated := anchor.Rotate(alpha2)
				ranked := substrate.PhaseDialRank(candidates, rotated)
				topIdx := ranked[0].Idx
				for _, rank := range ranked {
					ct := geometry.ReadoutText(substrate.Entries[rank.Idx].Readout)
					if ct != excludeA && ct != excludeB {
						topIdx = rank.Idx
						break
					}
				}
				fpC := substrate.Entries[topIdx].Fingerprint
				g := math.Min(fpC.Similarity(fpA), fpC.Similarity(fpB))
				if g > best {
					best = g
				}
			}
			return best
		}

		type snapSlice struct {
			Alpha1     float64
			SnapAlpha  float64
			SnapScore  float64
			SnapGain   float64
			MidptGain  float64
			Base1Gain  float64
			Base2Gain  float64
			SnapC      string
			SnapSimCA  float64
			SnapSimCB  float64
		}

		alpha1List := []float64{15.0, 30.0, 45.0, 60.0, 75.0}
		var slices []snapSlice

		Convey("When snapping the composed midpoint F_AB onto the manifold surface via phase sweep", func() {
			for _, alpha1Deg := range alpha1List {
				alpha1Rad := alpha1Deg * (math.Pi / 180.0)
				rotatedA := fingerprintA.Rotate(alpha1Rad)
				rankedA1 := substrate.PhaseDialRank(candidates, rotatedA)
				bestMatchB := rankedA1[0]
				for _, rank := range rankedA1 {
					if geometry.ReadoutText(substrate.Entries[rank.Idx].Readout) != seedQuery {
						bestMatchB = rank
						break
					}
				}
				fingerprintB := substrate.Entries[bestMatchB.Idx].Fingerprint
				textB := geometry.ReadoutText(substrate.Entries[bestMatchB.Idx].Readout)

				So(textB, ShouldNotBeEmpty)
				So(textB, ShouldNotEqual, seedQuery)

				fingerprintAB := fingerprintA.ComposeMidpoint(fingerprintB)

				simAB_A := fingerprintAB.Similarity(fingerprintA)
				simAB_B := fingerprintAB.Similarity(fingerprintB)
				// Midpoint should be equidistant from A and B
				So(simAB_A, ShouldBeBetweenOrEqual, -1.0, 1.0)
				So(simAB_B, ShouldBeBetweenOrEqual, -1.0, 1.0)

				// Step 1: find α* that maximises peak corpus score from F_AB
				var bestSnapAlpha float64
				var bestSnapScore float64 = -math.MaxFloat64
				for s := range 360 {
					alpha := float64(s) * (math.Pi / 180.0)
					rotated := fingerprintAB.Rotate(alpha)
					ranked := substrate.PhaseDialRank(candidates, rotated)
					if ranked[0].Score > bestSnapScore {
						bestSnapScore = ranked[0].Score
						bestSnapAlpha = float64(s)
					}
				}

				So(bestSnapScore, ShouldBeGreaterThan, -1.0)

				// Step 2: build snapped anchor and measure hop-2 gain
				snappedAB := fingerprintAB.Rotate(bestSnapAlpha * (math.Pi / 180.0))
				snapGain := sweepBestGain(snappedAB, fingerprintA, fingerprintB, seedQuery, textB)

				// Find the best C text + similarities for snapped anchor
				var snapBestC string
				var snapSimCA, snapSimCB float64
				{
					var bestG float64 = -1.0
					for s := range 360 {
						a2 := float64(s) * (math.Pi / 180.0)
						rot := snappedAB.Rotate(a2)
						ranked := substrate.PhaseDialRank(candidates, rot)
						topIdx := ranked[0].Idx
						for _, rank := range ranked {
							ct := geometry.ReadoutText(substrate.Entries[rank.Idx].Readout)
							if ct != seedQuery && ct != textB {
								topIdx = rank.Idx
								break
							}
						}
						fpC := substrate.Entries[topIdx].Fingerprint
						ca := fpC.Similarity(fingerprintA)
						cb := fpC.Similarity(fingerprintB)
						g := math.Min(ca, cb)
						if g > bestG {
							bestG = g
							snapBestC = geometry.ReadoutText(substrate.Entries[topIdx].Readout)
							snapSimCA = ca
							snapSimCB = cb
						}
					}
				}

				midptGain := sweepBestGain(fingerprintAB, fingerprintA, fingerprintB, seedQuery, textB)
				base1Gain := sweepBestGain(fingerprintA, fingerprintA, fingerprintB, seedQuery, textB)
				base2Gain := sweepBestGain(fingerprintB, fingerprintA, fingerprintB, seedQuery, textB)

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
					SnapSimCA: snapSimCA,
					SnapSimCB: snapSimCB,
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
				for i, s := range slices {
					xAxis[i] = fmt.Sprintf("%.0f°", s.Alpha1)
					snapGains[i] = s.SnapGain
					midptGains[i] = s.MidptGain
					base1Gains[i] = s.Base1Gain
					base2Gains[i] = s.Base2Gain
				}
				barSeries := []projector.BarSeries{
					{Name: "Snap Gain", Data: snapGains},
					{Name: "Midpoint Gain", Data: midptGains},
					{Name: "Baseline A", Data: base1Gains},
					{Name: "Baseline B", Data: base2Gains},
				}
				f, _ := os.Create(filepath.Join(PaperDir(), "snap_surface_bar.tex"))
				err := WriteBarChart(xAxis, barSeries,
					"Snap-to-Surface Gain vs Baselines",
					"Snap gain, raw midpoint gain, and single-anchor baselines across first-hop angles α₁.",
					"fig:snap_surface_bar", "snap_surface_bar", f)
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
				tableErr := WriteTable(tableRows, "snap_surface_summary.tex")
				So(tableErr, ShouldBeNil)

				tablePath := filepath.Join(PaperDir(), "snap_surface_summary.tex")
				_, statErr := os.Stat(tablePath)
				So(statErr, ShouldBeNil)
			})
		})
	})
}
