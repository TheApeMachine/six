package codegen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/geometry"
)


func TestSpanSolver(t *testing.T) {
	Convey("Given the Python corpus and a span memory substrate", t, func() {
		corpus := pythonCorpus()
		sm := BuildSpanMemory(corpus)
		So(len(sm.Index), ShouldBeGreaterThan, 0)

		const outLen = 8
		const topK = 16
		const nRefine = 3
		const nDial = 6

		// solve runs the BVP vote-and-refine loop for one prompt.
		solve := func(prefix, suffix string) SpanSolverEntry {
			fpPrefix := geometry.NewPhaseDial().Encode(prefix)
			fpBoundary := BuildBoundaryFP(prefix, suffix)
			currentTokens := make([]string, outLen)
			var converged bool

			for iter := 0; iter < nRefine; iter++ {
				queryFP := fpBoundary
				if iter > 0 {
					combined := prefix + "\n    " + detokenize(currentTokens)
					if suffix != "" {
						combined += "\n" + suffix
					}
					queryFP = geometry.NewPhaseDial().Encode(combined)
				}

				candidates := sm.RetrieveDiverse(queryFP, nDial, topK)

				// Token voting
				newTokens := make([]string, outLen)
				for pos := 0; pos < outLen; pos++ {
					votes := make(map[string]float64)
					for _, c := range candidates {
						span := sm.Index[c.Idx]
						if pos < len(span.Tokens) {
							votes[span.Tokens[pos]] += c.Score + 1.0
						}
					}
					bestTok, bestW := "", -1.0
					for tok, w := range votes {
						if w > bestW {
							bestW, bestTok = w, tok
						}
					}
					newTokens[pos] = bestTok
				}

				if iter > 0 {
					if strings.Join(newTokens, "\x00") == strings.Join(currentTokens, "\x00") {
						converged = true
						currentTokens = newTokens
						break
					}
				}
				currentTokens = newTokens
			}

			generated := detokenize(currentTokens)
			finalFP := geometry.NewPhaseDial().Encode(prefix + "\n    " + generated)
			finalRanked := sm.Substrate.PhaseDialRank(sm.Candidates, finalFP)
			showN := min(5, len(finalRanked))
			topTexts := make([]string, showN)
			topScores := make([]float64, showN)
			for i := 0; i < showN; i++ {
				topTexts[i] = string(sm.Substrate.Entries[finalRanked[i].Idx].Readout)
				topScores[i] = finalRanked[i].Score
			}

			unique := make(map[string]bool)
			for _, tok := range currentTokens {
				if tok != "" {
					unique[tok] = true
				}
			}
			uniqueRatio := 0.0
			if len(currentTokens) > 0 {
				uniqueRatio = float64(len(unique)) / float64(len(currentTokens))
			}

			return SpanSolverEntry{
				Prefix:          prefix,
				Generated:       generated,
				Converged:       converged,
				Iterations:      nRefine,
				HasReturn:       strings.Contains(generated, "return"),
				HasColon:        strings.Contains(generated, ":"),
				UniqueRatio:     uniqueRatio,
				PrefixRelevance: fpPrefix.Similarity(finalFP),
				TopSpans:        topTexts,
				TopScores:       topScores,
			}
		}

		prompts := []struct{ prefix, suffix, desc string }{
			{"def factorial(n):", "", "Factorial — arithmetic recursion"},
			{"def find_max(lst):", "", "Find max — list iteration"},
			{"def is_palindrome(s):", "", "Palindrome check — string operation"},
			{"def binary_search(lst, target):", "", "Binary search — algorithm"},
			{"def filter_list(fn, lst):", "", "Filter — higher-order function"},
		}

		Convey("When solving each prompt with BVP vote-and-refine", func() {
			var entries []SpanSolverEntry
			for _, p := range prompts {
				e := solve(p.prefix, p.suffix)
				e.Desc = p.desc
				entries = append(entries, e)
			}

			convergedCount, returnCount, colonCount := 0, 0, 0
			sumUniq, sumRel := 0.0, 0.0
			for _, e := range entries {
				if e.Converged {
					convergedCount++
				}
				if e.HasReturn {
					returnCount++
				}
				if e.HasColon {
					colonCount++
				}
				sumUniq += e.UniqueRatio
				sumRel += e.PrefixRelevance
			}
			n := float64(len(prompts))

			Convey("All generated spans should be non-empty", func() {
				for _, e := range entries {
					So(e.Generated, ShouldNotBeEmpty)
				}
			})

			Convey("Mean unique token ratio should be positive", func() {
				So(sumUniq/n, ShouldBeGreaterThan, 0)
			})

			Convey("Prefix relevance should be a valid similarity score", func() {
				for _, e := range entries {
					So(e.PrefixRelevance, ShouldBeBetweenOrEqual, -1.0, 1.0)
				}
			})

			Convey("Artifacts should be written to the paper directory", func() {
				xAxis := make([]string, len(entries))
				relData := make([]float64, len(entries))
				uniqueData := make([]float64, len(entries))
				for i, e := range entries {
					xAxis[i] = fmt.Sprintf("%d", i+1)
					relData[i] = e.PrefixRelevance
					uniqueData[i] = e.UniqueRatio
				}
				So(WriteBarChart(xAxis, []projector.BarSeries{
					{Name: "Prefix Relevance", Data: relData},
					{Name: "Unique Token Ratio", Data: uniqueData},
				}, "Span Solver Quality Metrics",
					"Per-prompt prefix relevance and unique token ratio for the BVP span solver.",
					"fig:span_solver", "span_solver"), ShouldBeNil)

				tableRows := make([]map[string]any, len(entries))
				for i, e := range entries {
					tableRows[i] = map[string]any{
						"Prompt":      e.Desc,
						"Converged":   e.Converged,
						"HasReturn":   e.HasReturn,
						"UniqueRatio": fmt.Sprintf("%.3f", e.UniqueRatio),
						"Relevance":   fmt.Sprintf("%.4f", e.PrefixRelevance),
					}
				}
				So(WriteTable(tableRows, "span_solver_summary.tex"), ShouldBeNil)
				_, statErr := os.Stat(filepath.Join(PaperDir(), "span_solver_summary.tex"))
				So(statErr, ShouldBeNil)

				So(len(sm.Index), ShouldBeGreaterThan, 0)
			})

			Convey("Artifact: write span solver subsection prose", func() {
				tmpl, err := os.ReadFile("prose/span_solver.tex.tmpl")
				So(err, ShouldBeNil)
				So(WriteProse(string(tmpl), map[string]any{
					"OutLen":          outLen,
					"NPrompts":        len(prompts),
					"MeanRelevance":   sumRel / n,
					"MeanUniqueRatio": sumUniq / n,
					"ConvergedCount":  convergedCount,
					"NRefine":         nRefine,
				}, "span_solver_prose.tex"), ShouldBeNil)
			})
		})
	})
}
