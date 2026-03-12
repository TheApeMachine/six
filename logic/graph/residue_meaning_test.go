package graph

import (
"os"
"testing"

. "github.com/smartystreets/goconvey/convey"
"github.com/theapemachine/six/data"
)

func TestUselessResidueDepletion(t *testing.T) {
	raw, err := os.ReadFile("../../cmd/cfg/alice.txt")
	if err != nil {
		t.Skip("alice.txt not found")
	}

	chunks := tokenize(raw)
	paths, err := buildPaths(chunks)
	if err != nil {
		t.Fatal(err)
	}
	matrix := NewMatrixServer()

	novelSentences := []struct {
		text        string
		description string
	}{
		{"Alice was beginning to get very tired of this cyberpunk nonsense", "single novel phrase at end"},
		{"cyberpunk Alice was beginning to get very tired of sitting by her sister", "novel at start"},
		{"Alice was beginning to get very tired of this quantum encryption nonsense", "longer novel phrase"},
		{"The Queen of Hearts screamed cyberspace and threw her crown at Alice", "novel embedded mid-sentence"},
		{"Down down down the rabbit hole went the neural network", "novel concept replacing familiar"},
		{"Alice had never been to a symposium before", "minimal novelty in familiar structure"},
		{"The Mad Hatter poured tea for the holographic avatar", "genre mashup mid-phrase"},
	}

	Convey("Given sentences WITH NOVELTY at varying positions", t, func() {
		for _, testCase := range novelSentences {
			Convey(testCase.description, func() {
				prompt, _ := data.BuildChord([]byte(testCase.text))
				startBits := prompt.ActiveCount()

				Convey("Keep removing shared components with LSM branches until depleted", func() {
					var branches []data.Chord
					for _, path := range paths {
						if data.ChordSimilarity(&prompt, &path) > 3 {
							branches = append(branches, path)
						}
					}

					residue := prompt
					steps := 0

					t.Logf("Initial residue: %d bits -> Text: %q", startBits, testCase.text)

					for {
						bestIdx, matchEnergy, _ := matrix.Evaluate(residue, branches)

						if bestIdx == -1 {
							break
						}

						match := branches[bestIdx]
						nextResidue := data.ChordHole(&residue, &match)

						if nextResidue.ActiveCount() == residue.ActiveCount() {
							break
						}

						t.Logf("Step %d: removed %d shared bits via branch match (Evaluate energy=%d)",
							steps+1, residue.ActiveCount()-nextResidue.ActiveCount(), matchEnergy)

						residue = nextResidue
						steps++

						if residue.ActiveCount() == 0 {
							break
						}

						branches = append(branches[:bestIdx], branches[bestIdx+1:]...)
					}

					endBits := residue.ActiveCount()
					t.Logf("Final run result: depleted from %d to %d bits in %d steps", startBits, endBits, steps)

					if endBits > 0 {
						t.Logf("Remaining Unexplainable Residue: %d bits", endBits)
					} else {
						t.Logf("Residue completely explained and depleted by branches!")
					}

					So(endBits, ShouldBeGreaterThanOrEqualTo, 0)
					So(steps, ShouldBeGreaterThan, 0)
				})
			})
		}
	})

	Convey("Given a sentence with STRICT branch filtering (similarity > 8)", t, func() {
		text := "Alice was beginning to get very tired of this cyberpunk nonsense"
		prompt, _ := data.BuildChord([]byte(text))
		startBits := prompt.ActiveCount()

		var branches []data.Chord
		for _, path := range paths {
			if data.ChordSimilarity(&prompt, &path) > 8 {
				branches = append(branches, path)
			}
		}
		initialBranches := len(branches)

		residue := prompt
		steps := 0
		for {
			bestIdx, _, _ := matrix.Evaluate(residue, branches)
			if bestIdx == -1 {
				break
			}
			match := branches[bestIdx]
			nextResidue := data.ChordHole(&residue, &match)
			if nextResidue.ActiveCount() == residue.ActiveCount() {
				break
			}
			residue = nextResidue
			steps++
			if residue.ActiveCount() == 0 {
				break
			}
			branches = append(branches[:bestIdx], branches[bestIdx+1:]...)
		}

		endBits := residue.ActiveCount()
		t.Logf("Strict filter: %d qualifying branches, depleted %d -> %d bits in %d steps", initialBranches, startBits, endBits, steps)
		So(endBits, ShouldBeGreaterThan, 0)
	})
}
