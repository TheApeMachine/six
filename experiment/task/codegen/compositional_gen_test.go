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

func TestCompositionalGeneration(t *testing.T) {
	Convey("Given out-of-corpus prompts and pure fingerprint similarity", t, func() {
		corpus := append(pythonCorpus(), longCorpus()...)
		sm := BuildSpanMemory(corpus)
		So(len(sm.Index), ShouldBeGreaterThan, 0)

		const topK = 64
		const nDial = 8
		const maxChains = 10
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

		prompts := []struct{ prefix, desc, expected string }{
			{"def is_even(n):", "Even check (not in corpus)", "return n % 2 == 0"},
			{"def square(x):", "Square (not in corpus)", "return x * x"},
			{"def product_list(lst):", "Product list (not in corpus)", "result = 1; for x in lst: result *= x; return result"},
			{"def has_duplicates(lst):", "Has duplicates (not in corpus)", "seen = set()"},
			{"def clamp(x, lo, hi):", "Clamp (not in corpus)", "if x < lo: return lo"},
			{"def second_largest(lst):", "Second largest (not in corpus)", "similar to find_max"},
			{"def mean(lst):", "Mean (not in corpus)", "return sum_list(lst) / len(lst)"},
		}

		Convey("When generating for out-of-corpus prompts", func() {
			var results []CompGenEntry
			for _, p := range prompts {
				outToks := tokenize(p.prefix)
				usedSpans := make(map[int]bool)
				var chain []CompGenStep
				reachedReturn := false

				for step := 0; step < maxChains; step++ {
					ctxToks := outToks
					if len(ctxToks) > 20 {
						ctxToks = ctxToks[len(ctxToks)-20:]
					}
					queryFP := geometry.NewPhaseDial().Encode(detokenize(ctxToks))
					cands := sm.RetrieveDiverse(queryFP, nDial, topK)

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
						// Pure sim only — no heuristics
						viable = append(viable, sc{c.Idx, ovl, newToks, c.Score, meta})
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
					srcFn := ""
					if best.meta.Source < len(corpus) {
						lines := strings.SplitN(corpus[best.meta.Source], "\n", 2)
						srcFn = lines[0]
					}
					chain = append(chain, CompGenStep{
						Step: step + 1, SpanText: best.meta.Text,
						NewText: newText, NewTokens: best.newToks,
						Overlap: best.ovl, SimScore: best.score,
						SourceIdx: best.meta.Source, SourceFn: srcFn,
					})
					if strings.Contains(newText, "return") && step > 0 {
						reachedReturn = true
						break
					}
				}

				fullText := detokenize(outToks)
				sources := make(map[int]bool)
				sourceFns := make(map[string]bool)
				totalNew := 0
				for _, c := range chain {
					sources[c.SourceIdx] = true
					sourceFns[c.SourceFn] = true
					totalNew += c.NewTokens
				}
				expectedToks := tokenize(p.expected)
				matched := 0
				for _, et := range expectedToks {
					if len(et) > 2 && strings.Contains(fullText, et) {
						matched++
					}
				}
				expOverlap := 0.0
				if len(expectedToks) > 0 {
					expOverlap = float64(matched) / float64(len(expectedToks))
				}

				hasReturn := strings.Contains(fullText, "return")
				hasLoop := strings.Contains(fullText, "for") || strings.Contains(fullText, "while")

				So(fullText, ShouldNotBeEmpty)

				results = append(results, CompGenEntry{
					Desc: p.desc, Prefix: p.prefix, Expected: p.expected,
					FullText: fullText, Chain: chain, ChainLength: len(chain),
					TotalTokens: len(outToks), TotalNew: totalNew,
					HasReturn: hasReturn, HasLoop: hasLoop,
					HasConditional:  strings.Contains(fullText, "if"),
					ReachedReturn:   reachedReturn,
					SourceCount:     len(sources),
					ExpectedOverlap: expOverlap,
				})
			}

			returnCount, loopCount := 0, 0
			sumOverlap := 0.0
			for _, e := range results {
				if e.HasReturn {
					returnCount++
				}
				if e.HasLoop {
					loopCount++
				}
				sumOverlap += e.ExpectedOverlap
			}
			n := float64(len(prompts))

			Convey("All outputs non-empty", func() {
				for _, e := range results {
					So(e.FullText, ShouldNotBeEmpty)
				}
			})

			Convey("Artifacts should be written to the paper directory", func() {
				xAxis := make([]string, len(results))
				ovlData := make([]float64, len(results))
				for i, e := range results {
					xAxis[i] = fmt.Sprintf("%d", i+1)
					ovlData[i] = e.ExpectedOverlap
				}
				So(WriteBarChart(xAxis, []projector.BarSeries{
					{Name: "Expected Overlap", Data: ovlData},
				}, "Compositional Generation",
					"Out-of-corpus expected token overlap per prompt.",
					"fig:comp_gen", "compositional_gen"), ShouldBeNil)

				tableRows := make([]map[string]any, len(results))
				for i, e := range results {
					tableRows[i] = map[string]any{
						"Prompt": e.Desc, "Return": e.HasReturn,
						"Loop": e.HasLoop, "ExpOvl": fmt.Sprintf("%.3f", e.ExpectedOverlap),
					}
				}
				So(WriteTable(tableRows, "compositional_gen_summary.tex"), ShouldBeNil)
				_, statErr := os.Stat(filepath.Join(PaperDir(), "compositional_gen_summary.tex"))
				So(statErr, ShouldBeNil)

				_ = CompGenResult{
					TotalSpans: len(sm.Index), Entries: results,
					ReturnCount: returnCount, LoopCount: loopCount,
					MeanTokens: 0, MeanExpectedOverlap: sumOverlap / n,
				}
			})

			Convey("Artifact: write compositional generation subsection prose", func() {
				tmpl, err := os.ReadFile("prose/compositional_gen.tex.tmpl")
				So(err, ShouldBeNil)
				So(WriteProse(string(tmpl), map[string]any{
					"MeanExpectedOverlap": sumOverlap / n,
					"ReturnCount":         returnCount,
				}, "compositional_gen_prose.tex"), ShouldBeNil)
			})
		})
	})
}
