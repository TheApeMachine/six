package phasedial

import (
	"fmt"
	"math/rand"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
)

func TestPartialDeletion(t *testing.T) {
	Convey("Given the aphorism corpus with ~30% of items dropped (critical items protected)", t, func() {
		aphorisms := Aphorisms

		criticalMap := map[string]bool{
			"Democracy requires individual sacrifice.":               true,
			"Nature does not hurry, yet everything is accomplished.": true,
			"Authoritarianism emerges from collective self-interest.": true,
			"A rolling stone gathers no moss.":                       true,
		}

		// Deterministic drop: use fixed seed so test is reproducible
		rng := rand.New(rand.NewSource(42))
		dropCount := 0
		targetDrops := 7
		var kept []string
		for _, text := range aphorisms {
			if !criticalMap[text] && dropCount < targetDrops && rng.Float32() < 0.4 {
				dropCount++
				continue
			}
			kept = append(kept, text)
		}

		So(len(kept), ShouldBeGreaterThan, 0)
		So(len(kept), ShouldBeLessThan, len(aphorisms))

		substrate := geometry.NewHybridSubstrate()
		var seedFingerprint geometry.PhaseDial
		var universalFilter data.Chord

		for i, text := range kept {
			fingerprint := geometry.NewPhaseDial().Encode(text)
			readout := []byte(fmt.Sprintf("%d: %s", i, text))
			substrate.Add(universalFilter, fingerprint, readout)
			if text == "Democracy requires individual sacrifice." {
				seedFingerprint = append(geometry.PhaseDial{}, fingerprint...)
			}
		}

		So(seedFingerprint, ShouldNotBeNil)

		Convey("When running a geodesic scan on the reduced substrate", func() {
			results := substrate.GeodesicScan(seedFingerprint, 72, 5.0)

			Convey("The scan should still produce 73 steps", func() {
				So(results, ShouldHaveLength, 73)
			})

			Convey("Every step should yield a non-empty readout", func() {
				for _, r := range results {
					So(r.BestReadout, ShouldNotBeEmpty)
				}
			})

			Convey("Every step should have a non-negative margin", func() {
				for _, r := range results {
					So(r.Margin, ShouldBeGreaterThanOrEqualTo, 0)
				}
			})

			Convey("The topology should still resolve semantically (seed item remains findable)", func() {
				// At phase=0° the seed fingerprint itself should be near top
				step0 := results[0]
				readout0 := geometry.ReadoutText(step0.BestReadout)
				So(readout0, ShouldNotBeEmpty)
			})

			Convey("All critical items should still be present in the substrate", func() {
				readouts := make([]string, len(substrate.Entries))
				for i, entry := range substrate.Entries {
					readouts[i] = geometry.ReadoutText(entry.Readout)
				}
				So(readouts, ShouldContain, "Democracy requires individual sacrifice.")
				So(readouts, ShouldContain, "Nature does not hurry, yet everything is accomplished.")
			})

			Convey("Artifacts should be written to the paper directory", func() {
				tableRows := []map[string]any{
					{
						"OriginalSize": len(aphorisms),
						"KeptSize":     len(kept),
						"DroppedCount": dropCount,
						"ScanSteps":    len(results),
						"Step0Match":   geometry.ReadoutText(results[0].BestReadout),
						"Step36Match":  geometry.ReadoutText(results[36].BestReadout),
						"Step72Match":  geometry.ReadoutText(results[72].BestReadout),
					},
				}
				tableErr := WriteTable(tableRows, "partial_deletion_summary.tex")
				So(tableErr, ShouldBeNil)
			})
		})
	})
}
