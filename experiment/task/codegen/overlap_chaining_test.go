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

func TestOverlapChaining(t *testing.T) {
	Convey("Given the Python corpus and overlap-aware span chaining", t, func() {
		corpus := pythonCorpus()
		sm := BuildSpanMemory(corpus)
		So(len(sm.Index), ShouldBeGreaterThan, 0)

		const topK = 64
		const nDial = 8
		const maxChains = 6
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

		extractName := func(text string) string {
			i := strings.Index(text, "def ")
			if i < 0 {
				return ""
			}
			rest := text[i+4:]
			p := strings.Index(rest, "(")
			if p < 0 {
				return ""
			}
			return strings.TrimSpace(rest[:p])
		}

		prompts := []struct{ prefix, desc string }{
			{"def factorial(n):", "Factorial"},
			{"def find_max(lst):", "Find max"},
			{"def is_palindrome(s):", "Palindrome"},
			{"def binary_search(lst, target):", "Binary search"},
			{"def filter_list(fn, lst):", "Filter"},
		}

		Convey("When running overlap-aware span chaining", func() {
			var results []OverlapChainingEntry
			for _, p := range prompts {
				outToks := tokenize(p.prefix)
				lockedName := extractName(p.prefix)
				usedSpans := make(map[int]bool)
				var chain []OverlapChainStep

				for step := 0; step < maxChains; step++ {
					ctxToks := outToks
					if len(ctxToks) > 16 {
						ctxToks = ctxToks[len(ctxToks)-16:]
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
						if step > 0 && lockedName != "" {
							if n := extractName(meta.Text); n != "" && n != lockedName {
								continue
							}
						}
						ovl := overlapLen(outToks, meta.Tokens)
						newToks := len(meta.Tokens) - ovl
						if newToks < minNewTokens {
							continue
						}
						score := c.Score + float64(newToks)*0.005
						if strings.Contains(detokenize(meta.Tokens[ovl:]), "return") {
							score += 0.01
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
					chain = append(chain, OverlapChainStep{
						Step: step + 1, SpanText: best.meta.Text,
						NewText: newText, NewTokens: best.newToks,
						Overlap: best.ovl, SimScore: best.score,
						SourceIdx: best.meta.Source,
					})
					if strings.Contains(newText, "return") && step > 0 {
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
				hasReturn := strings.Contains(fullText, "return")
				hasLoop := strings.Contains(fullText, "for") || strings.Contains(fullText, "while")

				So(fullText, ShouldNotBeEmpty)

				results = append(results, OverlapChainingEntry{
					Desc: p.desc, Prefix: p.prefix, FullText: fullText,
					Chain: chain, ChainLength: len(chain), TotalNew: totalNew,
					HasReturn: hasReturn, HasLoop: hasLoop,
					HasColon:     strings.Count(fullText, ":") > 1,
					LooksValid:   strings.HasPrefix(fullText, "def ") && hasReturn,
					SingleSource: len(sources) == 1,
					SourceCount:  len(sources),
				})
			}

			validCount, returnCount := 0, 0
			for _, e := range results {
				if e.LooksValid {
					validCount++
				}
				if e.HasReturn {
					returnCount++
				}
			}

			Convey("All outputs are non-empty", func() {
				for _, e := range results {
					So(e.FullText, ShouldNotBeEmpty)
				}
			})

			Convey("Artifacts should be written to the paper directory", func() {
				xAxis := make([]string, len(results))
				validData := make([]float64, len(results))
				for i, e := range results {
					xAxis[i] = e.Desc
					if e.LooksValid {
						validData[i] = 1
					}
				}
				So(WriteBarChart(xAxis, []projector.BarSeries{
					{Name: "Valid", Data: validData},
				}, "Overlap-Aware Chaining", "Validity per prompt.",
					"fig:overlap_chaining", "overlap_chaining"), ShouldBeNil)

				tableRows := make([]map[string]any, len(results))
				for i, e := range results {
					tableRows[i] = map[string]any{
						"Prompt": e.Desc, "Steps": e.ChainLength,
						"NewToks": fmt.Sprintf("%d", e.TotalNew),
						"Valid": e.LooksValid, "SingleSrc": e.SingleSource,
					}
				}
				So(WriteTable(tableRows, "overlap_chaining_summary.tex"), ShouldBeNil)
				_, statErr := os.Stat(filepath.Join(PaperDir(), "overlap_chaining_summary.tex"))
				So(statErr, ShouldBeNil)

				_ = OverlapChainingResult{
					TotalSpans: len(sm.Index), MaxChains: maxChains,
					MinNewTokens: minNewTokens, Entries: results,
					ValidCount: validCount, ReturnCount: returnCount,
				}
			})

			Convey("Artifact: write overlap chaining subsection prose", func() {
				tmpl, err := os.ReadFile("prose/overlap_chaining.tex.tmpl")
				So(err, ShouldBeNil)
				So(WriteProse(string(tmpl), map[string]any{
					"ValidCount":  validCount,
					"NPrompts":    len(results),
					"ReturnCount": returnCount,
				}, "overlap_chaining_prose.tex"), ShouldBeNil)
			})
		})
	})
}
