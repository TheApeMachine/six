package codegen

import (
	"fmt"
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

func TestCompositionalGeneration(t *testing.T) {
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
		
		prompts := []struct{ prefix, desc, expected string }{
			{"def compute_pi_approx(iters):", "Pi Approximation (Novel math)", "pi = 0; for i in range(iters):"},
			{"def lcg_next(seed, a, c, m):", "LCG PRNG (Novel math)", "return (a * seed + c) % m"},
			{"def fibonacci_sum(n):", "Fibonacci Sum (Novel logic)", "sum = 0"},
			{"def count_vowels(s):", "String processing (Novel logic)", "count = 0; for char in s:"},
			{"def is_palindrome(s):", "Sequence reflection (Novel logic)", "return s == s[::-1]"},
			{"def geometric_progression(a, r, n):", "Series generation (Novel logic)", "return [a * (r ** i) for i in range(n)]"},
		}

		Convey("When generating for completely out-of-corpus prompts via chords", func() {
			var results []CompGenEntry
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
				for res := range machine.Prompt(promptChords) {
					if limit > 64 {
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
				hasLoop := strings.Contains(fullText, "for") || strings.Contains(fullText, "while")
				hasConditional := strings.Contains(fullText, "if")
				looksValid := isValidSyntax(fullText)

				So(fullText, ShouldNotBeEmpty)

				results = append(results, CompGenEntry{
					Desc: p.desc, Prefix: p.prefix, Expected: p.expected,
					FullText: fullText, TotalNew: len(generatedChords),
					HasReturn: hasReturn, HasLoop: hasLoop,
					HasConditional: hasConditional,
					ReachedReturn:  looksValid,  // Used as general ast validation score here
				})
			}

			validCount, loopCount := 0, 0
			for _, e := range results {
				if e.ReachedReturn {
					validCount++
				}
				if e.HasLoop {
					loopCount++
				}
			}

			Convey("Artifacts should be written to the paper directory", func() {
				xAxis := make([]string, len(results))
				validData := make([]float64, len(results))
				tokensData := make([]float64, len(results))
				var codeSections []projector.CodeSection

				for i, e := range results {
					xAxis[i] = fmt.Sprintf("Prompt %d", i+1)
					if e.ReachedReturn {
						validData[i] = 1.0
					} else {
						validData[i] = 0.0
					}
					tokensData[i] = float64(e.TotalNew)

					codeSections = append(codeSections, projector.CodeSection{
						Prompt: e.Desc,
						Code:   e.FullText,
					})
				}

				So(WriteComboChart(xAxis, []projector.ComboSeries{
					{Name: "AST Validation Success", Type: "bar", Data: validData},
					{Name: "Chords Generated", Type: "line", Data: tokensData},
				}, "Prompts", "Metrics", 0.0, 64.0, "Compositional CodeGen (Chord Native)",
					"Validation success vs chords generated over genuinely out-of-corpus pipelines.",
					"fig:comp_gen", "compositional_gen"), ShouldBeNil)

				So(WriteCodeAppendix(codeSections, "compositional_gen_code.tex"), ShouldBeNil)
			})
		})
	})
}
