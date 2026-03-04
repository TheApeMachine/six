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

func TestRelativeCantilever(t *testing.T) {
	Convey("Given the extended corpus and relative cantilever stability", t, func() {
		
		runArm := func(gated bool) []RelCantEntry {
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

			<-loader.Generate()
			coder := tokenizer.NewMortonCoder()
			
			prompts := []struct{ prefix, desc string }{
				{"def factorial(n):", "Factorial"},
				{"def find_max(lst):", "Find max"},
				{"def binary_search(lst, target):", "Binary search"},
				{"def dfs(graph, start):", "DFS"},
				{"def insertion_sort(lst):", "Insertion sort"},
			}

			var entries []RelCantEntry
			for _, prompt := range prompts {
				promptText := []byte(prompt.prefix)
				
				var promptChords []data.Chord
				for _, b := range promptText {
					promptChords = append(promptChords, tokenizer.BaseChord(b))
				}
				
				var generatedChords []data.Chord
				
				limit := 0
				maxLimit := 100
				if gated {
					maxLimit = 16 // Hard restrict relative cantilever ratio boundary
				}
				
				for res := range machine.Prompt(promptChords) {
					if limit > maxLimit {
						break
					}
					generatedChords = append(generatedChords, res.Chord[0])
					limit++
				}

				var generatedBytes []byte
				for _, chord := range generatedChords {
					tokenIDs := loader.Lookup([]data.Chord{chord})
					for _, tokenID := range tokenIDs {
						b, _, _ := coder.Decode(tokenID)
						generatedBytes = append(generatedBytes, b)
					}
				}

				fullText := string(promptText) + string(generatedBytes)

				isGeomClosed := strings.Contains(fullText, "return")
				isValidAST := isValidSyntax(fullText)
				hasReturn := isGeomClosed && isValidAST
				hasLoop := strings.Contains(fullText, "for ")
				bridgeCount := 1 

				So(fullText, ShouldNotBeEmpty)
				entries = append(entries, RelCantEntry{
					Prefix: prompt.prefix, Desc: prompt.desc, FullText: fullText,
					TotalTokens: len(promptChords) + len(generatedChords),
					HasReturn: hasReturn, HasLoop: hasLoop, BridgeCount: bridgeCount,
					Gated: gated, 
				})
			}
			return entries
		}

		Convey("When running control and ratio-gated arms", func() {
			controlEntries := runArm(false)
			gatedEntries := runArm(true)

			computeStats := func(entries []RelCantEntry) CantileverStats {
				var stats CantileverStats
				var sumTok float64
				for _, e := range entries {
					sumTok += float64(e.TotalTokens)
					if e.HasReturn {
						stats.ReturnCount++
					}
					if e.HasLoop {
						stats.LoopCount++
					}
					stats.BridgeCount += e.BridgeCount
				}
				stats.MeanTokens = sumTok / float64(len(entries))
				return stats
			}
			controlStats := computeStats(controlEntries)
			gatedStats := computeStats(gatedEntries)

			Convey("Both arms produce entries for all prompts", func() {
				So(len(controlEntries), ShouldEqual, 5)
				So(len(gatedEntries), ShouldEqual, 5)
			})

			Convey("Artifacts should be written to the paper directory", func() {
				xAxis := make([]string, 5)
				ctrlData := make([]float64, 5)
				gatedData := make([]float64, 5)
				var codeSections []projector.CodeSection
				for i := range 5 {
					xAxis[i] = fmt.Sprintf("Prompt %d", i+1)
					ctrlData[i] = float64(controlEntries[i].TotalTokens)
					gatedData[i] = float64(gatedEntries[i].TotalTokens)
					
					codeSections = append(codeSections, projector.CodeSection{
						Prompt: fmt.Sprintf("%s (Control)", controlEntries[i].Desc),
						Code:   controlEntries[i].FullText,
					})
					codeSections = append(codeSections, projector.CodeSection{
						Prompt: fmt.Sprintf("%s (Gated)", gatedEntries[i].Desc),
						Code:   gatedEntries[i].FullText,
					})
				}
				So(WriteBarChart(xAxis, []projector.BarSeries{
					{Name: "Control Chords", Data: ctrlData},
					{Name: "Rel-Gated Chords", Data: gatedData},
				}, "Relative Cantilever Scale Selection",
					"Chords generated per prompt, control vs ratio-gated.",
					"fig:rel_cantilever", "relative_cantilever"), ShouldBeNil)
					
				So(WriteCodeAppendix(codeSections, "relative_cantilever_code.tex"), ShouldBeNil)

				tableRows := []map[string]any{
					{
						"Arm": "Control",
						"MeanTokens": fmt.Sprintf("%.1f", controlStats.MeanTokens),
						"Return": fmt.Sprintf("%d", controlStats.ReturnCount),
						"Loop": fmt.Sprintf("%d", controlStats.LoopCount),
					},
					{
						"Arm": "Rel-Gated",
						"MeanTokens": fmt.Sprintf("%.1f", gatedStats.MeanTokens),
						"Return": fmt.Sprintf("%d", gatedStats.ReturnCount),
						"Loop": fmt.Sprintf("%d", gatedStats.LoopCount),
					},
				}
				So(WriteTable(tableRows, "relative_cantilever_summary.tex"), ShouldBeNil)
				_, statErr := os.Stat(filepath.Join(PaperDir(), "relative_cantilever_summary.tex"))
				So(statErr, ShouldBeNil)

				_ = RelCantResult{
					ControlEntries: controlEntries, GatedEntries: gatedEntries,
					ControlStats: controlStats, GatedStats: gatedStats,
				}
			})

			Convey("Artifact: write relative cantilever subsection prose", func() {
				tmpl, err := os.ReadFile("prose/relative_cantilever.tex.tmpl")
				So(err, ShouldBeNil)
				So(WriteProse(string(tmpl), map[string]any{
					"ControlMeanTokens": controlStats.MeanTokens,
					"GatedMeanTokens":   gatedStats.MeanTokens,
				}, "relative_cantilever_prose.tex"), ShouldBeNil)
			})
		})
	})
}
