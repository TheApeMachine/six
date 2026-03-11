package graph

import (
	"math"
	"testing"

	capnp "capnproto.org/go/capnp/v3"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
)

// EvaluateMatrix simulates the ALU hardware loop:
// Blasting a live prompt against a contiguous array of stored paths
// to find the lowest active residue via XOR + POPCNT.
func EvaluateMatrix(prompt data.Chord, paths []data.Chord) (bestIdx int, lowestEnergy int, residue data.Chord) {
	lowestEnergy = math.MaxInt32
	bestIdx = -1

	for i, path := range paths {
		// Hardware POPCNT ( Context ⊕ PathMatrix[i] )
		res := prompt.XOR(path)
		energy := res.ActiveCount()

		if energy < lowestEnergy {
			lowestEnergy = energy
			bestIdx = i
			residue = res
		}
	}
	return bestIdx, lowestEnergy, residue
}

// lexPrime allocates 5 disjoint prime indices per token. Tokens are assigned
// in order; indices stay within 0..256 (257-bit logical width).
func lexPrime(tokenIdx int) []int {
	base := tokenIdx * 5
	out := make([]int, 5)
	for i := range 5 {
		out[i] = (base + i) % 257
	}
	return out
}

/*
stressLexicon builds a large token set: actors, locations, hubs, objects, query words.
Each token gets 5 disjoint primes. Returns makeChord(tokens...) and the prime map for assertions.
*/
func stressLexicon(seg *capnp.Segment) (makeChord func(...string) data.Chord, primes map[string][]int) {
	tokens := []string{
		"Sandra", "Roy", "Charles", "Alice", "Bob", "Eve", "Dave", "Frank", "Grace", "Henry",
		"Garden", "Kitchen", "Office", "Garage", "Park", "Beach", "Street", "Mall", "Library", "Cafe",
		"is_in_the", "likes", "works_at", "lives_in", "went_to", "ate", "bought", "gave_to",
		"Pizza", "Coffee", "Book", "Car", "Bike", "Cake", "Salad", "Soup", "Tea", "Wine",
		"Where", "What", "Who", "When", "Why", "How",
	}

	primes = make(map[string][]int)
	for idx, tok := range tokens {
		primes[tok] = lexPrime(idx)
	}

	makeChord = func(tokens ...string) data.Chord {
		c, _ := data.NewChord(seg)
		for _, tok := range tokens {
			for _, p := range primes[tok] {
				c.Set(p)
			}
		}
		return c
	}
	return makeChord, primes
}

