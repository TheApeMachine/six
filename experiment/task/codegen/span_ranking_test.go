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

func TestSpanRanking(t *testing.T) {
	Convey("Given the Python corpus and a multi-length span memory", t, func() {
		corpus := pythonCorpus()
		sm := BuildSpanMemory(corpus)
		So(len(sm.Index), ShouldBeGreaterThan, 0)

		const topK = 64
		const nDial = 8

		// scoreSpan computes total = sim + prefix-overlap bonus + structural bonus.
		scoreSpan := func(idx int, fpBoundary, _ interface{}, prefixTokens []string) (sim, ovl, str, total float64) {
			_ = idx // suppress unused; accessed in caller
			return // filled by caller
		}
		_ = scoreSpan // unused wrapper — inline below for clarity

		prompts := []struct{ prefix, desc string }{
			{"def factorial(n):", "Factorial"},
			{"def find_max(lst):", "Find max"},
			{"def is_palindrome(s):", "Palindrome"},
			{"def binary_search(lst, target):", "Binary search"},
			{"def filter_list(fn, lst):", "Filter"},
		}

		Convey("When ranking spans for each prompt", func() {
			var entries []SpanRankingEntry

			for _, p := range prompts {
				fpBoundary := BuildBoundaryFP(p.prefix, "")
				prefixTokens := tokenize(p.prefix)

				candidates := sm.RetrieveDiverse(fpBoundary, nDial, topK)
				So(len(candidates), ShouldBeGreaterThan, 0)

				type scored struct {
					idx             int
					sim, ovl, str, total float64
				}
				scoredList := make([]scored, len(candidates))
				for i, c := range candidates {
					meta := sm.Index[c.Idx]
					simScore := sm.Substrate.Entries[c.Idx].Fingerprint.Similarity(fpBoundary)
					prefixOvl := 0.0
					for _, pt := range prefixTokens {
						for _, st := range meta.Tokens {
							if pt == st && len(pt) > 1 {
								prefixOvl += 0.02
							}
						}
					}
					if prefixOvl > 0.1 {
						prefixOvl = 0.1
					}
					structBonus := 0.0
					if strings.Contains(meta.Text, "return") {
						structBonus += 0.01
					}
					if strings.Contains(meta.Text, ":") {
						structBonus += 0.005
					}
					if strings.Contains(meta.Text, "    ") {
						structBonus += 0.005
					}
					scoredList[i] = scored{c.Idx, simScore, prefixOvl, structBonus, simScore + prefixOvl + structBonus}
				}
				for i := 0; i < len(scoredList); i++ {
					for j := i + 1; j < len(scoredList); j++ {
						if scoredList[j].total > scoredList[i].total {
							scoredList[i], scoredList[j] = scoredList[j], scoredList[i]
						}
					}
				}

				w := scoredList[0]
				winMeta := sm.Index[w.idx]
				So(winMeta.Text, ShouldNotBeEmpty)
				So(w.sim, ShouldBeBetweenOrEqual, -1.0, 1.0)

				identReuse := 0
				for _, pt := range prefixTokens {
					if len(pt) > 2 {
						for _, wt := range winMeta.Tokens {
							if strings.Contains(wt, pt) || strings.Contains(pt, wt) {
								identReuse++
								break
							}
						}
					}
				}

				showN := min(20, len(scoredList))
				topCandidates := make([]SpanCandidate, showN)
				for i := 0; i < showN; i++ {
					s := scoredList[i]
					m := sm.Index[s.idx]
					topCandidates[i] = SpanCandidate{
						Rank: i + 1, Text: m.Text, Length: m.Length,
						SimScore: s.sim, PrefixOvl: s.ovl, StructBonus: s.str,
						Total: s.total, SourceIdx: m.Source,
					}
				}

				entries = append(entries, SpanRankingEntry{
					Desc: p.desc, Prefix: p.prefix,
					WinnerText: winMeta.Text, WinnerLength: winMeta.Length,
					WinnerSim: w.sim, WinnerTotal: w.total,
					HasReturn:      strings.Contains(winMeta.Text, "return"),
					HasColon:       strings.Contains(winMeta.Text, ":"),
					HasIndent:      strings.Contains(winMeta.Text, "    "),
					IdentReuse:     identReuse,
					ExactCorpus:    ExactMatch(corpus, winMeta.Text),
					TopCandidates:  topCandidates,
					TotalRetrieved: len(candidates),
				})
			}

			exactCount, returnCount, colonCount, indentCount := 0, 0, 0, 0
			sumSim := 0.0
			for _, e := range entries {
				if e.ExactCorpus {
					exactCount++
				}
				if e.HasReturn {
					returnCount++
				}
				if e.HasColon {
					colonCount++
				}
				if e.HasIndent {
					indentCount++
				}
				sumSim += e.WinnerSim
			}
			n := float64(len(prompts))

			Convey("Every winner span should be non-empty", func() {
				for _, e := range entries {
					So(e.WinnerText, ShouldNotBeEmpty)
				}
			})

			Convey("Every winner should have valid similarity", func() {
				for _, e := range entries {
					So(e.WinnerSim, ShouldBeBetweenOrEqual, -1.0, 1.0)
				}
			})

			Convey("Artifacts should be written to the paper directory", func() {
				xAxis := make([]string, len(entries))
				simData := make([]float64, len(entries))
				totalData := make([]float64, len(entries))
				for i, e := range entries {
					xAxis[i] = e.Desc
					simData[i] = e.WinnerSim
					totalData[i] = e.WinnerTotal
				}
				So(WriteBarChart(xAxis, []projector.BarSeries{
					{Name: "Winner Sim", Data: simData},
					{Name: "Winner Total", Data: totalData},
				}, "Span Ranking BVP",
					"Per-prompt winner similarity and total score for whole-span selection.",
					"fig:span_ranking", "span_ranking"), ShouldBeNil)

				tableRows := make([]map[string]any, len(entries))
				for i, e := range entries {
					tableRows[i] = map[string]any{
						"Prompt":      e.Desc,
						"WinnerLen":   e.WinnerLength,
						"WinnerSim":   fmt.Sprintf("%.4f", e.WinnerSim),
						"Total":       fmt.Sprintf("%.4f", e.WinnerTotal),
						"ExactCorpus": e.ExactCorpus,
					}
				}
				So(WriteTable(tableRows, "span_ranking_summary.tex"), ShouldBeNil)
				_, statErr := os.Stat(filepath.Join(PaperDir(), "span_ranking_summary.tex"))
				So(statErr, ShouldBeNil)

				So(sumSim/n, ShouldBeBetweenOrEqual, -1.0, 1.0)
			})

			Convey("Artifact: write span ranking subsection prose", func() {
				tmpl, err := os.ReadFile("prose/span_ranking.tex.tmpl")
				So(err, ShouldBeNil)
				So(WriteProse(string(tmpl), map[string]any{
					"ExactCount":     exactCount,
					"NPrompts":       len(prompts),
					"MeanWinnerSim": sumSim / n,
				}, "span_ranking_prose.tex"), ShouldBeNil)
			})
		})
	})
}
