package codegen

import (
	"fmt"
	"math"
	"math/cmplx"
	"sort"
	"strings"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/numeric"
	"gonum.org/v1/gonum/mat"
)

// eigenPhaseTable holds precomputed per-byte eigenphases and structural weights.
type eigenPhaseTable struct {
	phase  [256]float64
	weight [256]float64 // structural informativeness weight per byte
}

// buildEigenPhaseTable constructs the multi-scale transition eigenphase table.
func buildEigenPhaseTable(corpus []string) eigenPhaseTable {
	const NSymbols = 256

	var fullCorpus []byte
	for _, fn := range corpus {
		fullCorpus = append(fullCorpus, []byte(fn)...)
		fullCorpus = append(fullCorpus, '\n')
	}

	var sinAcc, cosAcc [NSymbols]float64

	for wi, w := range numeric.FibWindows {
		wt := numeric.FibWeights[wi]

		var T [NSymbols][NSymbols]float64
		for pos := 0; pos < len(fullCorpus); pos++ {
			sym := fullCorpus[pos]
			end := pos + w + 1
			if end > len(fullCorpus) {
				end = len(fullCorpus)
			}
			for j := pos + 1; j < end; j++ {
				T[sym][fullCorpus[j]] += 1.0
			}
		}

		for i := 0; i < NSymbols; i++ {
			var sum float64
			for j := 0; j < NSymbols; j++ {
				sum += T[i][j]
			}
			if sum > 0 {
				for j := 0; j < NSymbols; j++ {
					T[i][j] /= sum
				}
			}
		}

		data := make([]float64, NSymbols*NSymbols)
		for i := 0; i < NSymbols; i++ {
			for j := 0; j < NSymbols; j++ {
				data[i*NSymbols+j] = T[i][j]
			}
		}
		dense := mat.NewDense(NSymbols, NSymbols, data)

		var eig mat.Eigen
		if !eig.Factorize(dense, mat.EigenRight) {
			continue
		}

		values := eig.Values(nil)
		indices := make([]int, NSymbols)
		for i := range indices {
			indices[i] = i
		}
		sort.Slice(indices, func(a, b int) bool {
			return cmplx.Abs(values[indices[a]]) > cmplx.Abs(values[indices[b]])
		})

		var vecs mat.CDense
		eig.VectorsTo(&vecs)

		idx1 := indices[1]
		lam1 := values[idx1]

		var v2, v3 [NSymbols]float64
		if imag(lam1) != 0 {
			for i := 0; i < NSymbols; i++ {
				v := vecs.At(i, idx1)
				v2[i] = real(v)
				v3[i] = imag(v)
			}
		} else {
			idx2 := indices[2]
			if imag(values[idx2]) != 0 {
				for i := 0; i < NSymbols; i++ {
					v := vecs.At(i, idx2)
					v2[i] = real(v)
					v3[i] = imag(v)
				}
			} else {
				for i := 0; i < NSymbols; i++ {
					v2[i] = real(vecs.At(i, idx1))
					v3[i] = real(vecs.At(i, idx2))
				}
			}
		}

		normalizeVec256(&v2)
		normalizeVec256(&v3)

		for i := 0; i < NSymbols; i++ {
			phase := math.Atan2(v3[i], v2[i])
			sinAcc[i] += wt * math.Sin(phase)
			cosAcc[i] += wt * math.Cos(phase)
		}
	}

	var table eigenPhaseTable
	for i := 0; i < NSymbols; i++ {
		table.phase[i] = math.Atan2(sinAcc[i], cosAcc[i])
	}

	// Structural informativeness weights:
	// Punctuation/operators = 1.0, letters/digits = 0.5, whitespace = 0.2
	for i := 0; i < NSymbols; i++ {
		b := byte(i)
		switch {
		case b == ' ' || b == '\n' || b == '\t' || b == '\r':
			table.weight[i] = 0.2
		case (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_':
			table.weight[i] = 0.5
		default: // punctuation, operators
			table.weight[i] = 1.0
		}
	}

	return table
}

// weightedCircularMean computes the weighted circular mean and concentration R.
func (t *eigenPhaseTable) weightedCircularMean(text string) (phase float64, concentration float64) {
	var sinSum, cosSum, wSum float64
	for _, b := range []byte(text) {
		w := t.weight[b]
		p := t.phase[b]
		sinSum += w * math.Sin(p)
		cosSum += w * math.Cos(p)
		wSum += w
	}
	if wSum == 0 {
		return 0, 0
	}
	phase = math.Atan2(sinSum, cosSum)
	concentration = math.Sqrt(sinSum*sinSum+cosSum*cosSum) / wSum
	return
}

// PhaseBridgingStep records one step of the phase-bridging chainer.
type PhaseBridgingStep struct {
	StepNum       int     `json:"step"`
	SimScore      float64 `json:"sim_score"`
	SpanText      string  `json:"span_text"`
	NewTokens     int     `json:"new_tokens"`
	Overlap       int     `json:"overlap"`
	SourceFunc    string  `json:"source_func"`
	EigenPhase    float64 `json:"eigen_phase"`
	Concentration float64 `json:"concentration"`
	InBridge      bool    `json:"in_bridge"`
}

// PhaseBridgingEntry records one prompt's generation.
type PhaseBridgingEntry struct {
	Prefix      string              `json:"prefix"`
	Desc        string              `json:"desc"`
	FullText    string              `json:"full_text"`
	ChainLength int                 `json:"chain_length"`
	TotalTokens int                 `json:"total_tokens"`
	HasReturn   bool                `json:"has_return"`
	HasLoop     bool                `json:"has_loop"`
	BridgeCount int                 `json:"bridge_count"`
	Chain       []PhaseBridgingStep `json:"chain"`
}

// PhaseBridgingResult holds all Test 9 results.
type PhaseBridgingResult struct {
	Entries     []PhaseBridgingEntry `json:"entries"`
	MeanTokens  float64              `json:"mean_tokens"`
	ReturnCount int                  `json:"return_count"`
	LoopCount   int                  `json:"loop_count"`
	BridgeTotal int                  `json:"bridge_total"`
}

// testPhaseBridging implements Test 9: Phase-triggered Manifold Bridging.
//
// Combines overlap-aware span chaining with eigenphase tracking.
// When the system detects a manifold transition (eigenphase crossing or
// similarity cliff), it enters "bridge mode" which:
//   - suppresses spans starting with "def "
//   - boosts candidates whose eigenphase is in the body hemisphere
//   - uses PhaseDial sweep for broader retrieval
func (experiment *Experiment) testPhaseBridging(corpus []string) PhaseBridgingResult {
	D := numeric.NBasis

	// Build eigenphase table
	eigenTable := buildEigenPhaseTable(corpus)

	// Build span memory
	spanLengths := numeric.FibWindows
	const topK = 64
	const nDial = 8
	const maxChains = 10
	const minProgress = 2

	// Manifold bridging thresholds
	const headerPhaseThreshold = -0.15 // below this = header hemisphere
	const bodyPhaseThreshold = -0.05   // above this = body hemisphere
	const simCliffThreshold = 0.4      // similarity drop triggers bridge
	const bridgePenalty = 0.3          // penalty for header-starting spans in bridge mode
	const bodyBoost = 0.15             // boost for body-hemisphere spans in bridge mode

	substrate := geometry.NewHybridSubstrate()
	var universalFilter data.Chord

	type spanMeta struct {
		tokens        []string
		source        int
		eigenPhase    float64 // precomputed weighted eigenphase
		concentration float64
	}
	var spanIndex []spanMeta

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
				spanIndex = append(spanIndex, spanMeta{
					tokens:        span,
					source:        corpIdx,
					eigenPhase:    ep,
					concentration: conc,
				})
				totalSpans++
			}
		}
	}

	console.Info(fmt.Sprintf("  Span memory: %d spans (lengths %v, corpus %d)", totalSpans, spanLengths, len(corpus)))

	// Similarity function
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

	// Function name extractor for logging
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

	// Overlap detection
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

	// Test prompts — same as overlap chaining but with bridging
	type testPrompt struct {
		prefix string
		desc   string
	}
	prompts := []testPrompt{
		{"def factorial(n):", "Factorial — should bridge to recursion body"},
		{"def find_max(lst):", "Find max — should bridge to loop body"},
		{"def binary_search(lst, target):", "Binary search — should bridge to while loop"},
		{"def dfs(graph, start):", "DFS — should bridge to stack loop"},
		{"def insertion_sort(lst):", "Insertion sort — should bridge to nested loop"},
	}

	var entries []PhaseBridgingEntry
	var totalReturnCount, totalLoopCount, totalBridgeCount int

	for _, prompt := range prompts {
		console.Info(fmt.Sprintf("\n  ┌─ %s", prompt.desc))

		currentOutput := prompt.prefix
		var chain []PhaseBridgingStep
		inBridge := false
		bridgeCount := 0
		prevSim := 1.0
		prevPhase := 0.0

		// Track the first function name for name-lock (like Test 4)
		firstName := ""
		if i := strings.Index(prompt.prefix, "("); i > 4 {
			firstName = prompt.prefix[4:i]
		}

		// Initial eigenphase
		prevPhase, _ = eigenTable.weightedCircularMean(prompt.prefix)

		for step := 0; step < maxChains; step++ {
			queryText := currentOutput
			queryFP := geometry.NewPhaseDial().Encode(queryText)

			// Compute current output eigenphase
			currentPhase, currentConc := eigenTable.weightedCircularMean(currentOutput)

			// Bridge detection: eigenphase crossing OR similarity cliff
			if step > 0 && !inBridge {
				if prevSim < simCliffThreshold || currentPhase > headerPhaseThreshold {
					inBridge = true
					bridgeCount++
					console.Info(fmt.Sprintf("  │  ⚡ BRIDGE MODE at step %d (phase=%.2f, R=%.2f, prevSim=%.3f)",
						step+1, currentPhase, currentConc, prevSim))
				}
			}

			// Retrieve candidates with PhaseDial sweep
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

					// Compute adjusted similarity with bridging logic
					adjSim := s

					spanFuncName := extractFuncName(idx)

					// Name lock: after step 0, penalize different function names
					if step > 0 && firstName != "" && spanFuncName != firstName {
						adjSim *= 0.5
					}

					// Bridge mode adjustments
					if inBridge {
						// Suppress spans starting with "def "
						if strings.HasPrefix(spanText, "def ") {
							adjSim -= bridgePenalty
						}

						// Boost spans in body hemisphere
						if spanIndex[idx].eigenPhase > bodyPhaseThreshold {
							adjSim += bodyBoost * spanIndex[idx].concentration
						}

						// Penalize spans deep in header hemisphere
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
						spanLen: len(spanIndex[idx].tokens),
						phase:   spanIndex[idx].eigenPhase,
						conc:    spanIndex[idx].concentration,
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

			// ── Progress-aware span acceptance ──
			// Walk the ranked candidate list; accept the first that makes
			// forward structural progress. Progress is measured by three signals:
			//   1. Δphase:     angular movement from current eigenphase
			//   2. new_ratio:  fraction of span that is genuinely new content
			//   3. Δsim:       similarity improvement (negative = declining, which is OK
			//                  if phase moves, meaning we're transitioning manifolds)
			//
			// A span stagnates when it has high overlap, no phase movement,
			// and no similarity improvement — i.e. it's an echo of what we already have.

			const (
				progressEps = 0.05 // minimum progress score to accept
				wPhase      = 2.0  // weight for phase movement
				wNewRatio   = 1.5  // weight for new-token ratio
				wSimDelta   = 0.5  // weight for similarity delta
			)

			accepted := false
			for ci, cand := range candidates {
				spanText := detokenize(spanIndex[cand.idx].tokens)
				ovl := cand.ovl
				newText := spanText[ovl:]
				srcFn := extractFuncName(cand.idx)

				// Compute progress signals
				phaseDiff := cand.phase - prevPhase
				angularDist := math.Abs(math.Atan2(math.Sin(phaseDiff), math.Cos(phaseDiff)))
				newRatio := float64(cand.newToks) / float64(cand.spanLen)

				// Novelty check: if the new text already appears in the output,
				// it's not actually novel — it's an echo. Zero the ratio.
				if len(newText) > 0 && strings.Contains(currentOutput, newText) {
					newRatio = 0
				}

				simDelta := cand.rawSim - prevSim
				if simDelta < 0 {
					simDelta = 0 // don't penalize sim decline during bridging
				}

				progress := wPhase*angularDist + wNewRatio*newRatio + wSimDelta*simDelta

				// Step 0 always accepts (we need to start somewhere)
				if step > 0 && progress < progressEps {
					continue // stagnating — try next candidate
				}

				// Accept this candidate
				currentOutput += newText
				prevSim = cand.rawSim
				prevPhase = cand.phase

				bridgeLabel := ""
				if inBridge {
					bridgeLabel = " [BRIDGE]"
				}
				skipLabel := ""
				if ci > 0 {
					skipLabel = fmt.Sprintf(" (skipped %d)", ci)
				}

				console.Info(fmt.Sprintf("  │  Step %d (sim=%.4f, adj=%.4f, ovl=%d, new=%d, φ=%.2f, R=%.2f, prog=%.3f): +[%s] ← %s%s%s",
					step+1, cand.rawSim, cand.adjSim, ovl, cand.newToks,
					cand.phase, cand.conc, progress, newText, srcFn, bridgeLabel, skipLabel))

				chain = append(chain, PhaseBridgingStep{
					StepNum:       step + 1,
					SimScore:      cand.rawSim,
					SpanText:      newText,
					NewTokens:     cand.newToks,
					Overlap:       ovl,
					SourceFunc:    srcFn,
					EigenPhase:    cand.phase,
					Concentration: cand.conc,
					InBridge:      inBridge,
				})
				accepted = true
				break
			}

			if !accepted {
				console.Info(fmt.Sprintf("  │  Step %d: no progressing candidate found (checked %d)", step+1, len(candidates)))
				break
			}
		}

		// Tokenize final output
		finalTokens := tokenize(currentOutput)
		hasReturn := strings.Contains(currentOutput, "return")
		hasLoop := strings.Contains(currentOutput, "for ") || strings.Contains(currentOutput, "while ")

		if hasReturn {
			totalReturnCount++
		}
		if hasLoop {
			totalLoopCount++
		}
		totalBridgeCount += bridgeCount

		// Truncate for display
		displayText := currentOutput
		if len(displayText) > 120 {
			displayText = displayText[:120] + "…"
		}
		console.Info(fmt.Sprintf("  │  Full (%d tokens): %s", len(finalTokens), displayText))
		console.Info(fmt.Sprintf("  └─ Steps: %d, tokens: %d, return: %v, loop: %v, bridges: %d",
			len(chain), len(finalTokens), hasReturn, hasLoop, bridgeCount))

		entries = append(entries, PhaseBridgingEntry{
			Prefix:      prompt.prefix,
			Desc:        prompt.desc,
			FullText:    currentOutput,
			ChainLength: len(chain),
			TotalTokens: len(finalTokens),
			HasReturn:   hasReturn,
			HasLoop:     hasLoop,
			BridgeCount: bridgeCount,
			Chain:       chain,
		})
	}

	// Summary
	n := float64(len(entries))
	var sumTokens float64
	for _, e := range entries {
		sumTokens += float64(e.TotalTokens)
	}

	console.Info("\n  ── Summary ──")
	console.Info(fmt.Sprintf("  Has return: %d/%d", totalReturnCount, len(entries)))
	console.Info(fmt.Sprintf("  Has loop: %d/%d", totalLoopCount, len(entries)))
	console.Info(fmt.Sprintf("  Bridge events: %d total", totalBridgeCount))
	console.Info(fmt.Sprintf("  Mean total tokens: %.1f", sumTokens/n))

	return PhaseBridgingResult{
		Entries:     entries,
		MeanTokens:  sumTokens / n,
		ReturnCount: totalReturnCount,
		LoopCount:   totalLoopCount,
		BridgeTotal: totalBridgeCount,
	}
}
