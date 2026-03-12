package graph

import (
	"os"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/data"
)

/*
TestResidueReasoning proves that when we strip away the known context, the remaining
unexplainable bits (the residue) represent the strict geometric deviation. We can then
feed ONLY those isolated, naked bits back into the Matrix to see what other chords
they align with, proving analogical resonance.
*/
func TestResidueReasoning(t *testing.T) {
	_, err := os.ReadFile("../../cmd/cfg/alice.txt")
	if err != nil {
		t.Skip("alice.txt not found")
	}

	substitutionPairs := []struct {
		contextPhrase     string
		novelPhrase       string
		expectedResonance string
		distractors       []string
		description       string
	}{
		{"the White Rabbit", "the Black Rabbit", "Black", []string{"White", "Rabbit", "darkness", "Alice"}, "adjective swap on known entity"},
		{"the Queen of Hearts", "the Queen of Spades", "Spades", []string{"Queen", "Hearts", "Cards", "Alice"}, "noun swap in known phrase"},
		{"Drink me", "Eat me", "Eat", []string{"Drink", "me", "bottle", "Alice"}, "verb swap in imperative"},
		{"the Cheshire Cat", "the Cheshire Dog", "Dog", []string{"Cheshire", "Cat", "smile", "Alice"}, "hyponym swap"},
		{"curiouser and curiouser", "stranger and stranger", "stranger", []string{"curiouser", "curious", "and", "Alice"}, "comparative substitution"},
	}

	Convey("Given prompts that deviate structurally from existing text", t, func() {
		for _, pair := range substitutionPairs {
			Convey(pair.description, func() {
				contextPrompt, _ := data.BuildChord([]byte(pair.contextPhrase))
				novelPrompt, _ := data.BuildChord([]byte(pair.novelPhrase))

				t.Logf("Base Context Size: %d bits", contextPrompt.ActiveCount())
				t.Logf("Novel Prompt Size: %d bits", novelPrompt.ActiveCount())

				Convey("We generate a pure Residue and find its resonance", func() {
					residue := data.ChordHole(&novelPrompt, &contextPrompt)

					t.Logf("Extracted Residue Size (Novelty): %d bits", residue.ActiveCount())
					So(residue.ActiveCount(), ShouldBeGreaterThan, 0)
					So(residue.ActiveCount(), ShouldBeLessThan, novelPrompt.ActiveCount())

					testPhrases := append([]string{pair.expectedResonance}, pair.distractors...)
					testChords := make([]data.Chord, len(testPhrases))
					for i, phrase := range testPhrases {
						testChords[i], _ = data.BuildChord([]byte(phrase))
					}

					bestMatchIdx := -1
					highestSimilarity := -1
					for i, chord := range testChords {
						sim := data.ChordSimilarity(&residue, &chord)
						t.Logf("  - Vs %-15q : %d bits overlap", testPhrases[i], sim)
						if sim > highestSimilarity {
							highestSimilarity = sim
							bestMatchIdx = i
						}
					}

					t.Logf("Highest Resonance: %q (%d bits)", testPhrases[bestMatchIdx], highestSimilarity)
					So(testPhrases[bestMatchIdx], ShouldEqual, pair.expectedResonance)
				})
			})
		}
	})
}
