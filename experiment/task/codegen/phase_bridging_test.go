package codegen

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/provider/huggingface"
	"github.com/theapemachine/six/store"
	"github.com/theapemachine/six/tokenizer"
	"github.com/theapemachine/six/vm"
)

func TestPhaseBridging(t *testing.T) {
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

		<-loader.Generate()
		coder := tokenizer.NewMortonCoder()
		
		prompts := []struct{ prefix, desc string }{
			{"def factorial(n):", "Factorial"},
			{"def find_max(lst):", "Find max"},
			{"def binary_search(lst, target):", "Binary search"},
			{"def dfs(graph, start):", "DFS"},
			{"def insertion_sort(lst):", "Insertion sort"},
		}

		Convey("When generating with genuine Toroidal phase-triggered manifold bridging", func() {
			var entries []PhaseBridgingEntry
			eigen := geometry.NewEigenMode()

			for _, prompt := range prompts {
				promptText := []byte(prompt.prefix)
				
				var promptChords []data.Chord
				for _, b := range promptText {
					promptChords = append(promptChords, tokenizer.BaseChord(b))
				}
				
				var generatedChords []data.Chord
				
				limit := 0
				bridgeCount := 0
				var prevPhase float64

				for res := range machine.Prompt(promptChords, nil) {
					if limit > 64 {
						break
					}
					// Only chords exist in the generation stream
					generatedChords = append(generatedChords, res.Chord.Cubes[0][0])

					// Reconstruct phase topology dynamically during generation stream
					currTheta, _ := eigen.PhaseForChord(&res.Chord.Cubes[0][0])
					phaseDiff := currTheta - prevPhase
					phaseDeriv := math.Abs(math.Atan2(math.Sin(phaseDiff), math.Cos(phaseDiff)))
					
					// Every time the generative walk abruptly shifts topology, we record it as a phase-bridge crossing
					if limit > 0 && phaseDeriv > 0.1 {
						bridgeCount++
					}

					prevPhase = currTheta
					limit++
				}

				// Re-inflate sequence
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

				So(fullText, ShouldNotBeEmpty)

				entries = append(entries, PhaseBridgingEntry{
					Prefix: prompt.prefix, Desc: prompt.desc,
					FullText: fullText, TotalTokens: len(promptChords) + len(generatedChords), 
					HasReturn: hasReturn, HasLoop: hasLoop, BridgeCount: bridgeCount,
				})
			}

			returnCount, loopCount, bridgeTotal := 0, 0, 0
			sumToks := 0.0
			for _, e := range entries {
				if e.HasReturn {
					returnCount++
				}
				if e.HasLoop {
					loopCount++
				}
				bridgeTotal += e.BridgeCount
				sumToks += float64(e.TotalTokens)
			}
			n := float64(len(entries))

			Convey("All outputs non-empty", func() {
				for _, e := range entries {
					So(e.FullText, ShouldNotBeEmpty)
				}
			})

			Convey("Artifacts should be written to the paper directory", func() {
				xAxis := make([]string, len(entries))
				tokData := make([]float64, len(entries))
				bridgeData := make([]float64, len(entries))
				var codeSections []projector.CodeSection

				for i, e := range entries {
					xAxis[i] = fmt.Sprintf("Prompt %d", i+1)
					tokData[i] = float64(e.TotalTokens)
					bridgeData[i] = float64(e.BridgeCount)

					codeSections = append(codeSections, projector.CodeSection{
						Prompt: e.Desc,
						Code:   e.FullText,
					})
				}
				
				So(WriteComboChart(xAxis, []projector.ComboSeries{
					{Name: "Chords/Bytes", Type: "bar", Data: tokData},
					{Name: "Topological Bridges", Type: "line", Data: bridgeData},
				}, "Prompts", "Metrics", 0.0, 100.0, "Toroidal Phase-Triggered Manifold Bridging",
					"Chords generated and topology events per prompt via vm.Machine.",
					"fig:phase_bridging", "phase_bridging"), ShouldBeNil)

				So(WriteCodeAppendix(codeSections, "phase_bridging_code.tex"), ShouldBeNil)

				tableRows := make([]map[string]any, len(entries))
				for i, e := range entries {
					tableRows[i] = map[string]any{
						"Prompt": e.Desc,
						"Tokens": fmt.Sprintf("%d", e.TotalTokens),
						"Return": e.HasReturn, "Loop": e.HasLoop,
						"Bridges": e.BridgeCount,
					}
				}
				So(WriteTable(tableRows, "phase_bridging_summary.tex"), ShouldBeNil)
				_, statErr := os.Stat(filepath.Join(PaperDir(), "phase_bridging_summary.tex"))
				So(statErr, ShouldBeNil)

				_ = PhaseBridgingResult{
					Entries: entries, MeanTokens: sumToks / n,
					ReturnCount: returnCount, LoopCount: loopCount,
					BridgeTotal: bridgeTotal,
				}
			})

			Convey("Artifact: write phase bridging subsection prose", func() {
				tmpl, err := os.ReadFile("prose/phase_bridging.tex.tmpl")
				So(err, ShouldBeNil)
				So(WriteProse(string(tmpl), map[string]any{
					"MeanTokens":  sumToks / n,
					"BridgeTotal": bridgeTotal,
					"NPrompts":    len(entries),
				}, "phase_bridging_prose.tex"), ShouldBeNil)
			})
		})
	})
}
