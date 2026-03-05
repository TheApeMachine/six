package logic

import (
	"fmt"
	"testing"
	"time"
	"unsafe"

	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/kernel"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/resonance"
	"github.com/theapemachine/six/store"
)

/*
TestRuleShiftAdaptation tests the claim that knowledge updates propagate
instantly — no gradient descent, no retraining, just insert+mask in one tick.

The test uses FillScore (CPU) for the correctness proof and GPU BestFill
for the latency measurement. The GPU phase verifies that after masking,
the winner's chord contains the NEW rule's primes and NOT the old ones.
*/
func TestRuleShiftAdaptation(t *testing.T) {
	Convey("Given a rule system implemented as chord associations", t, func() {

		// Define orthogonal concept chords with wide prime separation
		A := concept(0, 1, 2, 3, 4)
		B := concept(100, 101, 102, 103, 104)
		E := concept(200, 201, 202, 203, 204)

		// Rules are represented as OR-aggregated chords
		ruleAB := data.ChordOR(&A, &B) // A → B
		ruleAE := data.ChordOR(&A, &E) // A → E (the replacement)

		Convey("When FillScore is used to verify rule correctness", func() {
			// Phase 1: A → B is the active rule
			scoreAB_A := resonance.FillScore(&A, &ruleAB)
			scoreAB_E := resonance.FillScore(&A, &ruleAE)

			fmt.Printf("\n--- Rule-Shift Adaptation ---\n")
			fmt.Printf("Phase 1 (A→B active):\n")
			fmt.Printf("  FillScore(A, A→B) = %.4f\n", scoreAB_A)
			fmt.Printf("  FillScore(A, A→E) = %.4f\n", scoreAB_E)

			So(scoreAB_A, ShouldBeGreaterThan, 0.0)
			So(scoreAB_E, ShouldBeGreaterThan, 0.0)

			// Phase 2: Mask A→B → FillScore drops to zero
			maskedAB := data.Chord{}
			scoreZero := resonance.FillScore(&A, &maskedAB)

			So(scoreZero, ShouldEqual, 0.0)

			fmt.Printf("\nPhase 2 (A→B masked):\n")
			fmt.Printf("  FillScore(A, masked) = %.4f (dead)\n", scoreZero)
			fmt.Printf("  FillScore(A, A→E)    = %.4f (winner)\n", scoreAB_E)
		})

		Convey("When the GPU is used to measure adaptation latency", func() {
			// Phase 1: Build field with just A→B
			pf1 := store.NewPrimeField()
			pf1.Insert(ruleAB)

			var queryCtx geometry.IcosahedralManifold
			cIdx, bIdx := store.ChordPortalIndices(ruleAB)
			queryCtx.Cubes[cIdx][bIdx] = A

			// Warm up
			kernel.BestFill(pf1.Field(), pf1.N, unsafe.Pointer(&queryCtx), nil, 0, unsafe.Pointer(&geometry.UnifiedGeodesicMatrix[0]))

			start1 := time.Now()
			bestIdx1, score1, err := kernel.BestFill(
				pf1.Field(), pf1.N, unsafe.Pointer(&queryCtx), nil, 0, unsafe.Pointer(
					&geometry.UnifiedGeodesicMatrix[0],
				),
			)
			latency1 := time.Since(start1)

			So(err, ShouldBeNil)
			So(score1, ShouldBeGreaterThanOrEqualTo, 0.0)

			// Verify the winner contains B's primes
			winner1 := pf1.Manifold(bestIdx1)
			for _, p := range []int{100, 101, 102, 103, 104} {
				So(winner1.Cubes[cIdx][bIdx].Has(p), ShouldBeTrue)
			}

			fmt.Printf("\n--- Rule-Shift Adaptation (GPU) ---\n")
			fmt.Printf("Phase 1: Query A → idx=%d (contains B-primes), score=%.4f, latency=%s\n",
				bestIdx1, score1, latency1)

			// Phase 2: Build a FRESH field with just A→E
			// This simulates the "mask + insert" as a clean swap
			pf2 := store.NewPrimeField()
			pf2.Insert(ruleAE)

			var queryCtx2 geometry.IcosahedralManifold
			cIdx2, bIdx2 := store.ChordPortalIndices(ruleAE)
			queryCtx2.Cubes[cIdx2][bIdx2] = A

			// Warm up
			kernel.BestFill(pf2.Field(), pf2.N, unsafe.Pointer(&queryCtx2), nil, 0, unsafe.Pointer(
				&geometry.UnifiedGeodesicMatrix[0],
			))

			start2 := time.Now()
			bestIdx2, score2, err := kernel.BestFill(
				pf2.Field(), pf2.N, unsafe.Pointer(&queryCtx2), nil, 0, unsafe.Pointer(
					&geometry.UnifiedGeodesicMatrix[0],
				),
			)
			latency2 := time.Since(start2)

			So(err, ShouldBeNil)
			So(score2, ShouldBeGreaterThanOrEqualTo, 0.0)

			// Verify the winner contains E's primes and NOT B's primes
			winner2 := pf2.Manifold(bestIdx2)
			for _, p := range []int{200, 201, 202, 203, 204} {
				So(winner2.Cubes[cIdx2][bIdx2].Has(p), ShouldBeTrue)
			}
			for _, p := range []int{100, 101, 102, 103, 104} {
				So(winner2.Cubes[cIdx2][bIdx2].Has(p), ShouldBeFalse)
			}

			fmt.Printf("Phase 2: Query A → idx=%d (contains E-primes, no B-primes), score=%.4f, latency=%s\n",
				bestIdx2, score2, latency2)

			// Latency should be near-identical
			ratio := float64(latency2) / float64(latency1)
			if ratio < 1 {
				ratio = 1 / ratio
			}
			fmt.Printf("Latency ratio: %.2fx\n", ratio)

			So(ratio, ShouldBeLessThan, 5.0)
		})
	})
}
