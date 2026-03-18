package data

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
)

/*
TestRoutingStrategies experiments with different Value operations to
route a prompt to the correct sequence among many candidates.
No hard-coded strategy — we try everything the algebra offers and
measure which operations produce correct routing.
*/
func TestRoutingStrategies(t *testing.T) {
	type example struct {
		sequences []string
		prompt    string
		target    int // index of correct sequence
	}

	examples := []example{
		{
			sequences: []string{"Roy is in the Kitchen", "Sandra is in the Garden"},
			prompt:    "Roy is in the ",
			target:    0,
		},
		{
			sequences: []string{"Roy is in the Kitchen", "Sandra is in the Garden"},
			prompt:    "Sandra is in the ",
			target:    1,
		},
		{
			sequences: []string{
				"Roy was in the living room.",
				"Roy is in the kitchen.",
				"If you have an umbrella or stay inside you stay dry.",
			},
			prompt: "Roy was in the ",
			target: 0,
		},
		{
			sequences: []string{
				"Roy was in the living room.",
				"Roy is in the kitchen.",
				"If you have an umbrella or stay inside you stay dry.",
			},
			prompt: "If you have an umbrella or ",
			target: 2,
		},
		{
			sequences: []string{
				"The cat sat on the mat.",
				"The dog ran in the park.",
				"The bird flew over the tree.",
			},
			prompt: "The dog ran in ",
			target: 1,
		},
		{
			sequences: []string{
				"Alice went to the store to buy milk.",
				"Bob went to the gym to exercise.",
				"Charlie went to the office to work.",
			},
			prompt: "Bob went to the gym ",
			target: 1,
		},
		{
			sequences: []string{
				"The quick brown fox jumps over the lazy dog.",
				"A slow red cat sleeps under the busy frog.",
			},
			prompt: "A slow red cat ",
			target: 1,
		},
		{
			sequences: []string{
				"Water boils at one hundred degrees celsius.",
				"Ice melts at zero degrees celsius.",
				"Steam condenses at one hundred degrees celsius.",
			},
			prompt: "Ice melts at ",
			target: 1,
		},
		{
			sequences: []string{
				"Water boils at one hundred degrees celsius.",
				"Ice melts at zero degrees celsius.",
				"Steam condenses at one hundred degrees celsius.",
			},
			prompt: "Steam condenses at ",
			target: 2,
		},
		{
			sequences: []string{
				"Monday is the first day of the week.",
				"Sunday is the last day of the week.",
				"Wednesday is the middle of the week.",
			},
			prompt: "Wednesday is the ",
			target: 2,
		},
		{
			sequences: []string{
				"Red is a warm color.",
				"Blue is a cool color.",
				"Green is a natural color.",
				"Yellow is a bright color.",
				"Purple is a royal color.",
			},
			prompt: "Purple is a ",
			target: 4,
		},
		{
			sequences: []string{
				"Red is a warm color.",
				"Blue is a cool color.",
				"Green is a natural color.",
				"Yellow is a bright color.",
				"Purple is a royal color.",
			},
			prompt: "Green is a ",
			target: 2,
		},
		{
			sequences: []string{
				"One plus one equals two.",
				"Two plus two equals four.",
				"Three plus three equals six.",
				"Four plus four equals eight.",
				"Five plus five equals ten.",
			},
			prompt: "Four plus four ",
			target: 3,
		},
		{
			sequences: []string{
				"Alpha is the first letter.",
				"Beta is the second letter.",
				"Gamma is the third letter.",
				"Delta is the fourth letter.",
				"Epsilon is the fifth letter.",
				"Zeta is the sixth letter.",
				"Eta is the seventh letter.",
			},
			prompt: "Epsilon is the ",
			target: 4,
		},
	}

	gc.Convey("Given sequences and prompts, test routing strategies", t, func() {
		strategies := map[string]func(promptUnion Value, seqUnions []Value) int{
			"XOR-energy": func(promptUnion Value, seqUnions []Value) int {
				bestIdx := -1
				bestEnergy := 999999

				for i, su := range seqUnions {
					energy := promptUnion.XOR(su).ActiveCount()

					if energy < bestEnergy {
						bestEnergy = energy
						bestIdx = i
					}
				}

				return bestIdx
			},
			"Similarity": func(promptUnion Value, seqUnions []Value) int {
				bestIdx := -1
				bestSim := -1

				for i, su := range seqUnions {
					sim := promptUnion.Similarity(su)

					if sim > bestSim {
						bestSim = sim
						bestIdx = i
					}
				}

				return bestIdx
			},
			"Hole-residue-sim": func(promptUnion Value, seqUnions []Value) int {
				// Hole out prompt from each sequence, pick smallest residue
				bestIdx := -1
				bestResidual := 999999

				for i, su := range seqUnions {
					residue := su.Hole(promptUnion)
					residual := residue.ActiveCount()

					if residual < bestResidual {
						bestResidual = residual
						bestIdx = i
					}
				}

				return bestIdx
			},
			"AND-overlap": func(promptUnion Value, seqUnions []Value) int {
				bestIdx := -1
				bestOverlap := -1

				for i, su := range seqUnions {
					overlap := promptUnion.AND(su).ActiveCount()

					if overlap > bestOverlap {
						bestOverlap = overlap
						bestIdx = i
					}
				}

				return bestIdx
			},
			"Hole-unique-match": func(promptUnion Value, seqUnions []Value) int {
				// For each sequence, compute its unique bits (Hole against others)
				// Then match prompt against each unique label
				bestIdx := -1
				bestSim := -1

				for i, su := range seqUnions {
					othersUnion := MustNewValue()

					for j, other := range seqUnions {
						if j != i {
							othersUnion = othersUnion.OR(other)
						}
					}

					unique := su.Hole(othersUnion)
					sim := promptUnion.Similarity(unique)

					if sim > bestSim {
						bestSim = sim
						bestIdx = i
					}
				}

				return bestIdx
			},
			"XOR-then-Hole": func(promptUnion Value, seqUnions []Value) int {
				// XOR prompt with allUnion, then match residue against Hole labels
				allUnion := MustNewValue()

				for _, su := range seqUnions {
					allUnion = allUnion.OR(su)
				}

				promptResidue := promptUnion.XOR(allUnion)
				bestIdx := -1
				bestSim := -1

				for i, su := range seqUnions {
					othersUnion := MustNewValue()

					for j, other := range seqUnions {
						if j != i {
							othersUnion = othersUnion.OR(other)
						}
					}

					unique := su.Hole(othersUnion)
					sim := promptResidue.Similarity(unique)

					if sim > bestSim {
						bestSim = sim
						bestIdx = i
					}
				}

				return bestIdx
			},
			"Rotate-align": func(promptUnion Value, seqUnions []Value) int {
				// Rotate prompt through 128 steps, find min XOR with each seq
				bestIdx := -1
				bestEnergy := 999999

				for i, su := range seqUnions {
					rotated := promptUnion

					for step := 0; step < 128; step++ {
						energy := rotated.XOR(su).ActiveCount()

						if energy < bestEnergy {
							bestEnergy = energy
							bestIdx = i
						}

						rotated = rotated.Rotate3D()
					}
				}

				return bestIdx
			},
			"Evaluate-interest": func(promptUnion Value, seqUnions []Value) int {
				// Use prompt as interest bias on XOR energy
				bestIdx := -1
				bestEnergy := 999999

				for i, su := range seqUnions {
					xorDelta := promptUnion.XOR(su)
					energy := xorDelta.ActiveCount()

					resonance := su.AND(promptUnion)
					energy -= resonance.ActiveCount()

					if energy < bestEnergy {
						bestEnergy = energy
						bestIdx = i
					}
				}

				return bestIdx
			},
		}

		for name, strategy := range strategies {
			correct := 0

			for _, ex := range examples {
				seqUnions := make([]Value, len(ex.sequences))

				for i, seq := range ex.sequences {
					for _, b := range []byte(seq) {
						seqUnions[i] = seqUnions[i].OR(BaseValue(b))
					}
				}

				var promptUnion Value

				for _, b := range []byte(ex.prompt) {
					promptUnion = promptUnion.OR(BaseValue(b))
				}

				result := strategy(promptUnion, seqUnions)

				if result == ex.target {
					correct++
				}
			}

			t.Logf("  %-25s  %d/%d correct (%.0f%%)",
				name, correct, len(examples),
				float64(correct)/float64(len(examples))*100,
			)
		}

		gc.Convey("Results logged above", func() {
			gc.So(true, gc.ShouldBeTrue)
		})
	})
}