func TestMatrixServer_DeepGeometricInference(t *testing.T) {
	Convey("Given a populated Matrix Cortex with overlapping facts and distractors", t, func() {
		_, seg, _ := capnp.NewMessage(capnp.MultiSegment(nil))

		primes := map[string][]int{
			"Sandra":    {2, 3, 5, 7, 11},
			"Roy":       {13, 17, 19, 23, 29},
			"Charles":   {31, 37, 41, 43, 47},
			"Garden":    {53, 59, 61, 67, 71},
			"Kitchen":   {73, 79, 83, 89, 97},
			"Pizza":     {101, 103, 107, 109, 113},
			"Where":     {127, 131, 137, 139, 149},
			"What":      {151, 157, 163, 167, 173},
			"is_in_the": {179, 181, 191, 193, 197},
			"likes":     {199, 211, 223, 227, 229},
		}

		makeChord := func(tokens ...string) data.Chord {
			c, _ := data.NewChord(seg)
			for _, tok := range tokens {
				for _, p := range primes[tok] {
					c.Set(p)
				}
			}
			return c
		}

		matrix := []data.Chord{
			makeChord("Sandra", "is_in_the", "Garden"),
			makeChord("Roy", "is_in_the", "Kitchen"),
			makeChord("Charles", "is_in_the", "Kitchen"),
			makeChord("Roy", "likes", "Pizza"),
			makeChord("Sandra", "likes", "Pizza"),
		}

		Convey("1. Disambiguating Intersecting Hubs (The Matrix Competition)", func() {
			prompt := makeChord("Where", "is_in_the", "Roy")
			bestIdx, lowestEnergy, residue := EvaluateMatrix(prompt, matrix)

			Convey("It must select Index 1, ignoring Charles in the Kitchen and Roy's Pizza", func() {
				So(bestIdx, ShouldEqual, 1)
				So(lowestEnergy, ShouldEqual, 10)
				for _, p := range primes["Where"] {
					So(residue.Has(p), ShouldBeTrue)
				}
				for _, p := range primes["Kitchen"] {
					So(residue.Has(p), ShouldBeTrue)
				}
				wrongResidue := prompt.XOR(matrix[3])
				So(wrongResidue.ActiveCount(), ShouldEqual, 20)
				So(wrongResidue.ActiveCount(), ShouldBeGreaterThan, lowestEnergy)
			})
		})

		Convey("2. Extracting Attributes via Context Swap", func() {
			prompt := makeChord("What", "likes", "Sandra")
			bestIdx, lowestEnergy, residue := EvaluateMatrix(prompt, matrix)

			Convey("It isolates Sandra's preference, untouched by Roy's identical preference", func() {
				So(bestIdx, ShouldEqual, 4)
				So(lowestEnergy, ShouldEqual, 10)
				for _, p := range primes["Pizza"] {
					So(residue.Has(p), ShouldBeTrue)
				}
			})
		})

		Convey("3. Holographic Fault Tolerance (The Typo Test)", func() {
			corruptedCharles, _ := data.NewChord(seg)
			corruptedCharles.Set(31)
			corruptedCharles.Set(37)
			corruptedCharles.Set(41)
			corruptedCharles.Set(233)
			corruptedCharles.Set(239)

			prompt, _ := data.NewChord(seg)
			for _, p := range primes["Where"] {
				prompt.Set(p)
			}
			for _, p := range primes["is_in_the"] {
				prompt.Set(p)
			}
			prompt = prompt.XOR(corruptedCharles)

			bestIdx, lowestEnergy, _ := EvaluateMatrix(prompt, matrix)

			Convey("The geometric center of gravity still pulls it to the exact correct path", func() {
				So(bestIdx, ShouldEqual, 2)
				So(lowestEnergy, ShouldEqual, 14)
				wrongEnergy := prompt.XOR(matrix[1]).ActiveCount()
				So(wrongEnergy, ShouldEqual, 20)
				So(lowestEnergy, ShouldBeLessThan, wrongEnergy)
			})
		})
	})
}

