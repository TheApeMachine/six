package codegen

import (
	"fmt"
	"math"
	"math/cmplx"
	"strings"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/numeric"
)

// testOverlapChaining implements Test 4: Overlap-Aware Span Chaining.
//
// Fixes applied over Test 3:
//  1. Overlap-aware concatenation: find longest suffix of output that is
//     a prefix of the candidate span, append only the non-overlapping tail.
//  2. Minimum progress: reject candidates that add fewer than minNewTokens.
//  3. Name lock: after step 1, reject candidates that start a different
//     function definition (def OTHER_NAME).
func (experiment *Experiment) testOverlapChaining(corpus []string) OverlapChainingResult {
	D := numeric.NBasis

	spanLengths := numeric.FibWindows
	const topK = 64
	const nDial = 8
	const maxChains = 6
	const minNewTokens = 2

	substrate := geometry.NewHybridSubstrate()
	var universalFilter data.Chord

	type spanMeta struct {
		tokens []string
		text   string
		source int
		length int
	}
	var spanIndex []spanMeta

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
				substrate.Add(universalFilter, fp, []byte(spanText))
				spanIndex = append(spanIndex, spanMeta{
					tokens: span,
					text:   spanText,
					source: corpIdx,
					length: sLen,
				})
			}
		}
	}

	console.Info(fmt.Sprintf("  Span memory: %d spans (lengths %v)", len(spanIndex), spanLengths))

	allIndices := make([]int, len(substrate.Entries))
	for i := range allIndices {
		allIndices[i] = i
	}

	sim := func(a, b geometry.PhaseDial) float64 {
		var dot complex128
		var na, nb float64
		for i := range a {
			dot += cmplx.Conj(a[i]) * b[i]
			na += real(a[i])*real(a[i]) + imag(a[i])*imag(a[i])
			nb += real(b[i])*real(b[i]) + imag(b[i])*imag(b[i])
		}
		if na == 0 || nb == 0 {
			return 0
		}
		return real(dot) / (math.Sqrt(na) * math.Sqrt(nb))
	}

	// overlapLen finds the longest suffix of 'out' tokens that is a prefix
	// of 'span' tokens, returning the number of overlapping tokens.
	overlapLen := func(outTokens, spanTokens []string) int {
		maxOvl := len(outTokens)
		if len(spanTokens) < maxOvl {
			maxOvl = len(spanTokens)
		}
		for ovl := maxOvl; ovl > 0; ovl-- {
			match := true
			for i := 0; i < ovl; i++ {
				if outTokens[len(outTokens)-ovl+i] != spanTokens[i] {
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

	// extractFuncName parses "def NAME(" from a string.
	extractFuncName := func(text string) string {
		idx := strings.Index(text, "def ")
		if idx < 0 {
			return ""
		}
		rest := text[idx+4:]
		paren := strings.Index(rest, "(")
		if paren < 0 {
			return ""
		}
		return strings.TrimSpace(rest[:paren])
	}

	type testPrompt struct {
		prefix string
		desc   string
	}
	prompts := []testPrompt{
		{"def factorial(n):", "Factorial — arithmetic recursion"},
		{"def find_max(lst):", "Find max — list iteration"},
		{"def is_palindrome(s):", "Palindrome check — string operation"},
		{"def binary_search(lst, target):", "Binary search — algorithm"},
		{"def filter_list(fn, lst):", "Filter — higher-order function"},
	}

	var results []OverlapChainingEntry

	for _, p := range prompts {
		console.Info(fmt.Sprintf("\n  ┌─ %s", p.desc))
		console.Info(fmt.Sprintf("  │  Prefix: %s", p.prefix))

		// The accumulated output tokens
		outTokens := tokenize(p.prefix)
		lockedName := extractFuncName(p.prefix) // Fix 3: name lock
		usedSpans := make(map[int]bool)
		var chain []OverlapChainStep

		for step := 0; step < maxChains; step++ {
			// Build query from the tail of accumulated output (context window)
			contextWindow := 16
			queryTokens := outTokens
			if len(queryTokens) > contextWindow {
				queryTokens = queryTokens[len(queryTokens)-contextWindow:]
			}
			queryText := detokenize(queryTokens)
			queryFP := geometry.NewPhaseDial().Encode(queryText)

			// Retrieve diverse candidates
			seen := make(map[int]bool)
			var candidates []int

			for d := 0; d < nDial; d++ {
				alpha := float64(d) * (2.0 * math.Pi / float64(nDial))
				rotated := make(geometry.PhaseDial, D)
				if d == 0 {
					copy(rotated, queryFP)
				} else {
					f1 := cmplx.Rect(1.0, alpha)
					f2 := cmplx.Rect(1.0, -alpha*0.5)
					for k := 0; k < D; k++ {
						if k < D/2 {
							rotated[k] = queryFP[k] * f1
						} else {
							rotated[k] = queryFP[k] * f2
						}
					}
				}

				ranked := substrate.PhaseDialRank(allIndices, rotated)
				perAngle := topK / nDial
				if perAngle < 4 {
					perAngle = 4
				}
				added := 0
				for _, r := range ranked {
					if !seen[r.Idx] && !usedSpans[r.Idx] {
						seen[r.Idx] = true
						candidates = append(candidates, r.Idx)
						added++
					}
					if added >= perAngle {
						break
					}
				}
			}

			// Score and filter candidates
			type scoredCandidate struct {
				idx      int
				score    float64
				overlap  int
				newToks  int
				spanMeta spanMeta
			}
			var viable []scoredCandidate

			for _, idx := range candidates {
				meta := spanIndex[idx]
				entry := substrate.Entries[idx]

				// Fix 3: name lock — reject spans that define a different function
				if step > 0 && lockedName != "" {
					spanFuncName := extractFuncName(meta.text)
					if spanFuncName != "" && spanFuncName != lockedName {
						continue // different function definition, skip
					}
				}

				// Fix 1: compute overlap
				ovl := overlapLen(outTokens, meta.tokens)
				newToks := len(meta.tokens) - ovl

				// Fix 2: minimum progress
				if newToks < minNewTokens {
					continue
				}

				// Score: fingerprint similarity + structural bonus
				score := sim(entry.Fingerprint, queryFP)

				// Bonus for longer new content
				score += float64(newToks) * 0.005

				// Structural bonus
				newText := detokenize(meta.tokens[ovl:])
				if strings.Contains(newText, "return") {
					score += 0.01
				}
				if strings.Contains(newText, ":") {
					score += 0.005
				}

				viable = append(viable, scoredCandidate{
					idx:      idx,
					score:    score,
					overlap:  ovl,
					newToks:  newToks,
					spanMeta: meta,
				})
			}

			if len(viable) == 0 {
				console.Info(fmt.Sprintf("  │  Step %d: no viable candidates, stopping", step+1))
				break
			}

			// Sort by score descending
			for i := 0; i < len(viable); i++ {
				for j := i + 1; j < len(viable); j++ {
					if viable[j].score > viable[i].score {
						viable[i], viable[j] = viable[j], viable[i]
					}
				}
			}

			best := viable[0]
			usedSpans[best.idx] = true

			// Append only the non-overlapping suffix
			newTokens := best.spanMeta.tokens[best.overlap:]
			outTokens = append(outTokens, newTokens...)

			newText := detokenize(newTokens)

			// Check exact match
			exactMatch := false
			for _, fn := range corpus {
				if strings.Contains(fn, best.spanMeta.text) {
					exactMatch = true
					break
				}
			}

			chainStep := OverlapChainStep{
				Step:       step + 1,
				SpanText:   best.spanMeta.text,
				NewText:    newText,
				NewTokens:  best.newToks,
				Overlap:    best.overlap,
				SimScore:   best.score,
				SourceIdx:  best.spanMeta.source,
				ExactMatch: exactMatch,
			}
			chain = append(chain, chainStep)

			console.Info(fmt.Sprintf("  │  Step %d (sim=%.4f, ovl=%d, new=%d, src=%d): +[%s]",
				step+1, best.score, best.overlap, best.newToks, best.spanMeta.source, newText))

			// Stop if we've hit a return statement in the new tokens
			if strings.Contains(newText, "return") && step > 0 {
				console.Info("  │  → return found, stopping")
				break
			}
		}

		fullText := detokenize(outTokens)
		console.Info(fmt.Sprintf("  │  Full: %s", fullText))

		// Quality metrics
		hasReturn := strings.Contains(fullText, "return")
		colonCount := strings.Count(fullText, ":")
		hasLoop := strings.Contains(fullText, "for") || strings.Contains(fullText, "while")
		looksValid := strings.HasPrefix(fullText, "def ") && hasReturn

		// Source coherence
		sources := make(map[int]bool)
		for _, c := range chain {
			sources[c.SourceIdx] = true
		}
		singleSource := len(sources) == 1

		// Total new tokens generated
		totalNew := 0
		for _, c := range chain {
			totalNew += c.NewTokens
		}

		console.Info(fmt.Sprintf("  └─ Steps: %d, new_tokens: %d, return: %v, loop: %v, valid: %v, single-source: %v",
			len(chain), totalNew, hasReturn, hasLoop, looksValid, singleSource))

		entry := OverlapChainingEntry{
			Desc:         p.desc,
			Prefix:       p.prefix,
			FullText:     fullText,
			Chain:        chain,
			ChainLength:  len(chain),
			TotalNew:     totalNew,
			HasReturn:    hasReturn,
			HasColon:     colonCount > 1,
			HasLoop:      hasLoop,
			LooksValid:   looksValid,
			SingleSource: singleSource,
			SourceCount:  len(sources),
		}
		results = append(results, entry)
	}

	// Summary
	nPrompts := len(prompts)
	validCount := 0
	returnCount := 0
	loopCount := 0
	singleSrcCount := 0
	totalNewSum := 0
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
		if e.SingleSource {
			singleSrcCount++
		}
		totalNewSum += e.TotalNew
	}

	console.Info("\n  ── Summary ──")
	console.Info(fmt.Sprintf("  Valid-looking: %d/%d", validCount, nPrompts))
	console.Info(fmt.Sprintf("  Has return: %d/%d", returnCount, nPrompts))
	console.Info(fmt.Sprintf("  Has loop: %d/%d", loopCount, nPrompts))
	console.Info(fmt.Sprintf("  Single-source: %d/%d", singleSrcCount, nPrompts))
	console.Info(fmt.Sprintf("  Mean new tokens per prompt: %.1f", float64(totalNewSum)/float64(nPrompts)))

	return OverlapChainingResult{
		TotalSpans:     len(spanIndex),
		MaxChains:      maxChains,
		MinNewTokens:   minNewTokens,
		Entries:        results,
		ValidCount:     validCount,
		ReturnCount:    returnCount,
		LoopCount:      loopCount,
		SingleSrcCount: singleSrcCount,
		MeanNewTokens:  float64(totalNewSum) / float64(nPrompts),
	}
}
