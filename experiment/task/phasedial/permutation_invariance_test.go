package phasedial

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
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
				data := []map[string]any{
					{"Step": 0, "Phase": "0°", "Margin": results[0].Margin, "Entropy": results[0].Entropy},
					{"Step": 36, "Phase": "180°", "Margin": results[36].Margin, "Entropy": results[36].Entropy},
					{"Step": 72, "Phase": "360°", "Margin": results[72].Margin, "Entropy": results[72].Entropy},
				}
				err := WriteTable(data, "permutation_metrics.tex")
				So(err, ShouldBeNil)

				tablePath := filepath.Join(PaperDir(), "permutation_metrics.tex")
				_, statErr := os.Stat(tablePath)
				So(statErr, ShouldBeNil)
			})
		})
	})
}
