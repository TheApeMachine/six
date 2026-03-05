package logic

import (
	"fmt"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/gpu/metal"
	"github.com/theapemachine/six/resonance"
	"github.com/theapemachine/six/store"
)

/*
TestContradictionResolution tests the claim that destructive interference
naturally eliminates false hypotheses without explicit filtering.

Setup: 5 contradictory location statements. The "true" answer has unique
reinforcement bits that the query also contains, creating constructive
resonance. False answers have no overlap with these reinforcement bits,
creating destructive interference (noise penalty in the FillScore).
*/
func TestContradictionResolution(t *testing.T) {
	Convey("Given contradictory facts stored in the prime substrate", t, func() {

		// Each location is a fully orthogonal concept
		locations := []struct {
			name   string
			primes []int
		}{
			{"under the mat", []int{10, 11, 12, 13, 14}},
			{"in the car", []int{20, 21, 22, 23, 24}},
			{"in the drawer", []int{30, 31, 32, 33, 34}},
			{"on the table", []int{40, 41, 42, 43, 44}},
			{"in the pocket", []int{50, 51, 52, 53, 54}},
		}

		trueIdx := 2 // "in the drawer" is the true answer

		// Reinforcement bits: shared between the true answer and the query
		// These represent "prior evidence" that strengthens the true hypothesis
		reinforcementBits := []int{200, 201, 202, 203, 204, 205, 206, 207, 208, 209}

		Convey("When building chords for each location hypothesis", func() {
			chords := make([]data.Chord, len(locations))

			for i, loc := range locations {
				for _, p := range loc.primes {
					chords[i].Set(p)
				}

				// Only the true answer gets reinforcement bits
				if i == trueIdx {
					for _, p := range reinforcementBits {
						chords[i].Set(p)
					}
				}
			}

			// Build query: contains only the reinforcement bits
			// This simulates "we have evidence pointing to the truth"
			var queryChord data.Chord
			for _, p := range reinforcementBits {
				queryChord.Set(p)
			}

			Convey("Then FillScore correctly ranks the true answer highest (pure bitwise logic)", func() {
				type scored struct {
					name  string
					score float64
				}

				var results []scored
				bestScore := 0.0
				bestIdx := -1

				for i, loc := range locations {
					fScore := resonance.FillScore(&queryChord, &chords[i])
					results = append(results, scored{loc.name, fScore})

					if fScore > bestScore {
						bestScore = fScore
						bestIdx = i
					}
				}

				fmt.Printf("\n--- Contradiction Resolution (CPU) ---\n")
				for i, r := range results {
					marker := "  "
					if i == trueIdx {
						marker = "✓ "
					}
					fmt.Printf("%s%-15s FillScore=%.4f  Bits=%d\n",
						marker, r.name, r.score, chords[i].ActiveCount())
				}

				// The true answer must win
				So(bestIdx, ShouldEqual, trueIdx)

				// All false answers should have score 0 (no overlap with query)
				for i, r := range results {
					if i != trueIdx {
						So(r.score, ShouldEqual, 0.0)
					}
				}
			})

			Convey("Then the GPU BestFill also resolves the contradiction correctly", func() {
				pf := store.NewPrimeField()

				for _, chord := range chords {
					pf.Insert(chord)
				}

				// Build GPU query context
				var queryCtx geometry.IcosahedralManifold
				for i := 0; i < 8; i++ {
				queryCtx.Cubes[0][0][i] = queryChord[i]
			}

				bestGPUIdx, bestGPUScore, err := metal.BestFill(
					pf.Field(), pf.N, unsafe.Pointer(&queryCtx), nil, 0, unsafe.Pointer(&geometry.UnifiedGeodesicMatrix[0]),
				)

				So(err, ShouldBeNil)
				So(bestGPUScore, ShouldBeGreaterThan, 0.0)

				fmt.Printf("\n--- Contradiction Resolution (GPU) ---\n")
				fmt.Printf("GPU winner: idx=%d ('%s'), score=%.4f\n",
					bestGPUIdx, locations[bestGPUIdx].name, bestGPUScore)

				// GPU must find the true answer
				So(bestGPUIdx, ShouldEqual, trueIdx)
			})
		})
	})
}
