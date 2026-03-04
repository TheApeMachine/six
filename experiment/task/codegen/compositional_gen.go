package codegen

import (
	"fmt"
	"math"
	"math/cmplx"
	"strings"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/numeric"
)

// testCompositionalGeneration implements Test 6: Out-of-Corpus Compositional Generation.
//
// This test addresses two critical questions:
//
//  1. Can the system generate code for prompts NOT in the corpus?
//     (Separates compositional retrieval from exact lookup.)
//
//  2. Does the fingerprint geometry alone provide sufficient signal,
//     without hand-crafted heuristics (name lock, structural bonuses)?
//
// All heuristics removed:
//   - No name lock (extractFuncName filter)
//   - No structural bonus (return/colon/indent bonuses)
//   - No prefix overlap bonus
//   - Score = pure fingerprint similarity only
//   - Overlap-aware concatenation and min-progress are retained
//     (those are assembly mechanics, not scoring heuristics)
func (experiment *Experiment) testCompositionalGeneration(corpus []string) CompGenResult {
	D := numeric.NBasis

	spanLengths := numeric.FibWindows
	const topK = 64
	const nDial = 8
	const maxChains = 10
	const minNewTokens = 2

	substrate := numeric.NewHybridSubstrate()
	var universalFilter numeric.Chord

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
				fp := numeric.EncodeText(spanText)
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

	sim := func(a, b numeric.PhaseDial) float64 {
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

	// Out-of-corpus prompts — functions NOT in the corpus
	// but whose implementations are composable from existing patterns
	type testPrompt struct {
		prefix   string
		desc     string
		expected string // what a correct implementation would look like
	}
	prompts := []testPrompt{
		{
			"def is_even(n):",
			"Even check — trivial mod (not in corpus)",
			"return n % 2 == 0",
		},
		{
			"def square(x):",
			"Square — trivial arithmetic (not in corpus)",
			"return x * x",
		},
		{
			"def product_list(lst):",
			"Product — structural analog of sum_list (not in corpus)",
			"result = 1; for x in lst: result *= x; return result",
		},
		{
			"def has_duplicates(lst):",
			"Duplicates — structural analog of unique (not in corpus)",
			"seen = set(); for x in lst: if x in seen: return True; seen.add(x); return False",
		},
		{
			"def clamp(x, lo, hi):",
			"Clamp — composable from min_val/max_val (not in corpus)",
			"if x < lo: return lo; if x > hi: return hi; return x",
		},
		{
			"def second_largest(lst):",
			"Second largest — analog of find_max (not in corpus)",
			"similar to find_max with two trackers",
		},
		{
			"def mean(lst):",
			"Mean — composable from sum_list (not in corpus)",
			"return sum_list(lst) / len(lst)",
		},
	}

	var results []CompGenEntry

	for _, p := range prompts {
		console.Info(fmt.Sprintf("\n  ┌─ %s", p.desc))
		console.Info(fmt.Sprintf("  │  Prefix: %s", p.prefix))
		console.Info(fmt.Sprintf("  │  Expected: %s", p.expected))

		outTokens := tokenize(p.prefix)
		usedSpans := make(map[int]bool)
		var chain []CompGenStep
		reachedReturn := false

		for step := 0; step < maxChains; step++ {
			contextWindow := 20
			queryTokens := outTokens
			if len(queryTokens) > contextWindow {
				queryTokens = queryTokens[len(queryTokens)-contextWindow:]
			}
			queryText := detokenize(queryTokens)
			queryFP := numeric.EncodeText(queryText)

			// Retrieve diverse candidates
			seen := make(map[int]bool)
			var candidates []int

			for d := 0; d < nDial; d++ {
				alpha := float64(d) * (2.0 * math.Pi / float64(nDial))
				rotated := make(numeric.PhaseDial, D)
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

			// Score: PURE fingerprint similarity only
			// No name lock, no structural bonus, no prefix overlap
			type scoredCandidate struct {
				idx     int
				score   float64
				overlap int
				newToks int
				meta    spanMeta
			}
			var viable []scoredCandidate

			for _, idx := range candidates {
				meta := spanIndex[idx]
				entry := substrate.Entries[idx]

				ovl := overlapLen(outTokens, meta.tokens)
				newToks := len(meta.tokens) - ovl

				if newToks < minNewTokens {
					continue
				}

				// Pure similarity — no heuristic bonuses
				score := sim(entry.Fingerprint, queryFP)

				viable = append(viable, scoredCandidate{
					idx:     idx,
					score:   score,
					overlap: ovl,
					newToks: newToks,
					meta:    meta,
				})
			}

			if len(viable) == 0 {
				console.Info(fmt.Sprintf("  │  Step %d: no viable candidates, stopping", step+1))
				break
			}

			// Sort by pure similarity
			for i := 0; i < len(viable); i++ {
				for j := i + 1; j < len(viable); j++ {
					if viable[j].score > viable[i].score {
						viable[i], viable[j] = viable[j], viable[i]
					}
				}
			}

			// Log top-3 candidates for diagnostic
			showN := 3
			if len(viable) < showN {
				showN = len(viable)
			}
			for i := 0; i < showN; i++ {
				c := viable[i]
				marker := "   "
				if i == 0 {
					marker = " ★ "
				}
				console.Info(fmt.Sprintf("  │%s  cand %d (sim=%.4f, ovl=%d, new=%d, src=%d): %s",
					marker, i+1, c.score, c.overlap, c.newToks, c.meta.source, c.meta.text))
			}

			best := viable[0]
			usedSpans[best.idx] = true

			newTokens := best.meta.tokens[best.overlap:]
			outTokens = append(outTokens, newTokens...)
			newText := detokenize(newTokens)

			// Which corpus function did this come from?
			sourceFn := ""
			if best.meta.source < len(corpus) {
				lines := strings.SplitN(corpus[best.meta.source], "\n", 2)
				sourceFn = lines[0]
			}

			chain = append(chain, CompGenStep{
				Step:      step + 1,
				SpanText:  best.meta.text,
				NewText:   newText,
				NewTokens: best.newToks,
				Overlap:   best.overlap,
				SimScore:  best.score,
				SourceIdx: best.meta.source,
				SourceFn:  sourceFn,
			})

			console.Info(fmt.Sprintf("  │  Step %d (sim=%.4f, ovl=%d, new=%d): +[%s] ← %s",
				step+1, best.score, best.overlap, best.newToks, newText, sourceFn))

			if strings.Contains(newText, "return") && step > 0 {
				reachedReturn = true
				console.Info("  │  → return found, stopping")
				break
			}
		}

		fullText := detokenize(outTokens)
		totalTokens := len(outTokens)
		console.Info(fmt.Sprintf("  │  Full (%d tokens): %s", totalTokens, fullText))

		// Quality — no heuristic assessment, just structural checks
		hasReturn := strings.Contains(fullText, "return")
		hasLoop := strings.Contains(fullText, "for") || strings.Contains(fullText, "while")
		hasConditional := strings.Contains(fullText, "if")

		// Source diversity
		sources := make(map[int]bool)
		sourceFns := make(map[string]bool)
		for _, c := range chain {
			sources[c.SourceIdx] = true
			sourceFns[c.SourceFn] = true
		}

		totalNew := 0
		for _, c := range chain {
			totalNew += c.NewTokens
		}

		// Does the output contain identifiers from the expected implementation?
		expectedTokens := tokenize(p.expected)
		matchedExpected := 0
		for _, et := range expectedTokens {
			if len(et) > 2 && strings.Contains(fullText, et) {
				matchedExpected++
			}
		}
		expectedOverlap := 0.0
		if len(expectedTokens) > 0 {
			expectedOverlap = float64(matchedExpected) / float64(len(expectedTokens))
		}

		console.Info(fmt.Sprintf("  └─ Steps: %d, tokens: %d, return: %v, loop: %v, sources: %d fns, expected_overlap: %.2f",
			len(chain), totalTokens, hasReturn, hasLoop, len(sourceFns), expectedOverlap))

		entry := CompGenEntry{
			Desc:            p.desc,
			Prefix:          p.prefix,
			Expected:        p.expected,
			FullText:        fullText,
			Chain:           chain,
			ChainLength:     len(chain),
			TotalTokens:     totalTokens,
			TotalNew:        totalNew,
			HasReturn:       hasReturn,
			HasLoop:         hasLoop,
			HasConditional:  hasConditional,
			ReachedReturn:   reachedReturn,
			SourceCount:     len(sources),
			ExpectedOverlap: expectedOverlap,
		}
		results = append(results, entry)
	}

	// Summary
	nPrompts := len(prompts)
	returnCount := 0
	loopCount := 0
	totalTokenSum := 0
	sumOverlap := 0.0
	for _, e := range results {
		if e.HasReturn {
			returnCount++
		}
		if e.HasLoop {
			loopCount++
		}
		totalTokenSum += e.TotalTokens
		sumOverlap += e.ExpectedOverlap
	}

	console.Info("\n  ── Summary ──")
	console.Info(fmt.Sprintf("  Has return: %d/%d", returnCount, nPrompts))
	console.Info(fmt.Sprintf("  Has loop: %d/%d", loopCount, nPrompts))
	console.Info(fmt.Sprintf("  Mean total tokens: %.1f", float64(totalTokenSum)/float64(nPrompts)))
	console.Info(fmt.Sprintf("  Mean expected overlap: %.3f", sumOverlap/float64(nPrompts)))

	return CompGenResult{
		TotalSpans:          len(spanIndex),
		Entries:             results,
		ReturnCount:         returnCount,
		LoopCount:           loopCount,
		MeanTokens:          float64(totalTokenSum) / float64(nPrompts),
		MeanExpectedOverlap: sumOverlap / float64(nPrompts),
	}
}
