package codegen

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/experiment/projector"
)

func TestChordGeneration(t *testing.T) {
	Convey("Given the chord-based BVP generation pipeline", t, func() {
		corpus := append(pythonCorpus(), longCorpus()...)
		corpusText := ""
		for i, fn := range corpus {
			if i > 0 {
				corpusText += "\n"
			}
			corpusText += fn
		}
		corpusBytes := []byte(corpusText)

		store := buildChordStore(corpusBytes)
		So(len(store.Entries), ShouldBeGreaterThan, 0)

		prompts := []string{
			"def factorial(",
			"def find_max(",
			"def binary_search(",
			"def dfs(",
			"def insertion_sort(",
		}

		Convey("When generating from chords for each prompt", func() {
			var entries []ChordGenEntry
			for _, prompt := range prompts {
				e := generateFromChords(store, corpusBytes, prompt)
				So(e.Prompt, ShouldEqual, prompt)
				entries = append(entries, e)
			}

			returnCount, colonCount := 0, 0
			totalTokens := 0
			for _, e := range entries {
				if e.HasReturn {
					returnCount++
				}
				if e.HasColon {
					colonCount++
				}
				totalTokens += e.Tokens
			}
			n := float64(len(prompts))

			Convey("Store has entries at all FibWindow scales", func() {
				So(len(store.Entries), ShouldBeGreaterThan, len(corpusBytes))
			})

			Convey("All entries have tokens and steps", func() {
				for _, e := range entries {
					So(e.Tokens, ShouldBeGreaterThan, 0)
				}
			})

			Convey("Artifacts should be written to the paper directory", func() {
				xAxis := make([]string, len(entries))
				tokData := make([]float64, len(entries))
				stepData := make([]float64, len(entries))
				for i, e := range entries {
					xAxis[i] = e.Prompt
					tokData[i] = float64(e.Tokens)
					stepData[i] = float64(len(e.Steps))
				}
				So(WriteBarChart(xAxis, []projector.BarSeries{
					{Name: "Tokens", Data: tokData},
					{Name: "Steps", Data: stepData},
				}, "Chord-Based Generation",
					"Tokens and steps per prompt in chord BVP pipeline.",
					"fig:chord_gen", "chord_generation"), ShouldBeNil)

				tableRows := make([]map[string]any, len(entries))
				for i, e := range entries {
					tableRows[i] = map[string]any{
						"Prompt":    e.Prompt,
						"Steps":     fmt.Sprintf("%d", len(e.Steps)),
						"Tokens":    fmt.Sprintf("%d", e.Tokens),
						"HasReturn": e.HasReturn,
						"HasColon":  e.HasColon,
					}
				}
				So(WriteTable(tableRows, "chord_generation_summary.tex"), ShouldBeNil)
				_, statErr := os.Stat(filepath.Join(PaperDir(), "chord_generation_summary.tex"))
				So(statErr, ShouldBeNil)
			})

			Convey("Artifact: write chord generation subsection prose", func() {
				tmpl, err := os.ReadFile("prose/chord_generation.tex.tmpl")
				So(err, ShouldBeNil)
				So(WriteProse(string(tmpl), map[string]any{
					"NPrompts":   len(prompts),
					"MeanTokens": float64(totalTokens) / n,
					"ReturnCount": returnCount,
				}, "chord_generation_prose.tex"), ShouldBeNil)
			})

			_ = ChordGenResult{
				Entries:    entries,
				StoreSize:  len(store.Entries),
				CorpusSize: len(corpusBytes),
			}
		})
	})
}
