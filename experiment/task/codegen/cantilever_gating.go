package codegen

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/numeric"
)

// CantileverEstimate holds the result of a cantilever probe at the prompt boundary.
type CantileverEstimate struct {
	MaxCoherentScale int       // largest FibWindow scale where similarity > threshold
	Extent           int       // sum of all coherent scales
	ScaleScores      []float64 // similarity score per FibWindow scale (largest first)
}

// cantileverProbe estimates how far the boundary's structural signal can propagate.
//
// For each FibWindow scale w (largest first: 21, 13, 8, 5, 3), it finds the
// best span of exactly length w in memory and computes its similarity to the
// boundary fingerprint. Coherent scales (sim > threshold) extend the beam;
// destructive scales (sim ≤ threshold) break it.
//
// This is the bitwise equivalent of the old wave-interference cantilever:
// instead of cos(2πw/λ), we measure fingerprint overlap at each scale.
func cantileverProbe(
	boundaryFP numeric.PhaseDial,
	spanIndex []cantileverSpan,
	substrate *numeric.HybridSubstrate,
	D int,
	threshold float64,
) CantileverEstimate {
	sim := func(a, b numeric.PhaseDial) float64 {
		var dot complex128
		var na, nb float64
		for i := 0; i < D; i++ {
			dot += complex(real(a[i]), -imag(a[i])) * b[i]
			na += real(a[i])*real(a[i]) + imag(a[i])*imag(a[i])
			nb += real(b[i])*real(b[i]) + imag(b[i])*imag(b[i])
		}
		if na == 0 || nb == 0 {
			return 0
		}
		return real(dot) / (math.Sqrt(na) * math.Sqrt(nb))
	}

	// Walk FibWindows largest → smallest (like the old cantilever)
	reversed := make([]int, len(numeric.FibWindows))
	copy(reversed, numeric.FibWindows)
	sort.Sort(sort.Reverse(sort.IntSlice(reversed)))

	var scores []float64
	maxCoherent := 0
	extent := 0

	for _, w := range reversed {
		// Find the best span of exactly this length
		bestSim := 0.0
		for idx, sp := range spanIndex {
			if sp.spanLen != w {
				continue
			}
			s := sim(boundaryFP, substrate.Entries[idx].Fingerprint)
			if s > bestSim {
				bestSim = s
			}
		}

		scores = append(scores, bestSim)

		if bestSim > threshold {
			if w > maxCoherent {
				maxCoherent = w
			}
			extent += w
		}
		// Note: unlike old cantilever we do NOT break here — we check all scales.
		// The old system broke on first destructive; here we want to see the full profile.
	}

	return CantileverEstimate{
		MaxCoherentScale: maxCoherent,
		Extent:           extent,
		ScaleScores:      scores,
	}
}

// cantileverSpan holds span metadata for the cantilever test.
type cantileverSpan struct {
	tokens     []string
	source     int
	spanLen    int
	eigenPhase float64
	conc       float64
}

// CantileverStep records one step of the cantilever-gated chainer.
type CantileverStep struct {
	StepNum       int     `json:"step"`
	SimScore      float64 `json:"sim_score"`
	SpanText      string  `json:"span_text"`
	NewTokens     int     `json:"new_tokens"`
	Overlap       int     `json:"overlap"`
	SourceFunc    string  `json:"source_func"`
	SpanLen       int     `json:"span_len"`
	CantExtent    int     `json:"cant_extent"`
	EigenPhase    float64 `json:"eigen_phase"`
	Progress      float64 `json:"progress"`
	InBridge      bool    `json:"in_bridge"`
}

// CantileverEntry records one prompt's generation.
type CantileverEntry struct {
	Prefix      string           `json:"prefix"`
	Desc        string           `json:"desc"`
	FullText    string           `json:"full_text"`
	ChainLength int              `json:"chain_length"`
	TotalTokens int              `json:"total_tokens"`
	HasReturn   bool             `json:"has_return"`
	HasLoop     bool             `json:"has_loop"`
	BridgeCount int              `json:"bridge_count"`
	Gated       bool             `json:"gated"`
	Chain       []CantileverStep `json:"chain"`
}

// CantileverResult holds all Test 10 results.
type CantileverResult struct {
	ControlEntries []CantileverEntry `json:"control"`
	GatedEntries   []CantileverEntry `json:"gated"`
	ControlStats   CantileverStats   `json:"control_stats"`
	GatedStats     CantileverStats   `json:"gated_stats"`
}

