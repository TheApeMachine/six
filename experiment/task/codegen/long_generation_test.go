package codegen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/provider/huggingface"
	"github.com/theapemachine/six/store"
	"github.com/theapemachine/six/tokenizer"
	"github.com/theapemachine/six/vm"
)

func TestLongGeneration(t *testing.T) {
	Convey("Given a machine loaded with MBPP", t, func() {
		loader := vm.NewLoader(
			vm.LoaderWithStore(store.NewLSMSpatialIndex(1.0)),
			vm.LoaderWithTokenizer(
				tokenizer.NewUniversal(
					tokenizer.TokenizerWithDataset(
						huggingface.New(
							huggingface.DatasetWithRepo("code-rag-bench/mbpp"),
							huggingface.DatasetWithSamples(100),
							huggingface.DatasetWithTextColumn("code"),
						),
					),
				),
			),
		)

		machine := vm.NewMachine(
			vm.MachineWithLoader(loader),
		)

		machine.Start()
		loader.Holdout(50, vm.HoldoutRandom)

		// Consume generator to populate the machine
		<-loader.Generate()

		// Build a coder to reconstruct sequences for AST validation
		coder := tokenizer.NewMortonCoder()
		
		prompts := []struct{ prefix, desc string }{
			{"def quicksort(lst):", "Quicksort"},
			{"def merge_sort(lst):", "Merge sort"},
			{"def dfs(graph, start):", "DFS"},
			{"def bfs(graph, start):", "BFS"},
			{"def bubble_sort(lst):", "Bubble sort"},
			{"def rle_encode(s):", "RLE encode"},
			{"def two_sum(nums, target):", "Two sum"},
		}

		Convey("When generating long programs via Toroidal routing over Chords", func() {
			var results []LongGenEntry
			for _, p := range prompts {
				promptText := []byte(p.prefix)
				
				// Translate the string prompt mathematically into Chords via tokenizer
				var promptChords []data.Chord
				for _, b := range promptText {
					promptChords = append(promptChords, tokenizer.BaseChord(b))
				}
				
				var generatedChords []data.Chord
				
				// Step the actual chord-based machine to generate raw phase sequence
				limit := 0
				for res := range machine.Prompt(promptChords, nil) {
					// We let it run longer for long_generation
					if limit > 200 { 
						break
					}
					// Only chords exist in the generation stream
					generatedChords = append(generatedChords, res.Chord[0])
					limit++
				}

				// Only when generation halts do we re-inflate the topology into bytes
				var generatedBytes []byte
				for _, chord := range generatedChords {
					tokenIDs := loader.Lookup([]data.Chord{chord})
					for _, tokenID := range tokenIDs {
						b, _, _ := coder.Decode(tokenID)
						generatedBytes = append(generatedBytes, b)
					}
				}

				fullText := string(promptText) + string(generatedBytes)

				hasReturn := strings.Contains(fullText, "return")
				hasLoop := strings.Contains(fullText, "for ") || strings.Contains(fullText, "while ")
				hasIf := strings.Contains(fullText, "if ")
				looksValid := isValidSyntax(fullText)

				So(fullText, ShouldNotBeEmpty)

				results = append(results, LongGenEntry{
					Desc: p.desc, Prefix: p.prefix, FullText: fullText,
					TotalTokens: len(promptChords) + len(generatedChords),
					TotalNew: len(generatedChords),
					HasReturn: hasReturn, HasLoop: hasLoop, HasConditional: hasIf,
					LooksValid: looksValid, ReachedReturn: looksValid,
				})
			}

			validCount, returnCount, loopCount := 0, 0, 0
			sumToks := 0
			for _, e := range results {
				if e.LooksValid {
					validCount++
				}
				if e.HasReturn {
					returnCount++
				}
				if e.HasLoop {
					loopCount++
				}
				sumToks += e.TotalTokens
			}
			n := float64(len(prompts))

			Convey("All outputs non-empty", func() {
				for _, e := range results {
					So(e.FullText, ShouldNotBeEmpty)
				}
			})

			Convey("Artifacts should be written to the paper directory", func() {
				xAxis := make([]string, len(results))
				tokData := make([]float64, len(results))
				validData := make([]float64, len(results))
				var codeSections []projector.CodeSection

				for i, e := range results {
					xAxis[i] = fmt.Sprintf("Prompt %d", i+1)
					tokData[i] = float64(e.TotalTokens)
					if e.LooksValid {
						validData[i] = 1.0
					} else {
						validData[i] = 0.0
					}
					codeSections = append(codeSections, projector.CodeSection{
						Prompt: e.Desc,
						Code:   e.FullText,
					})
				}
				
				So(WriteComboChart(xAxis, []projector.ComboSeries{
					{Name: "AST Validation", Type: "bar", Data: validData},
					{Name: "Total Chords", Type: "line", Data: tokData},
				}, "Prompts", "Metrics", 0.0, 260.0, "Long Program Generation (Chord Native)",
					"Validation and length of generated programs over time.",
					"fig:long_gen", "long_generation"), ShouldBeNil)

				So(WriteCodeAppendix(codeSections, "long_generation_code.tex"), ShouldBeNil)

				tableRows := make([]map[string]any, len(results))
				for i, e := range results {
					tableRows[i] = map[string]any{
						"Prompt": e.Desc,
						"Chords": fmt.Sprintf("%d", e.TotalTokens),
						"Return": e.HasReturn, "Loop": e.HasLoop, "Valid": e.LooksValid,
					}
				}
				So(WriteTable(tableRows, "long_generation_summary.tex"), ShouldBeNil)
				_, statErr := os.Stat(filepath.Join(PaperDir(), "long_generation_summary.tex"))
				So(statErr, ShouldBeNil)

				_ = LongGenResult{
					TotalSpans: 100, CorpusSize: 100,
					MaxChains: 200, Entries: results,
					ValidCount: validCount, ReturnCount: returnCount,
					LoopCount: loopCount, MeanTokens: float64(sumToks) / n,
					MeanNewTokens: float64(sumToks) / n,
				}
			})

			Convey("Artifact: write long generation subsection prose", func() {
				tmpl, err := os.ReadFile("prose/long_generation.tex.tmpl")
				So(err, ShouldBeNil)
				So(WriteProse(string(tmpl), map[string]any{
					"ValidCount":  validCount,
					"NPrompts":    len(prompts),
					"MeanTokens":  float64(sumToks) / n,
					"ReturnCount": returnCount,
					"LoopCount":   loopCount,
				}, "long_generation_prose.tex"), ShouldBeNil)
			})
		})
	})
}