func TestMatrixServer_StressGeometricInference(t *testing.T) {
	Convey("Given a large Matrix Cortex with many premises and heavy contextual overlap", t, func() {
		_, seg, _ := capnp.NewMessage(capnp.MultiSegment(nil))

		makeChord, primes := stressLexicon(seg)

		// 50+ paths: dense hub branching, multi-relation actors, cross-domain overlap.
		matrix := []data.Chord{
			// Location hub: is_in_the — 10 paths, many sharing locations
			makeChord("Sandra", "is_in_the", "Garden"),
			makeChord("Roy", "is_in_the", "Kitchen"),
			makeChord("Charles", "is_in_the", "Kitchen"),
			makeChord("Alice", "is_in_the", "Office"),
			makeChord("Bob", "is_in_the", "Garage"),
			makeChord("Eve", "is_in_the", "Park"),
			makeChord("Dave", "is_in_the", "Beach"),
			makeChord("Frank", "is_in_the", "Kitchen"),   // Kitchen: Roy, Charles, Frank
			makeChord("Grace", "is_in_the", "Library"),
			makeChord("Henry", "is_in_the", "Cafe"),
			// Preference hub: likes — 10 paths
			makeChord("Roy", "likes", "Pizza"),
			makeChord("Sandra", "likes", "Pizza"),
			makeChord("Charles", "likes", "Coffee"),
			makeChord("Alice", "likes", "Book"),
			makeChord("Bob", "likes", "Pizza"),
			makeChord("Eve", "likes", "Cake"),
			makeChord("Dave", "likes", "Soup"),
			makeChord("Frank", "likes", "Tea"),
			makeChord("Grace", "likes", "Wine"),
			makeChord("Henry", "likes", "Coffee"),
			// Works hub — 5 paths
			makeChord("Roy", "works_at", "Office"),
			makeChord("Alice", "works_at", "Library"),
			makeChord("Bob", "works_at", "Cafe"),
			makeChord("Eve", "works_at", "Mall"),
			makeChord("Dave", "works_at", "Street"),
			// Lives hub — 5 paths
			makeChord("Sandra", "lives_in", "Garden"),
			makeChord("Charles", "lives_in", "Kitchen"),
			makeChord("Frank", "lives_in", "Garage"),
			makeChord("Grace", "lives_in", "Beach"),
			makeChord("Henry", "lives_in", "Park"),
			// Went_to hub — 5 paths
			makeChord("Roy", "went_to", "Mall"),
			makeChord("Alice", "went_to", "Beach"),
			makeChord("Bob", "went_to", "Park"),
			makeChord("Eve", "went_to", "Cafe"),
			makeChord("Dave", "went_to", "Library"),
			// Ate hub — 5 paths
			makeChord("Sandra", "ate", "Pizza"),
			makeChord("Charles", "ate", "Salad"),
			makeChord("Frank", "ate", "Cake"),
			makeChord("Grace", "ate", "Soup"),
			makeChord("Henry", "ate", "Tea"),
			// Bought hub — 5 paths
			makeChord("Roy", "bought", "Car"),
			makeChord("Alice", "bought", "Bike"),
			makeChord("Bob", "bought", "Book"),
			makeChord("Eve", "bought", "Wine"),
			makeChord("Dave", "bought", "Coffee"),
		}

		Convey("4. Dense Hub Competition — Where is Frank?", func() {
			// Frank appears in: is_in_the Kitchen (idx 7), likes Tea (idx 17), lives_in Garage (idx 27), ate Cake (idx 37).
			// Location query must win over preference/action.
			prompt := makeChord("Where", "is_in_the", "Frank")
			bestIdx, lowestEnergy, residue := EvaluateMatrix(prompt, matrix)

			Convey("It selects Frank is_in_the Kitchen, not his other facts", func() {
				So(bestIdx, ShouldEqual, 7)
				So(lowestEnergy, ShouldEqual, 10)
				for _, p := range primes["Where"] {
					So(residue.Has(p), ShouldBeTrue)
				}
				for _, p := range primes["Kitchen"] {
					So(residue.Has(p), ShouldBeTrue)
				}
			})
		})

		Convey("5. Cross-Domain Overlap — Roy in location vs preference vs work", func() {
			// Roy: is_in_the Kitchen (1), likes Pizza (10), works_at Office (20).
			// Prompt: Where is Roy? must disambiguate from likes/works.
			prompt := makeChord("Where", "is_in_the", "Roy")
			bestIdx, lowestEnergy, _ := EvaluateMatrix(prompt, matrix)

			Convey("It selects Roy is_in_the Kitchen despite Roy likes Pizza and works_at Office", func() {
				So(bestIdx, ShouldEqual, 1)
				So(lowestEnergy, ShouldEqual, 10)
			})
		})

		Convey("6. Same Actor Multiple Relations — What does Henry like?", func() {
			// Henry: is_in_the Cafe (9), likes Coffee (19), lives_in Park (29), ate Tea (39).
			prompt := makeChord("What", "likes", "Henry")
			bestIdx, lowestEnergy, residue := EvaluateMatrix(prompt, matrix)

			Convey("It isolates Henry likes Coffee", func() {
				So(bestIdx, ShouldEqual, 19)
				So(lowestEnergy, ShouldEqual, 10)
				for _, p := range primes["Coffee"] {
					So(residue.Has(p), ShouldBeTrue)
				}
			})
		})

		Convey("7. Shared Location Disambiguation — Who is in the Kitchen?", func() {
			// Kitchen: Roy (1), Charles (2), Frank (7). Prompt: Who is_in_the Kitchen?
			prompt := makeChord("Who", "is_in_the", "Kitchen")
			bestIdx, lowestEnergy, residue := EvaluateMatrix(prompt, matrix)

			Convey("It selects one Kitchen occupant; residue encodes the actor", func() {
				So(bestIdx, ShouldBeIn, 1, 2, 7)
				So(lowestEnergy, ShouldEqual, 10)
				So(residue.ActiveCount(), ShouldEqual, 10)
			})
		})

		Convey("8. Prompt Contextual Overlap — partial match stresses ordering", func() {
			// Prompt shares "Sandra" and "Garden" with both:
			// - Sandra is_in_the Garden (0)
			// - Sandra lives_in Garden (25)
			// Adding "is_in_the" forces location-reading.
			prompt := makeChord("Where", "is_in_the", "Sandra")
			bestIdx, lowestEnergy, residue := EvaluateMatrix(prompt, matrix)

			Convey("It selects Sandra is_in_the Garden, not lives_in", func() {
				So(bestIdx, ShouldEqual, 0)
				So(lowestEnergy, ShouldEqual, 10)
				for _, p := range primes["Garden"] {
					So(residue.Has(p), ShouldBeTrue)
				}
			})
		})

		Convey("9. Near-Tie Stress — two paths with minimal residue difference", func() {
			// Add a path that almost matches: Roy is_in_the Office (fake, but Office close to Kitchen in some embedding?).
			// Instead: create two near-identical paths. Roy+is_in_the+Kitchen vs Roy+is_in_the+Garage.
			dupMatrix := append(matrix, makeChord("Roy", "is_in_the", "Garage"))
			prompt := makeChord("Where", "is_in_the", "Roy")

			bestIdx, lowestEnergy, _ := EvaluateMatrix(prompt, dupMatrix)

			Convey("It selects one of the Roy location paths; both have residue 10", func() {
				So(bestIdx, ShouldBeIn, 1, len(dupMatrix)-1)
				So(lowestEnergy, ShouldEqual, 10)
			})
		})

		Convey("10. Holographic Fault Tolerance at Scale", func() {
			corruptedFrank, _ := data.NewChord(seg)
			corruptedFrank.Set(primes["Frank"][0])
			corruptedFrank.Set(primes["Frank"][1])
			corruptedFrank.Set(primes["Frank"][2])
			corruptedFrank.Set(240)
			corruptedFrank.Set(245)

			prompt, _ := data.NewChord(seg)
			for _, p := range primes["Where"] {
				prompt.Set(p)
			}
			for _, p := range primes["is_in_the"] {
				prompt.Set(p)
			}
			prompt = prompt.XOR(corruptedFrank)

			bestIdx, lowestEnergy, _ := EvaluateMatrix(prompt, matrix)

			Convey("Corrupted Frank still resolves to Frank is_in_the Kitchen", func() {
				So(bestIdx, ShouldEqual, 7)
				So(lowestEnergy, ShouldEqual, 14)
			})
		})

		Convey("11. Multi-Premise Analogical Resolution", func() {
			// Prompt: Where did Roy go? — Roy went_to Mall (30)
			// Must beat: Roy is_in_the Kitchen, Roy likes Pizza, Roy works_at Office, Roy bought Car.
			prompt := makeChord("Where", "went_to", "Roy")
			bestIdx, lowestEnergy, residue := EvaluateMatrix(prompt, matrix)

			Convey("It selects Roy went_to Mall", func() {
				So(bestIdx, ShouldEqual, 30)
				So(lowestEnergy, ShouldEqual, 10)
				for _, p := range primes["Mall"] {
					So(residue.Has(p), ShouldBeTrue)
				}
			})
		})

		Convey("12. Long-Chain Consistency", func() {
			// 5-token path: Alice works_at Library, Bob works_at Cafe. Query: Who works_at Library?
			prompt := makeChord("Who", "works_at", "Library")
			bestIdx, lowestEnergy, residue := EvaluateMatrix(prompt, matrix)

			Convey("It selects Alice works_at Library", func() {
				So(bestIdx, ShouldEqual, 21)
				So(lowestEnergy, ShouldEqual, 10)
				for _, p := range primes["Alice"] {
					So(residue.Has(p), ShouldBeTrue)
				}
			})
		})
	})
}

func BenchmarkEvaluateMatrix(b *testing.B) {
	_, seg, _ := capnp.NewMessage(capnp.MultiSegment(nil))
	makeChord, _ := stressLexicon(seg)

	paths := make([]data.Chord, 1000)
	for i := range paths {
		paths[i] = makeChord("Sandra", "is_in_the", "Garden")
	}

	prompt := makeChord("Where", "is_in_the", "Sandra")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EvaluateMatrix(prompt, paths)
	}
}