// CantileverStats holds aggregate metrics for one arm of the experiment.
type CantileverStats struct {
	MeanTokens  float64 `json:"mean_tokens"`
	ReturnCount int     `json:"return_count"`
	LoopCount   int     `json:"loop_count"`
	BridgeCount int     `json:"bridge_count"`
}

// testCantileverGating implements Test 10: Cantilever-Gated Span Retrieval.
//
// Runs the same generation prompts twice:
//   - CONTROL: retrieve spans of any FibWindow length (current behavior)
//   - GATED: run cantilever probe at each step, only retrieve spans ≤ extent
//
// Both arms use the same progress filter and eigenphase bridging from Test 9.
// The comparison shows whether cantilever gating reduces wrong-body jumps.
func (experiment *Experiment) testCantileverGating(corpus []string) CantileverResult {
	D := numeric.NBasis

	// Build eigenphase table
	eigenTable := buildEigenPhaseTable(corpus)

	// Build span memory
	spanLengths := numeric.FibWindows
	const topK = 64
	const nDial = 8
	const maxChains = 10
	const minProgress = 2

	// Bridge thresholds (from Test 9)
	const headerPhaseThreshold = -0.15
	const bodyPhaseThreshold = -0.05
	const simCliffThreshold = 0.4
	const bridgePenalty = 0.3
	const bodyBoost = 0.15

	// Progress filter (from Test 9)
	const progressEps = 0.05
	const wPhase = 2.0
	const wNewRatio = 1.5
	const wSimDelta = 0.5

	// Cantilever threshold
	const cantileverThreshold = 0.3

	substrate := numeric.NewHybridSubstrate()
	var universalFilter numeric.Chord

	var spanIndex []cantileverSpan

	totalSpans := 0
	for corpIdx, fn := range corpus {
		tokens := tokenize(fn)
		for _, sLen := range spanLengths {
			if len(tokens) < sLen {
				continue
			}
			for start := 0; start <= len(tokens)-sLen; start++ {
				span := make([]string, sLen)
				copy(span, tokens[start:start+sLen])
				spanText := detokenize(span)
				fp := numeric.EncodeText(spanText)
				readout := []byte(spanText)
				substrate.Add(universalFilter, fp, readout)

				ep, conc := eigenTable.weightedCircularMean(spanText)
				spanIndex = append(spanIndex, cantileverSpan{
					tokens:     span,
					source:     corpIdx,
					spanLen:    sLen,
					eigenPhase: ep,
					conc:       conc,
				})
				totalSpans++
			}
		}
	}

	console.Info(fmt.Sprintf("  Span memory: %d spans (lengths %v, corpus %d)", totalSpans, spanLengths, len(corpus)))

	// Similarity function
	sim := func(a, b numeric.PhaseDial) float64 {
		var dot complex128
		var na, nb float64
		for i := 0; i < D; i++ {
			dot += complex(real(a[i]), -imag(a[i])) * b[i]
			na += real(a[i])*real(a[i]) + imag(a[i])*imag(a[i])
			nb += real(b[i])*real(b[i]) + imag(b[i])*imag(b[i])
		}
		if na == 0 || nb == 0 {
			return 0
		}
		return real(dot) / (math.Sqrt(na) * math.Sqrt(nb))
	}

	extractFuncName := func(idx int) string {
		src := spanIndex[idx].source
		if src < len(corpus) {
			fn := corpus[src]
			if i := strings.Index(fn, "("); i > 0 {
				return fn[4:i]
			}
		}
		return "?"
	}

	longestOverlap := func(output, span string) int {
		maxOvl := len(output)
		if maxOvl > len(span) {
			maxOvl = len(span)
		}
		for ovl := maxOvl; ovl > 0; ovl-- {
			if strings.HasSuffix(output, span[:ovl]) {
				return ovl
			}
		}
		return 0
	}

	type testPrompt struct {
		prefix string
		desc   string
	}
	prompts := []testPrompt{
		{"def factorial(n):", "Factorial"},
		{"def find_max(lst):", "Find max"},
		{"def binary_search(lst, target):", "Binary search"},
		{"def dfs(graph, start):", "DFS"},
		{"def insertion_sort(lst):", "Insertion sort"},
	}

	// Run one arm of the experiment
	runArm := func(gated bool) []CantileverEntry {
		label := "CONTROL"
		if gated {
			label = "GATED"
		}
		console.Info(fmt.Sprintf("\n  ═══ %s arm ═══", label))

		var entries []CantileverEntry

		for _, prompt := range prompts {
			console.Info(fmt.Sprintf("\n  ┌─ [%s] %s", label, prompt.desc))

			currentOutput := prompt.prefix
			var chain []CantileverStep
			inBridge := false
			bridgeCount := 0
			prevSim := 1.0
			prevPhase, _ := eigenTable.weightedCircularMean(prompt.prefix)

			firstName := ""
			if i := strings.Index(prompt.prefix, "("); i > 4 {
				firstName = prompt.prefix[4:i]
			}

			for step := 0; step < maxChains; step++ {
				queryFP := numeric.EncodeText(currentOutput)
				currentPhase, currentConc := eigenTable.weightedCircularMean(currentOutput)

				// Cantilever probe (only used in gated arm)
				cantExt := 0
				if gated {
					cant := cantileverProbe(queryFP, spanIndex, substrate, D, cantileverThreshold)
					cantExt = cant.MaxCoherentScale
					if cantExt == 0 {
						cantExt = numeric.FibWindows[0] // minimum: smallest FibWindow
					}
					if step == 0 {
						console.Info(fmt.Sprintf("  │  Cantilever: extent=%d, scores=%v",
							cant.MaxCoherentScale, formatScores(cant.ScaleScores)))
					}
				}

				// Bridge detection
				if step > 0 && !inBridge {
					if prevSim < simCliffThreshold || currentPhase > headerPhaseThreshold {
						inBridge = true
						bridgeCount++
						console.Info(fmt.Sprintf("  │  ⚡ BRIDGE at step %d (φ=%.2f, R=%.2f, sim=%.3f)",
							step+1, currentPhase, currentConc, prevSim))

						// In bridge mode with gating, expand the allowed scale
						if gated {
							cantExt = numeric.FibWindows[len(numeric.FibWindows)-1] // allow largest
						}
					}
				}

				// Retrieve candidates
				type candidate struct {
					idx     int
					rawSim  float64
					adjSim  float64
					ovl     int
					newToks int
					spanLen int
					phase   float64
					conc    float64
				}
				var candidates []candidate

				angles := make([]float64, nDial)
				for d := 0; d < nDial; d++ {
					angles[d] = float64(d) * math.Pi / float64(nDial)
				}

				seen := make(map[int]bool)

				for _, alpha := range angles {
					rotated := make(numeric.PhaseDial, D)
					if alpha == 0 {
						copy(rotated, queryFP)
					} else {
						rot := complex(math.Cos(alpha), math.Sin(alpha))
						for i := 0; i < D; i++ {
							rotated[i] = queryFP[i] * rot
						}
					}

					for idx := range spanIndex {
						if seen[idx] {
							continue
						}

						// GATING: skip spans longer than cantilever extent
						if gated && spanIndex[idx].spanLen > cantExt {
							continue
						}

						entryFP := substrate.Entries[idx].Fingerprint
						s := sim(rotated, entryFP)
						if s < 0.1 {
							continue
						}

						spanText := detokenize(spanIndex[idx].tokens)
						ovl := longestOverlap(currentOutput, spanText)
						newText := spanText[ovl:]
						newToks := len(tokenize(newText))
						if newToks < minProgress {
							continue
						}

						adjSim := s

						spanFuncName := extractFuncName(idx)
						if step > 0 && firstName != "" && spanFuncName != firstName {
							adjSim *= 0.5
						}

						if inBridge {
							if strings.HasPrefix(spanText, "def ") {
								adjSim -= bridgePenalty
							}
							if spanIndex[idx].eigenPhase > bodyPhaseThreshold {
								adjSim += bodyBoost * spanIndex[idx].conc
							}
							if spanIndex[idx].eigenPhase < headerPhaseThreshold-0.2 {
								adjSim -= 0.1
							}
						}

						candidates = append(candidates, candidate{
							idx:     idx,
							rawSim:  s,
							adjSim:  adjSim,
							ovl:     ovl,
							newToks: newToks,
							spanLen: spanIndex[idx].spanLen,
							phase:   spanIndex[idx].eigenPhase,
							conc:    spanIndex[idx].conc,
						})
						seen[idx] = true
					}
				}

				if len(candidates) == 0 {
					break
				}

				sort.Slice(candidates, func(a, b int) bool {
					return candidates[a].adjSim > candidates[b].adjSim
				})

				if len(candidates) > topK {
					candidates = candidates[:topK]
				}

				// Progress-aware acceptance (from Test 9)
				accepted := false
				for _, cand := range candidates {
					spanText := detokenize(spanIndex[cand.idx].tokens)
					ovl := cand.ovl
					newText := spanText[ovl:]

					phaseDiff := cand.phase - prevPhase
					angularDist := math.Abs(math.Atan2(math.Sin(phaseDiff), math.Cos(phaseDiff)))
					newRatio := float64(cand.newToks) / float64(cand.spanLen)

					if len(newText) > 0 && strings.Contains(currentOutput, newText) {
						newRatio = 0
					}

					simDelta := cand.rawSim - prevSim
					if simDelta < 0 {
						simDelta = 0
					}

					progress := wPhase*angularDist + wNewRatio*newRatio + wSimDelta*simDelta

					if step > 0 && progress < progressEps {
						continue
					}

					currentOutput += newText
					prevSim = cand.rawSim
					prevPhase = cand.phase

					bridgeLabel := ""
					if inBridge {
						bridgeLabel = " [B]"
					}
					gateLabel := ""
					if gated {
						gateLabel = fmt.Sprintf(" [≤%d]", cantExt)
					}

					srcFn := extractFuncName(cand.idx)
					console.Info(fmt.Sprintf("  │  Step %d (sim=%.3f, φ=%.2f, prog=%.3f, len=%d): +[%s] ← %s%s%s",
						step+1, cand.rawSim, cand.phase, progress, cand.spanLen,
						truncStr(newText, 40), srcFn, bridgeLabel, gateLabel))

					chain = append(chain, CantileverStep{
						StepNum:    step + 1,
						SimScore:   cand.rawSim,
						SpanText:   newText,
						NewTokens:  cand.newToks,
						Overlap:    ovl,
						SourceFunc: srcFn,
						SpanLen:    cand.spanLen,
						CantExtent: cantExt,
						EigenPhase: cand.phase,
						Progress:   progress,
						InBridge:   inBridge,
					})
					accepted = true
					break
				}

				if !accepted {
					console.Info(fmt.Sprintf("  │  Step %d: no progress (checked %d)", step+1, len(candidates)))
					break
				}
			}

			finalTokens := tokenize(currentOutput)
			hasReturn := strings.Contains(currentOutput, "return")
			hasLoop := strings.Contains(currentOutput, "for ") || strings.Contains(currentOutput, "while ")

			displayText := currentOutput
			if len(displayText) > 100 {
				displayText = displayText[:100] + "…"
			}
			console.Info(fmt.Sprintf("  └─ %d tokens, return=%v, loop=%v, bridges=%d: %s",
				len(finalTokens), hasReturn, hasLoop, bridgeCount, displayText))

			entries = append(entries, CantileverEntry{
				Prefix:      prompt.prefix,
				Desc:        prompt.desc,
				FullText:    currentOutput,
				ChainLength: len(chain),
				TotalTokens: len(finalTokens),
				HasReturn:   hasReturn,
				HasLoop:     hasLoop,
				BridgeCount: bridgeCount,
				Gated:       gated,
				Chain:       chain,
			})
		}

		return entries
	}

	controlEntries := runArm(false)
	gatedEntries := runArm(true)

	// Compute stats
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

	// Comparison
	console.Info("\n  ── Control vs Gated ──")
	console.Info(fmt.Sprintf("  %-14s  %8s  %8s", "", "Control", "Gated"))
	console.Info(fmt.Sprintf("  %-14s  %8.1f  %8.1f", "Mean tokens", controlStats.MeanTokens, gatedStats.MeanTokens))
	console.Info(fmt.Sprintf("  %-14s  %8d  %8d", "Has return", controlStats.ReturnCount, gatedStats.ReturnCount))
	console.Info(fmt.Sprintf("  %-14s  %8d  %8d", "Has loop", controlStats.LoopCount, gatedStats.LoopCount))
	console.Info(fmt.Sprintf("  %-14s  %8d  %8d", "Bridges", controlStats.BridgeCount, gatedStats.BridgeCount))

	return CantileverResult{
		ControlEntries: controlEntries,
		GatedEntries:   gatedEntries,
		ControlStats:   controlStats,
		GatedStats:     gatedStats,
	}
}

func formatScores(scores []float64) string {
	parts := make([]string, len(scores))
	for i, s := range scores {
		parts[i] = fmt.Sprintf("%.3f", s)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func truncStr(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", "↵")
	if len(s) > maxLen {
		return s[:maxLen] + "…"
	}
	return s
}
