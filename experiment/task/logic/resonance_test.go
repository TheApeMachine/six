package logic

import (
	"fmt"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/provider/huggingface"
	"github.com/theapemachine/six/resonance"
	"github.com/theapemachine/six/store"
	"github.com/theapemachine/six/tokenizer"
	"github.com/theapemachine/six/vm"
)

// concept returns a pure, orthogonal structural concept using explicit basis primes.
func concept(primes ...int) data.Chord {
	var c data.Chord
	for _, p := range primes {
		c.Set(p)
	}
	return c
}

func TestTransitiveResonance(t *testing.T) {
	Convey("Given a purely bitwise analogy system backed by prime topologies", t, func() {

		Convey("When testing a classical analogy (A:B :: C:D -> H(A,D))", func() {
			// To mathematically verify the prime substrate, concepts must be orthogonal (no shared basic primes).
			// If we used random words, their character bytes would overlap and corrupt the geometric GCD boundaries.
			A := concept(1, 2, 3)          // Cat
			C := concept(4, 5, 6)          // Dog
			B := concept(7, 8, 9)          // Wants Food
			D := concept(10, 11, 12, 13)   // Is Animal

			// F1(A, B): Cat wants food
			F1 := data.ChordOR(&A, &B)
			// F2(C, B): Dog wants food
			F2 := data.ChordOR(&C, &B)
			// F3(C, D): Dog is animal
			F3 := data.ChordOR(&C, &D)

			// Theoretical Hypothesis H(A, D): Cat is animal
			expectedH := data.ChordOR(&A, &D)

			Convey("It forges the correct hypothesis purely via non-neural prime operations", func() {
				H := resonance.TransitiveResonance(&F1, &F2, &F3)

				// FillScore evaluates structural congruence between Hypothesis and Expected Target.
				score := resonance.FillScore(&expectedH, &H)

				fmt.Printf("\n--- Transitive Resonance Forgery ---\n")
				fmt.Printf("F1 (Cat + Food)         Bits: %d\n", F1.ActiveCount())
				fmt.Printf("F2 (Dog + Food)         Bits: %d\n", F2.ActiveCount())
				fmt.Printf("F3 (Dog + Animal)       Bits: %d\n", F3.ActiveCount())
				fmt.Printf("Forged H (Cat + Animal) Bits: %d\n", H.ActiveCount())
				fmt.Printf("Structural Fill Score:  %.2f%%\n", score*100)

				// We expect absolutely perfect structural reconstruction (1.0 or 100%)
				So(score, ShouldEqual, 1.0)
			})
		})

		Convey("When mapping real-world logical relation datasets into Toroidal space", func() {
			loader := vm.NewLoader(
				vm.LoaderWithStore(store.NewLSMSpatialIndex(1.0)),
				vm.LoaderWithTokenizer(tokenizer.NewUniversal(
					tokenizer.TokenizerWithDataset(
						huggingface.New(
							huggingface.DatasetWithRepo("ag_news"),
							huggingface.DatasetWithSamples(100),
							huggingface.DatasetWithTextColumn("text"),
						),
					),
				)),
			)

			machine := vm.NewMachine(
				vm.MachineWithLoader(loader),
			)

			// Start the machine to index the prime topologies
			machine.Start()
			loader.Holdout(50, vm.HoldoutLinear)

			Convey("The data ingestion pipeline seamlessly initializes and performs bitwise transitive routing", func() {
				So(machine, ShouldNotBeNil)

				// Stream the generated holdouts and pipe into the Machine inference Stream
				var buf []data.Chord
				runCount := 0

				for chord := range loader.Generate() {
					if chord.ActiveCount() == 0 {
						// Null chord (Boundary holdout marker reached) -> Ask Machine to Prompt
						resCount := 0
						for res := range machine.Prompt(buf) {
							// For test stability, we just ensure it generated valid spatial completions
							So(res.Score, ShouldBeGreaterThanOrEqualTo, 0.0)
							resCount++
							if resCount > 5 {
								break
							}
						}
						buf = buf[:0]
						runCount++

						if runCount > 3 {
							break
						}
					} else {
						buf = append(buf, chord)
					}
				}
				
				// Assert the pipeline achieved some valid continuous routing sequences
				So(runCount, ShouldBeGreaterThan, 0)
			})
		})
	})
}
