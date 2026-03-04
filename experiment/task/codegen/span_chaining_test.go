package codegen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/experiment/projector"
)

func TestSpanChaining(t *testing.T) {
	Convey("Given the Python corpus and a span memory", t, func() {
		corpus := pythonCorpus()
		sm := BuildSpanMemory(corpus)
		So(len(sm.Index), ShouldBeGreaterThan, 0)

		const topK = 64
		const nDial = 8
		const maxChains = 4

		type chainEntry struct {
			desc, prefix, fullText string
			chain                  []SpanChainingEntry
			hasReturn, hasLoop     bool
			valid                  bool
			sourceCount            int
		}

		prompts := []struct{ prefix, desc string }{
			{"def factorial(n):", "Factorial"},
			{"def find_max(lst):", "Find max"},
			{"def is_palindrome(s):", "Palindrome"},
			{"def binary_search(lst, target):", "Binary search"},
			{"def filter_list(fn, lst):", "Filter"},
		}

		Convey("When chaining spans for each prompt", func() {
			var results []SpanChainingEntry
			for _, p := range prompts {
				outToks := tokenize(p.prefix)
				usedSpans := make(map[int]bool)
				var chain []ChainedSpan
				for step := 0; step < maxChains; step++ {
					ctxToks := outToks
					if len(ctxToks) > 16 {
						ctxToks = ctxToks[len(ctxToks)-16:]
					}
					fpBoundary := BuildBoundaryFP(detokenize(ctxToks), "")
					cands := sm.RetrieveDiverse(fpBoundary, nDial, topK)

					bestIdx, bestScore := -1, -1.0
					for _, c := range cands {
						if usedSpans[c.Idx] {
							continue
						}
						if c.Score > bestScore {
							bestScore, bestIdx = c.Score, c.Idx
						}
					}
					if bestIdx < 0 {
						break
					}
					usedSpans[bestIdx] = true
					span := sm.Index[bestIdx]
					outToks = append(outToks, span.Tokens...)
					newText := span.Text
					chain = append(chain, ChainedSpan{
						Step: step + 1, Text: newText,
						Length: len(span.Tokens), SimScore: bestScore, SourceIdx: span.Source,
					})
					if strings.Contains(newText, "return") && step > 0 {
						break
					}
				}

				fullText := detokenize(outToks)
				sources := make(map[int]bool)
				for _, c := range chain {
					sources[c.SourceIdx] = true
				}
				hasReturn := strings.Contains(fullText, "return")
				hasLoop := strings.Contains(fullText, "for") || strings.Contains(fullText, "while")

				So(fullText, ShouldNotBeEmpty)

				results = append(results, SpanChainingEntry{
					Desc: p.desc, Prefix: p.prefix, FullText: fullText,
					Chain: chain, ChainLength: len(chain),
					HasReturn: hasReturn, HasLoop: hasLoop,
					LooksValid:  strings.HasPrefix(fullText, "def ") && hasReturn,
					SourceCount: len(sources),
				})
			}

			returnCount, loopCount := 0, 0
			for _, e := range results {
				if e.HasReturn {
					returnCount++
				}
				if e.HasLoop {
					loopCount++
				}
			}

			Convey("All chained outputs are non-empty", func() {
				for _, e := range results {
					So(e.FullText, ShouldNotBeEmpty)
				}
			})

			Convey("Artifacts should be written to the paper directory", func() {
				xAxis := make([]string, len(results))
				retData := make([]float64, len(results))
				loopData := make([]float64, len(results))
				for i, e := range results {
					xAxis[i] = e.Desc
					if e.HasReturn {
						retData[i] = 1
					}
					if e.HasLoop {
						loopData[i] = 1
					}
				}
				So(WriteBarChart(xAxis, []projector.BarSeries{
					{Name: "Has Return", Data: retData},
					{Name: "Has Loop", Data: loopData},
				}, "Span Chaining", "Multi-span chaining quality.",
					"fig:span_chaining", "span_chaining"), ShouldBeNil)

				tableRows := make([]map[string]any, len(results))
				for i, e := range results {
					tableRows[i] = map[string]any{
						"Prompt": e.Desc, "Steps": e.ChainLength,
						"Return": e.HasReturn, "Loop": e.HasLoop,
						"Valid": e.LooksValid, "Sources": fmt.Sprintf("%d", e.SourceCount),
					}
				}
				So(WriteTable(tableRows, "span_chaining_summary.tex"), ShouldBeNil)
				_, statErr := os.Stat(filepath.Join(PaperDir(), "span_chaining_summary.tex"))
				So(statErr, ShouldBeNil)

				_ = SpanChainingResult{
					TotalSpans: len(sm.Index), MaxChains: maxChains,
					Entries:     results,
					ReturnCount: returnCount, LoopCount: loopCount,
				}
			})

			Convey("Artifact: write span chaining subsection prose", func() {
				tmpl, err := os.ReadFile("prose/span_chaining.tex.tmpl")
				So(err, ShouldBeNil)
				So(WriteProse(string(tmpl), map[string]any{
					"ReturnCount": returnCount,
					"LoopCount":   loopCount,
					"MaxChains":   maxChains,
				}, "span_chaining_prose.tex"), ShouldBeNil)
			})
		})
	})
}
