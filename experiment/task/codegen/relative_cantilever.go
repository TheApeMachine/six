package codegen

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/numeric"
)

// RelativeCantilever holds the relative scale analysis.
type RelativeCantilever struct {
	Scores     []float64 // similarity score per FibWindow scale (largest first)
	Ratios     []float64 // s(w)/s(w_smaller) for adjacent scale pairs
	MaxSafeLen int       // largest scale where ratio >= threshold
}

// relativeCantileverProbe computes the coherence profile and selects the
// maximum safe span length using a ratio criterion.
//
// Instead of "does s(w) exceed 0.3?", it asks:
// "does s(w)/s(w_smaller) exceed the ratio threshold?"
//
// This catches cases where s(21) = 0.4 (passes absolute) but
// s(21)/s(13) = 0.47 (fails ratio) — meaning the jump from 13→21
// loses too much structural coherence to be safe.
func relativeCantileverProbe(
	boundaryFP geometry.PhaseDial,
	spanIndex []cantileverSpan,
	substrate *geometry.HybridSubstrate,
	D int,
	ratioThreshold float64,
) RelativeCantilever {
	sim := func(a, b geometry.PhaseDial) float64 {
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

	// Compute scores for each FibWindow scale (smallest first for ratio computation)
	windows := make([]int, len(numeric.FibWindows))
	copy(windows, numeric.FibWindows)
	sort.Ints(windows) // ascending: 3, 5, 8, 13, 21

	scores := make([]float64, len(windows))
	for wi, w := range windows {
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
		scores[wi] = bestSim
	}

	// Compute ratios between adjacent scales
	// ratios[i] = scores[i+1] / scores[i] (how much coherence is lost going larger)
	ratios := make([]float64, len(windows)-1)
	for i := 0; i < len(windows)-1; i++ {
		if scores[i] > 0 {
			ratios[i] = scores[i+1] / scores[i]
		}
		// if scores[i] == 0, ratio stays 0 → will fail threshold
	}

	// Walk from smallest to largest: accept the scale if the ratio to get there is OK
	maxSafe := windows[0] // minimum: smallest FibWindow
	for i, r := range ratios {
		if r >= ratioThreshold {
			maxSafe = windows[i+1]
		} else {
			break // first bad ratio → stop
		}
	}

	// Reverse scores for display (largest first, matching Test 10 format)
	reversedScores := make([]float64, len(scores))
	for i, s := range scores {
		reversedScores[len(scores)-1-i] = s
	}

	return RelativeCantilever{
		Scores:     reversedScores,
		Ratios:     ratios,
		MaxSafeLen: maxSafe,
	}
}

// RelCantStep records one step of the relative-cantilever chainer.
type RelCantStep struct {
	StepNum    int     `json:"step"`
	SimScore   float64 `json:"sim_score"`
	SpanText   string  `json:"span_text"`
	NewTokens  int     `json:"new_tokens"`
	Overlap    int     `json:"overlap"`
	SourceFunc string  `json:"source_func"`
	SpanLen    int     `json:"span_len"`
	MaxSafe    int     `json:"max_safe"`
	EigenPhase float64 `json:"eigen_phase"`
	Progress   float64 `json:"progress"`
	InBridge   bool    `json:"in_bridge"`
}

// RelCantEntry records one prompt's generation.
type RelCantEntry struct {
	Prefix      string        `json:"prefix"`
	Desc        string        `json:"desc"`
	FullText    string        `json:"full_text"`
	ChainLength int           `json:"chain_length"`
	TotalTokens int           `json:"total_tokens"`
	HasReturn   bool          `json:"has_return"`
	HasLoop     bool          `json:"has_loop"`
	BridgeCount int           `json:"bridge_count"`
	Gated       bool          `json:"gated"`
	Chain       []RelCantStep `json:"chain"`
}

// RelCantResult holds all Test 11 results.
type RelCantResult struct {
	ControlEntries []RelCantEntry  `json:"control"`
	GatedEntries   []RelCantEntry  `json:"gated"`
	ControlStats   CantileverStats `json:"control_stats"`
	GatedStats     CantileverStats `json:"gated_stats"`
}

// testRelativeCantilever implements Test 11: Relative Cantilever Scale Selection.
//
// Replaces absolute-threshold cantilever (Test 10) with ratio-based selection:
// s(w_large) / s(w_small) must exceed a ratio threshold for the larger scale
// to be allowed. This catches cases where absolute similarity is acceptable
// but the coherence drop between scales is too steep.
func (experiment *Experiment) testRelativeCantilever(corpus []string) RelCantResult {
	D := numeric.NBasis

	eigenTable := buildEigenPhaseTable(corpus)

	spanLengths := numeric.FibWindows
	const topK = 64
	const nDial = 8
	const maxChains = 10
	const minProgress = 2

	const headerPhaseThreshold = -0.15
	const bodyPhaseThreshold = -0.05
	const simCliffThreshold = 0.4
	const bridgePenalty = 0.3
	const bodyBoost = 0.15

	const progressEps = 0.05
	const wPhase = 2.0
	const wNewRatio = 1.5
	const wSimDelta = 0.5

	// Ratio threshold: s(large)/s(small) must be ≥ this to allow the larger scale
	const ratioThreshold = 0.7

	substrate := geometry.NewHybridSubstrate()
	var universalFilter data.Chord

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
				fp := geometry.NewPhaseDial().Encode(spanText)
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

	sim := func(a, b geometry.PhaseDial) float64 {
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

	// Run one arm
	runArm := func(gated bool) []RelCantEntry {
		label := "CONTROL"
		if gated {
			label = "REL-GATED"
		}
		console.Info(fmt.Sprintf("\n  ═══ %s arm ═══", label))

		var entries []RelCantEntry

		for _, prompt := range prompts {
			console.Info(fmt.Sprintf("\n  ┌─ [%s] %s", label, prompt.desc))

			currentOutput := prompt.prefix
			var chain []RelCantStep
			inBridge := false
			bridgeCount := 0
			prevSim := 1.0
			prevPhase, _ := eigenTable.weightedCircularMean(prompt.prefix)

			firstName := ""
			if i := strings.Index(prompt.prefix, "("); i > 4 {
				firstName = prompt.prefix[4:i]
			}

			for step := 0; step < maxChains; step++ {
				queryFP := geometry.NewPhaseDial().Encode(currentOutput)
				currentPhase, currentConc := eigenTable.weightedCircularMean(currentOutput)

				// Relative cantilever (only in gated arm)
				maxSafe := numeric.FibWindows[len(numeric.FibWindows)-1] // default: max
				if gated {
					cant := relativeCantileverProbe(queryFP, spanIndex, substrate, D, ratioThreshold)
					maxSafe = cant.MaxSafeLen
					if step == 0 {
						console.Info(fmt.Sprintf("  │  Cantilever: maxSafe=%d, scores=%v, ratios=%v",
							cant.MaxSafeLen, formatScores(cant.Scores), formatScores(cant.Ratios)))
					}
				}

				// Bridge detection
				if step > 0 && !inBridge {
					if prevSim < simCliffThreshold || currentPhase > headerPhaseThreshold {
						inBridge = true
						bridgeCount++
						console.Info(fmt.Sprintf("  │  ⚡ BRIDGE at step %d (φ=%.2f, R=%.2f, sim=%.3f)",
							step+1, currentPhase, currentConc, prevSim))
						// In bridge mode, relax gating to allow manifold crossing
						if gated {
							maxSafe = numeric.FibWindows[len(numeric.FibWindows)-1]
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
					rotated := make(geometry.PhaseDial, D)
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

						// GATING: skip spans longer than max safe length
						if gated && spanIndex[idx].spanLen > maxSafe {
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

				// Progress-aware acceptance
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
						gateLabel = fmt.Sprintf(" [≤%d]", maxSafe)
					}

					srcFn := extractFuncName(cand.idx)
					console.Info(fmt.Sprintf("  │  Step %d (sim=%.3f, φ=%.2f, prog=%.3f, len=%d): +[%s] ← %s%s%s",
						step+1, cand.rawSim, cand.phase, progress, cand.spanLen,
						truncStr(newText, 40), srcFn, bridgeLabel, gateLabel))

					chain = append(chain, RelCantStep{
						StepNum:    step + 1,
						SimScore:   cand.rawSim,
						SpanText:   newText,
						NewTokens:  cand.newToks,
						Overlap:    ovl,
						SourceFunc: srcFn,
						SpanLen:    cand.spanLen,
						MaxSafe:    maxSafe,
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

			entries = append(entries, RelCantEntry{
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

	computeStats := func(entries []RelCantEntry) CantileverStats {
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

	console.Info("\n  ── Control vs Relative-Gated ──")
	console.Info(fmt.Sprintf("  %-14s  %8s  %8s", "", "Control", "Rel-Gate"))
	console.Info(fmt.Sprintf("  %-14s  %8.1f  %8.1f", "Mean tokens", controlStats.MeanTokens, gatedStats.MeanTokens))
	console.Info(fmt.Sprintf("  %-14s  %8d  %8d", "Has return", controlStats.ReturnCount, gatedStats.ReturnCount))
	console.Info(fmt.Sprintf("  %-14s  %8d  %8d", "Has loop", controlStats.LoopCount, gatedStats.LoopCount))
	console.Info(fmt.Sprintf("  %-14s  %8d  %8d", "Bridges", controlStats.BridgeCount, gatedStats.BridgeCount))

	return RelCantResult{
		ControlEntries: controlEntries,
		GatedEntries:   gatedEntries,
		ControlStats:   controlStats,
		GatedStats:     gatedStats,
	}
}
