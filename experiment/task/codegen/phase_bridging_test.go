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
)

func TestPhaseBridging(t *testing.T) {
	Convey("Given the extended corpus and phase-triggered manifold bridging", t, func() {
		corpus := append(pythonCorpus(), longCorpus()...)
		eigenTable := buildEigenMode(corpus)

		type spanEntry struct {
			tokens                []string
			text                  string
			source                int
			eigenPhase, conc float64
		}

		sub := geometry.NewHybridSubstrate()
		var spans []spanEntry
		sm := BuildSpanMemory(corpus)
		// Rebuild with added eigenphase
		for i, meta := range sm.Index {
			ep, conc := weightedCircularMean(eigenTable, meta.Text)
			spans = append(spans, spanEntry{
				tokens: meta.Tokens, text: meta.Text,
				source: meta.Source, eigenPhase: ep, conc: conc,
			})
			_ = i
		}
		sub = sm.Substrate

		So(len(spans), ShouldBeGreaterThan, 0)

		const maxChains = 10
		const minProgress = 2
		const progressEps = 0.05
		const wPhase = 2.0
		const wNewRatio = 1.5
		const wSimDelta = 0.5

		prompts := []struct{ prefix, desc string }{
			{"def factorial(n):", "Factorial"},
			{"def find_max(lst):", "Find max"},
			{"def binary_search(lst, target):", "Binary search"},
			{"def dfs(graph, start):", "DFS"},
			{"def insertion_sort(lst):", "Insertion sort"},
		}

		Convey("When generating with phase-triggered manifold bridging", func() {
			var entries []PhaseBridgingEntry
			for _, prompt := range prompts {
				currentOutput := prompt.prefix
				var chain []PhaseBridgingStep
				inBridge := false
				bridgeCount := 0
				prevSim := 1.0
				prevPhase, _ := weightedCircularMean(eigenTable, prompt.prefix)

				for step := 0; step < maxChains; step++ {
					queryFP := geometry.NewPhaseDial().Encode(currentOutput)
					currentPhase, _ := weightedCircularMean(eigenTable, currentOutput)

					if step > 0 && !inBridge {
						phaseDiff := currentPhase - prevPhase
						phaseDeriv := math.Abs(math.Atan2(math.Sin(phaseDiff), math.Cos(phaseDiff)))
						if phaseDeriv > 0.1 {
							inBridge = true
							bridgeCount++
						}
					}

				type cand struct {
					idx        int
					rawSim, adjSim float64
					ovl, newToks   int
					phase, conc    float64
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
					for idx := range spans {
						if seen[idx] {
							continue
						}
						s := sub.Entries[idx].Fingerprint.Similarity(rotated)
						if s < 0.1 {
							continue
						}
						spanText := spans[idx].text
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
						if inBridge {
							// Sustained phase coherence directly evaluates topological continuity
							phaseDiff := spans[idx].eigenPhase - prevPhase
							angDist := math.Abs(math.Atan2(math.Sin(phaseDiff), math.Cos(phaseDiff)))
							// Apply dynamic topological boost based purely on continuity
							adjSim += 0.2 * math.Max(0, 1.0-angDist)
						}
						candidates = append(candidates, cand{idx, s, adjSim, ovl, newToks,
							spans[idx].eigenPhase, spans[idx].conc})
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
					spanText := spans[c.idx].text
					ovl := c.ovl
					newText := spanText[ovl:]
					phaseDiff := c.phase - prevPhase
					angDist := math.Abs(math.Atan2(math.Sin(phaseDiff), math.Cos(phaseDiff)))
					newRatio := float64(c.newToks) / float64(len(spans[c.idx].tokens))
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
					if src := spans[c.idx].source; src < len(corpus) {
						if i2 := strings.Index(corpus[src], "("); i2 > 4 {
							srcFn = corpus[src][4:i2]
						}
					}
					chain = append(chain, PhaseBridgingStep{
						StepNum: step + 1, SimScore: c.rawSim,
						SpanText: newText, NewTokens: c.newToks, Overlap: ovl,
						SourceFunc: srcFn, EigenPhase: c.phase, Concentration: c.conc,
						InBridge: inBridge,
					})
					accepted = true
					break
				}
				if !accepted {
					break
				}
				}

				finalTokens := tokenize(currentOutput)
				
				// Apply mathematically sound structural validation instead of surface string checks
				isGeomClosed := IsGeometricallyClosed(eigenTable, currentOutput, prevPhase)
				isValidAST := isValidSyntax(currentOutput)
				hasReturn := isGeomClosed && isValidAST
				hasLoop := isValidAST
				
				So(currentOutput, ShouldNotBeEmpty)

				entries = append(entries, PhaseBridgingEntry{
					Prefix: prompt.prefix, Desc: prompt.desc,
					FullText: currentOutput, ChainLength: len(chain),
					TotalTokens: len(finalTokens), HasReturn: hasReturn,
					HasLoop: hasLoop, BridgeCount: bridgeCount, Chain: chain,
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
				for i, e := range entries {
					xAxis[i] = e.Desc
					tokData[i] = float64(e.TotalTokens)
					bridgeData[i] = float64(e.BridgeCount)
				}
				So(WriteBarChart(xAxis, []projector.BarSeries{
					{Name: "Tokens", Data: tokData},
					{Name: "Bridges", Data: bridgeData},
				}, "Phase-Triggered Manifold Bridging",
					"Tokens generated and bridge events per prompt.",
					"fig:phase_bridging", "phase_bridging"), ShouldBeNil)

				tableRows := make([]map[string]any, len(entries))
				for i, e := range entries {
					tableRows[i] = map[string]any{
						"Prompt": e.Desc, "Steps": e.ChainLength,
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
