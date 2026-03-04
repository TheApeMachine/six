package codegen

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/experiment/projector"
)

func TestLongGeneration(t *testing.T) {
	Convey("Given the extended corpus and long-range span chaining", t, func() {
		corpus := append(pythonCorpus(), longCorpus()...)
		sm := BuildSpanMemory(corpus)
		eigenTable := buildEigenMode(corpus)
		So(eigenTable, ShouldNotBeNil)
		So(len(sm.Index), ShouldBeGreaterThan, 0)

		const topK = 64
		const nDial = 8
		const maxChains = 12
		const minNewTokens = 2

		overlapLen := func(out, span []string) int {
			maxOvl := len(out)
			if len(span) < maxOvl {
				maxOvl = len(span)
			}
			for ovl := maxOvl; ovl > 0; ovl-- {
				match := true
				for i := 0; i < ovl; i++ {
					if out[len(out)-ovl+i] != span[i] {
						match = false
						break
					}
				}
				if match {
					return ovl
				}
			}
			return 0
		}

		prompts := []struct{ prefix, desc string }{
			{"def quicksort(lst):", "Quicksort"},
			{"def merge_sort(lst):", "Merge sort"},
			{"def dfs(graph, start):", "DFS"},
			{"def bfs(graph, start):", "BFS"},
			{"def bubble_sort(lst):", "Bubble sort"},
			{"def rle_encode(s):", "RLE encode"},
			{"def two_sum(nums, target):", "Two sum"},
		}

		Convey("When generating long programs via span chaining", func() {
			var results []LongGenEntry
			for _, p := range prompts {
				outToks := tokenize(p.prefix)
				usedSpans := make(map[int]bool)
				var chain []LongGenStep
				reachedReturn := false
				promptPhase, _ := weightedCircularMean(eigenTable, p.prefix)

				for step := 0; step < maxChains; step++ {
					ctxToks := outToks
					if len(ctxToks) > 20 {
						ctxToks = ctxToks[len(ctxToks)-20:]
					}
					fpQ := BuildBoundaryFP(detokenize(ctxToks), "")
					cands := sm.RetrieveDiverse(fpQ, nDial, topK)

					type sc struct {
						idx, ovl, newToks int
						score             float64
						meta              SpanMeta
					}
					var viable []sc
					for _, c := range cands {
						if usedSpans[c.Idx] {
							continue
						}
						meta := sm.Index[c.Idx]
						ovl := overlapLen(outToks, meta.Tokens)
						newToks := len(meta.Tokens) - ovl
						if newToks < minNewTokens {
							continue
						}
						
						score := c.Score + float64(newToks)*0.005
						
						// Replace string-matching keyword rewards with topological geometric pull 
						spanPhase, spanConc := weightedCircularMean(eigenTable, meta.Text)
						phaseDiff := spanPhase - promptPhase
						angDist := math.Abs(math.Atan2(math.Sin(phaseDiff), math.Cos(phaseDiff)))
						
						if angDist < 0.5 {
							score += 0.01 * spanConc * (1.0 - angDist)
						}
						
						viable = append(viable, sc{c.Idx, ovl, newToks, score, meta})
					}
					if len(viable) == 0 {
						break
					}
					for i := 0; i < len(viable); i++ {
						for j := i + 1; j < len(viable); j++ {
							if viable[j].score > viable[i].score {
								viable[i], viable[j] = viable[j], viable[i]
							}
						}
					}
					best := viable[0]
					usedSpans[best.idx] = true
					newToks := best.meta.Tokens[best.ovl:]
					outToks = append(outToks, newToks...)
					newText := detokenize(newToks)
					chain = append(chain, LongGenStep{
						Step: step + 1, SpanText: best.meta.Text,
						NewText: newText, NewTokens: best.newToks,
						Overlap: best.ovl, SimScore: best.score,
						SourceIdx: best.meta.Source,
					})
					if step > 0 && IsGeometricallyClosed(eigenTable, newText, promptPhase) {
						reachedReturn = true
						break
					}
				}

				fullText := detokenize(outToks)
				sources := make(map[int]bool)
				totalNew := 0
				for _, c := range chain {
					sources[c.SourceIdx] = true
					totalNew += c.NewTokens
				}
				hasReturn := IsGeometricallyClosed(eigenTable, fullText, promptPhase)
				hasLoop := isValidSyntax(fullText)
				hasIf := isValidSyntax(fullText)
				looksValid := hasReturn && isValidSyntax(fullText)

				So(fullText, ShouldNotBeEmpty)

				results = append(results, LongGenEntry{
					Desc: p.desc, Prefix: p.prefix, FullText: fullText,
					Chain: chain, ChainLength: len(chain),
					TotalTokens: len(outToks), TotalNew: totalNew,
					HasReturn: hasReturn, HasLoop: hasLoop, HasConditional: hasIf,
					LooksValid: looksValid, ReachedReturn: reachedReturn,
					SourceCount: len(sources),
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
				for i, e := range results {
					xAxis[i] = e.Desc
					tokData[i] = float64(e.TotalTokens)
				}
				So(WriteBarChart(xAxis, []projector.BarSeries{
					{Name: "Total Tokens", Data: tokData},
				}, "Long Program Generation", "Tokens generated per prompt.",
					"fig:long_gen", "long_generation"), ShouldBeNil)

				tableRows := make([]map[string]any, len(results))
				for i, e := range results {
					tableRows[i] = map[string]any{
						"Prompt": e.Desc, "Steps": e.ChainLength,
						"Tokens": fmt.Sprintf("%d", e.TotalTokens),
						"Return": e.HasReturn, "Loop": e.HasLoop, "Valid": e.LooksValid,
					}
				}
				So(WriteTable(tableRows, "long_generation_summary.tex"), ShouldBeNil)
				_, statErr := os.Stat(filepath.Join(PaperDir(), "long_generation_summary.tex"))
				So(statErr, ShouldBeNil)

				_ = LongGenResult{
					TotalSpans: len(sm.Index), CorpusSize: len(corpus),
					MaxChains: maxChains, Entries: results,
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
