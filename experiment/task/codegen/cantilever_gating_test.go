package codegen

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/numeric"
)

func TestCantileverGating(t *testing.T) {
	Convey("Given the extended corpus and cantilever-gated span retrieval", t, func() {
		corpus := append(pythonCorpus(), longCorpus()...)
		eigenTable := buildEigenMode(corpus)
		sm := BuildSpanMemory(corpus)

		// Build cantileverSpan index
		var cspans []cantileverSpan
		for _, meta := range sm.Index {
			ep, conc := weightedCircularMean(eigenTable, meta.Text)
			cspans = append(cspans, cantileverSpan{
				tokens: meta.Tokens, source: meta.Source,
				spanLen: meta.Length, eigenPhase: ep, conc: conc,
			})
		}

		const maxChains = 10
		const minProgress = 2
		const cantileverThreshold = 0.3
		const headerPhaseThreshold = -0.15
		const bodyPhaseThreshold = -0.05
		const simCliffThreshold = 0.4
		const bridgePenalty = 0.3
		const bodyBoost = 0.15
		const progressEps = 0.05
		const wPhase, wNewRatio, wSimDelta = 2.0, 1.5, 0.5

		prompts := []struct{ prefix, desc string }{
			{"def factorial(n):", "Factorial"},
			{"def find_max(lst):", "Find max"},
			{"def binary_search(lst, target):", "Binary search"},
			{"def dfs(graph, start):", "DFS"},
			{"def insertion_sort(lst):", "Insertion sort"},
		}

		runArm := func(gated bool) []CantileverEntry {
			var entries []CantileverEntry
			for _, prompt := range prompts {
				currentOutput := prompt.prefix
				var chain []CantileverStep
				inBridge := false
				bridgeCount := 0
				prevSim := 1.0
				prevPhase, _ := weightedCircularMean(eigenTable, prompt.prefix)
				firstName := ""
				if i := strings.Index(prompt.prefix, "("); i > 4 {
					firstName = prompt.prefix[4:i]
				}

				for step := 0; step < maxChains; step++ {
					queryFP := geometry.NewPhaseDial().Encode(currentOutput)
					currentPhase, _ := weightedCircularMean(eigenTable, currentOutput)

					cantExt := 0
					if gated {
						cant := cantileverProbe(queryFP, cspans, sm.Substrate, numeric.NBasis, cantileverThreshold)
						cantExt = cant.MaxCoherentScale
						if cantExt == 0 {
							cantExt = numeric.FibWindows[0]
						}
					}

					if step > 0 && !inBridge {
						if prevSim < simCliffThreshold || currentPhase > headerPhaseThreshold {
							inBridge = true
							bridgeCount++
							if gated {
								cantExt = numeric.FibWindows[len(numeric.FibWindows)-1]
							}
						}
					}

					type cand struct {
						idx, ovl, newToks int
						rawSim, adjSim    float64
						phase, conc       float64
						spanLen           int
					}
					var candidates []cand
					seen := make(map[int]bool)
					for d := 0; d < 8; d++ {
						alpha := float64(d) * math.Pi / 8.0
						rotated := make(geometry.PhaseDial, len(queryFP))
						if alpha == 0 {
							copy(rotated, queryFP)
						} else {
							rot := complex(math.Cos(alpha), math.Sin(alpha))
							for k := range rotated {
								rotated[k] = queryFP[k] * rot
							}
						}
						for idx := range cspans {
							if seen[idx] {
								continue
							}
							if gated && cspans[idx].spanLen > cantExt {
								continue
							}
							s := sm.Substrate.Entries[idx].Fingerprint.Similarity(rotated)
							if s < 0.1 {
								continue
							}
							spanText := detokenize(cspans[idx].tokens)
							ovl := 0
							for o := min(len(currentOutput), len(spanText)); o > 0; o-- {
								if strings.HasSuffix(currentOutput, spanText[:o]) {
									ovl = o
									break
								}
							}
							newText := spanText[ovl:]
							newToks := len(tokenize(newText))
							if newToks < minProgress {
								continue
							}
							adjSim := s
							spanName := ""
							if src := cspans[idx].source; src < len(corpus) {
								if i2 := strings.Index(corpus[src], "("); i2 > 4 {
									spanName = corpus[src][4:i2]
								}
							}
							if step > 0 && firstName != "" && spanName != firstName {
								adjSim *= 0.5
							}
							if inBridge {
								if strings.HasPrefix(spanText, "def ") {
									adjSim -= bridgePenalty
								}
								if cspans[idx].eigenPhase > bodyPhaseThreshold {
									adjSim += bodyBoost * cspans[idx].conc
								}
							}
							candidates = append(candidates, cand{idx, ovl, newToks, s, adjSim,
								cspans[idx].eigenPhase, cspans[idx].conc, cspans[idx].spanLen})
							seen[idx] = true
						}
					}
					if len(candidates) == 0 {
						break
					}
					for i := 0; i < len(candidates); i++ {
						for j := i + 1; j < len(candidates); j++ {
							if candidates[j].adjSim > candidates[i].adjSim {
								candidates[i], candidates[j] = candidates[j], candidates[i]
							}
						}
					}

					accepted := false
					for _, c := range candidates {
						spanText := detokenize(cspans[c.idx].tokens)
						newText := spanText[c.ovl:]
						phaseDiff := c.phase - prevPhase
						angDist := math.Abs(math.Atan2(math.Sin(phaseDiff), math.Cos(phaseDiff)))
						newRatio := float64(c.newToks) / float64(c.spanLen)
						if len(newText) > 0 && strings.Contains(currentOutput, newText) {
							newRatio = 0
						}
						simDelta := c.rawSim - prevSim
						if simDelta < 0 {
							simDelta = 0
						}
						progress := wPhase*angDist + wNewRatio*newRatio + wSimDelta*simDelta
						if step > 0 && progress < progressEps {
							continue
						}
						currentOutput += newText
						prevSim = c.rawSim
						prevPhase = c.phase
						srcFn := ""
						if src := cspans[c.idx].source; src < len(corpus) {
							if i2 := strings.Index(corpus[src], "("); i2 > 4 {
								srcFn = corpus[src][4:i2]
							}
						}
						chain = append(chain, CantileverStep{
							StepNum: step + 1, SimScore: c.rawSim,
							SpanText: newText, NewTokens: c.newToks, Overlap: c.ovl,
							SourceFunc: srcFn, SpanLen: c.spanLen, CantExtent: cantExt,
							EigenPhase: c.phase, Progress: progress, InBridge: inBridge,
						})
						accepted = true
						break
					}
					if !accepted {
						break
					}
				}

				finalTokens := tokenize(currentOutput)
				hasReturn := strings.Contains(currentOutput, "return")
				hasLoop := strings.Contains(currentOutput, "for ") || strings.Contains(currentOutput, "while ")
				So(currentOutput, ShouldNotBeEmpty)
				entries = append(entries, CantileverEntry{
					Prefix: prompt.prefix, Desc: prompt.desc, FullText: currentOutput,
					ChainLength: len(chain), TotalTokens: len(finalTokens),
					HasReturn: hasReturn, HasLoop: hasLoop, BridgeCount: bridgeCount,
					Gated: gated, Chain: chain,
				})
			}
			return entries
		}

		Convey("When running control and gated arms", func() {
			controlEntries := runArm(false)
			gatedEntries := runArm(true)

			computeStats := func(entries []CantileverEntry) CantileverStats {
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

			Convey("Control and gated arms both produce output", func() {
				So(len(controlEntries), ShouldEqual, len(prompts))
				So(len(gatedEntries), ShouldEqual, len(prompts))
			})

			Convey("Artifacts should be written to the paper directory", func() {
				xAxis := make([]string, len(prompts))
				ctrlData := make([]float64, len(prompts))
				gatedData := make([]float64, len(prompts))
				for i := range prompts {
					xAxis[i] = controlEntries[i].Desc
					ctrlData[i] = float64(controlEntries[i].TotalTokens)
					gatedData[i] = float64(gatedEntries[i].TotalTokens)
				}
				So(WriteBarChart(xAxis, []projector.BarSeries{
					{Name: "Control", Data: ctrlData},
					{Name: "Gated", Data: gatedData},
				}, "Cantilever-Gated Retrieval",
					"Tokens generated per prompt, control vs cantilever-gated.",
					"fig:cantilever_gating", "cantilever_gating"), ShouldBeNil)

				tableRows := []map[string]any{
					{
						"Arm":         "Control",
						"MeanTokens":  fmt.Sprintf("%.1f", controlStats.MeanTokens),
						"HasReturn":   fmt.Sprintf("%d", controlStats.ReturnCount),
						"HasLoop":     fmt.Sprintf("%d", controlStats.LoopCount),
						"BridgeCount": fmt.Sprintf("%d", controlStats.BridgeCount),
					},
					{
						"Arm":         "Gated",
						"MeanTokens":  fmt.Sprintf("%.1f", gatedStats.MeanTokens),
						"HasReturn":   fmt.Sprintf("%d", gatedStats.ReturnCount),
						"HasLoop":     fmt.Sprintf("%d", gatedStats.LoopCount),
						"BridgeCount": fmt.Sprintf("%d", gatedStats.BridgeCount),
					},
				}
				So(WriteTable(tableRows, "cantilever_gating_summary.tex"), ShouldBeNil)
				_, statErr := os.Stat(filepath.Join(PaperDir(), "cantilever_gating_summary.tex"))
				So(statErr, ShouldBeNil)

				_ = CantileverResult{
					ControlEntries: controlEntries, GatedEntries: gatedEntries,
					ControlStats: controlStats, GatedStats: gatedStats,
				}
			})

			Convey("Artifact: write cantilever gating subsection prose", func() {
				tmpl, err := os.ReadFile("prose/cantilever_gating.tex.tmpl")
				So(err, ShouldBeNil)
				So(WriteProse(string(tmpl), map[string]any{
					"ControlMeanTokens": controlStats.MeanTokens,
					"GatedMeanTokens":   gatedStats.MeanTokens,
					"ControlBridges":    controlStats.BridgeCount,
					"GatedBridges":      gatedStats.BridgeCount,
				}, "cantilever_gating_prose.tex"), ShouldBeNil)
			})
		})
	})
}
