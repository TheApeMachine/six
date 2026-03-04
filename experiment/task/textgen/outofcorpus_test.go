package textgen

import (
	"fmt"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/resonance"
)

func conceptWord(wordIndex int) data.Chord {
	var c data.Chord
	offset := wordIndex * 16
	for i := 0; i < 16; i++ {
		bit := offset + i
		c[bit/64] |= 1 << uint(bit%64)
	}
	return c
}

/*
TestOutOfCorpusCompletion tests compositional inference on a richer
knowledge base with multiple entities and properties.

Knowledge Base (8 facts, 15 concepts):
  F0: cat    + likes + fish
  F1: dog    + likes + bones
  F2: parrot + likes + seeds
  F3: shark  + likes + plankton
  F4: frog   + likes + flies
  F5: cat    + hates + water
  F6: dog    + hates + thunder
  F7: parrot + hates + noise

3 Out-of-Corpus Queries:
  Q1: "shark hates ?" — via dog bridge    → thunder
  Q2: "frog hates ?"  — via cat bridge    → water
  Q3: "frog hates ?"  — via parrot bridge → noise

The key insight: DIFFERENT analogy bridges yield DIFFERENT (but
structurally valid) inferences for the SAME query. This demonstrates
the richness of compositional reasoning — the answer depends on
which structural pattern you follow.
*/
func TestOutOfCorpusCompletion(t *testing.T) {
	Convey("Given a knowledge base of 8 facts about 5 animals", t, func() {

		// 15-word vocabulary, each word gets a unique 16-bit block
		vocab := map[string]int{
			"cat": 0, "dog": 1, "parrot": 2, "shark": 3, "frog": 4,
			"likes": 5, "hates": 6,
			"fish": 7, "bones": 8, "seeds": 9, "plankton": 10, "flies": 11,
			"water": 12, "thunder": 13, "noise": 14,
		}

		wc := make(map[string]data.Chord)
		for word, idx := range vocab {
			wc[word] = conceptWord(idx)
		}

		or := func(words ...string) data.Chord {
			var c data.Chord
			for _, w := range words {
				chord := wc[w]
				for j := range c {
					c[j] |= chord[j]
				}
			}
			return c
		}

		// Knowledge base
		facts := map[string]data.Chord{
			"cat likes fish":       or("cat", "likes", "fish"),
			"dog likes bones":      or("dog", "likes", "bones"),
			"parrot likes seeds":   or("parrot", "likes", "seeds"),
			"shark likes plankton": or("shark", "likes", "plankton"),
			"frog likes flies":     or("frog", "likes", "flies"),
			"cat hates water":      or("cat", "hates", "water"),
			"dog hates thunder":    or("dog", "hates", "thunder"),
			"parrot hates noise":   or("parrot", "hates", "noise"),
		}

		// All possible completion candidates
		objects := []string{"fish", "bones", "seeds", "plankton", "flies", "water", "thunder", "noise"}

		// Helper: run one inference and return the predicted word
		infer := func(label string, F1name, F2name, F3name, queryStr string) string {
			F1 := facts[F1name]
			F2 := facts[F2name]
			F3 := facts[F3name]
			query := or(splitWords(queryStr)...)

			H := resonance.TransitiveResonance(&F1, &F2, &F3)

			// Novel bits = H minus query minus F3
			novelBits := data.ChordHole(&H, &query)
			novelBits = data.ChordHole(&novelBits, &F3)

			bestWord := ""
			bestScore := -1.0

			for _, word := range objects {
				chord := wc[word]
				sim := data.ChordSimilarity(&novelBits, &chord)
				score := float64(sim) / float64(chord.ActiveCount())
				if score > bestScore {
					bestScore = score
					bestWord = word
				}
			}

			fmt.Printf("  %s\n", label)
			fmt.Printf("    Bridge: %s ↔ %s ↔ %s\n", F1name, F2name, F3name)
			fmt.Printf("    Novel bits: %d → predicted: \"%s\" (score=%.2f)\n\n",
				novelBits.ActiveCount(), bestWord, bestScore)

			return bestWord
		}

		Convey("When running 3 out-of-corpus queries via different analogy bridges", func() {
			fmt.Printf("\n╔══════════════════════════════════════════════════════════════╗\n")
			fmt.Printf("║        COMPLEX OUT-OF-CORPUS INFERENCE                      ║\n")
			fmt.Printf("║        8 facts, 15 concepts, 3 queries                      ║\n")
			fmt.Printf("╠══════════════════════════════════════════════════════════════╣\n")
			fmt.Printf("║  Knowledge Base:                                            ║\n")
			fmt.Printf("║    cat    likes fish      cat    hates water                ║\n")
			fmt.Printf("║    dog    likes bones     dog    hates thunder              ║\n")
			fmt.Printf("║    parrot likes seeds     parrot hates noise                ║\n")
			fmt.Printf("║    shark  likes plankton                                    ║\n")
			fmt.Printf("║    frog   likes flies                                       ║\n")
			fmt.Printf("╠══════════════════════════════════════════════════════════════╣\n\n")

			// Q1: "shark hates ?" via the DOG bridge
			// dog:shark share "likes" verb → transfer dog's "hates" property
			// TransitiveResonance(dog hates thunder, dog likes bones, shark likes plankton)
			fmt.Printf("  Q1: \"shark hates ?\" (NEVER STORED)\n")
			q1 := infer(
				"Via dog→shark bridge (both 'like' something)",
				"dog hates thunder",   // F1: source of property to transfer
				"dog likes bones",     // F2: bridge fact (shares "dog" with F1)
				"shark likes plankton", // F3: target (shares "likes" with F2)
				"shark hates",
			)

			// Q2: "frog hates ?" via the CAT bridge
			// cat:frog share "likes" verb → transfer cat's "hates" property
			fmt.Printf("  Q2: \"frog hates ?\" (NEVER STORED) — via cat bridge\n")
			q2 := infer(
				"Via cat→frog bridge (both 'like' something)",
				"cat hates water",    // F1
				"cat likes fish",     // F2
				"frog likes flies",   // F3
				"frog hates",
			)

			// Q3: "frog hates ?" via the PARROT bridge — SAME QUERY, DIFFERENT BRIDGE
			fmt.Printf("  Q3: \"frog hates ?\" (SAME QUERY) — via parrot bridge\n")
			q3 := infer(
				"Via parrot→frog bridge (both 'like' something)",
				"parrot hates noise",   // F1
				"parrot likes seeds",   // F2
				"frog likes flies",     // F3
				"frog hates",
			)

			fmt.Printf("╔══════════════════════════════════════════════════════════════╗\n")
			fmt.Printf("║  Summary:                                                   ║\n")
			fmt.Printf("║    Q1: shark hates → %-8s (via dog bridge)               ║\n", q1)
			fmt.Printf("║    Q2: frog  hates → %-8s (via cat bridge)               ║\n", q2)
			fmt.Printf("║    Q3: frog  hates → %-8s (via parrot bridge)            ║\n", q3)
			fmt.Printf("║                                                             ║\n")
			fmt.Printf("║  Same query + different bridge = different valid answer!     ║\n")
			fmt.Printf("╚══════════════════════════════════════════════════════════════╝\n")

			Convey("Then each query correctly transfers the bridge animal's hate-property", func() {
				So(q1, ShouldEqual, "thunder") // shark inherits dog's fear
				So(q2, ShouldEqual, "water")   // frog inherits cat's fear
				So(q3, ShouldEqual, "noise")   // frog inherits parrot's fear
			})
		})
	})
}

// splitWords is a simple space-based word splitter.
func splitWords(s string) []string {
	var words []string
	word := ""
	for _, c := range s {
		if c == ' ' {
			if word != "" {
				words = append(words, word)
			}
			word = ""
		} else {
			word += string(c)
		}
	}
	if word != "" {
		words = append(words, word)
	}
	return words
}
