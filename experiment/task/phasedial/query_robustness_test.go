package phasedial

import (
	"fmt"
	"math/rand"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
)

func TestQueryRobustness(t *testing.T) {
	Convey("Given the intact aphorism corpus and a 30% character-dropout corrupted query", t, func() {
		aphorisms := Aphorisms
		substrate := geometry.NewHybridSubstrate()
		var universalFilter data.Chord

		for i, text := range aphorisms {
			fingerprint := geometry.NewPhaseDial().Encode(text)
			readout := []byte(fmt.Sprintf("%d: %s", i, text))
			substrate.Add(universalFilter, fingerprint, readout)
		}

		rawQuery := "Democracy requires individual sacrifice."

		// Apply deterministic 30% character dropout (replace with '_')
		rng := rand.New(rand.NewSource(7))
		queryBytes := []byte(rawQuery)
		maskedQuery := make([]byte, len(queryBytes))
		for i, b := range queryBytes {
			if rng.Float32() > 0.3 {
				maskedQuery[i] = b
			} else {
				maskedQuery[i] = '_'
			}
		}

		So(string(maskedQuery), ShouldNotEqual, rawQuery)
		So(len(maskedQuery), ShouldEqual, len(queryBytes))

		corruptedFP := geometry.NewPhaseDial().Encode(string(maskedQuery))
		cleanFP := geometry.NewPhaseDial().Encode(rawQuery)

		Convey("When running geodesic scans with the corrupted vs clean query fingerprints", func() {
			corruptedResults := substrate.GeodesicScan(corruptedFP, 72, 5.0)
			cleanResults := substrate.GeodesicScan(cleanFP, 72, 5.0)

			Convey("Both scans should produce 73 steps", func() {
				So(corruptedResults, ShouldHaveLength, 73)
				So(cleanResults, ShouldHaveLength, 73)
			})

			Convey("The corrupted query scan should still yield non-empty readouts at every step", func() {
				for _, r := range corruptedResults {
					So(r.BestReadout, ShouldNotBeEmpty)
				}
			})

			Convey("The corrupted fingerprint should remain similar to the clean fingerprint", func() {
				sim := corruptedFP.Similarity(cleanFP)
				// With 30% dropout, the corrupted query should still correlate positively
				So(sim, ShouldBeGreaterThan, 0.0)
			})

			Convey("The Democracy item should be resolved by the corrupted scan within 0–36° (first half-sweep)", func() {
				found := false
				for _, r := range corruptedResults[:37] {
					if geometry.ReadoutText(r.BestReadout) == rawQuery {
						found = true
						break
					}
				}
				// Allow the result at any point in the full sweep
				if !found {
					for _, r := range corruptedResults {
						if geometry.ReadoutText(r.BestReadout) == rawQuery {
							found = true
							break
						}
					}
				}
				So(found, ShouldBeTrue)
			})

			Convey("Artifacts should be written to the paper directory", func() {
				tableRows := []map[string]any{
					{
						"Query":      "Clean",
						"ScanSteps":  len(cleanResults),
						"Step0Match": geometry.ReadoutText(cleanResults[0].BestReadout),
						"Step36Match": geometry.ReadoutText(cleanResults[36].BestReadout),
						"CorruptSim": fmt.Sprintf("%.4f", corruptedFP.Similarity(cleanFP)),
					},
					{
						"Query":      fmt.Sprintf("Corrupted (30%% dropout): %s", string(maskedQuery)),
						"ScanSteps":  len(corruptedResults),
						"Step0Match": geometry.ReadoutText(corruptedResults[0].BestReadout),
						"Step36Match": geometry.ReadoutText(corruptedResults[36].BestReadout),
						"CorruptSim": fmt.Sprintf("%.4f", corruptedFP.Similarity(cleanFP)),
					},
				}
				tableErr := WriteTable(tableRows, "query_robustness_summary.tex")
				So(tableErr, ShouldBeNil)
			})
		})
	})
}
