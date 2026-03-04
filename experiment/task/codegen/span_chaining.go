package codegen

import (
	"fmt"
	"math"
	"math/cmplx"
	"strings"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/numeric"
)

// testSpanChaining implements Test 3: Span Chaining.
//
// Given a prefix, iteratively retrieve and emit the best whole span,
// then use the emitted span as the new query boundary to retrieve the next.
// This produces multi-span generation: prefix → span₁ → span₂ → span₃ → …
//
// The key question: does the system naturally select spans that compose
// correctly across boundaries?
func (experiment *Experiment) testSpanChaining(corpus []string) SpanChainingResult {
	D := numeric.NBasis

	spanLengths := numeric.FibWindows
	const topK = 32
	const nDial = 6
	const maxChains = 4 // emit up to 4 spans per prompt

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

	// Retrieve + score a single span given a query fingerprint
	retrieveBest := func(queryFP numeric.PhaseDial, prefixTokens []string, usedSpans map[int]bool) (int, float64) {
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

		// Score candidates
		bestIdx := -1
		bestScore := -1.0
		for _, idx := range candidates {
			meta := spanIndex[idx]
			entry := substrate.Entries[idx]

			score := sim(entry.Fingerprint, queryFP)

			// Prefix overlap bonus
			ovl := 0.0
			for _, pt := range prefixTokens {
				for _, st := range meta.tokens {
					if pt == st && len(pt) > 1 {
						ovl += 0.02
					}
				}
			}
			if ovl > 0.1 {
				ovl = 0.1
			}
			score += ovl

			// Structural bonus
			if strings.Contains(meta.text, "return") {
				score += 0.01
			}
			if strings.Contains(meta.text, ":") {
				score += 0.005
			}

			if score > bestScore {
				bestScore = score
				bestIdx = idx
			}
		}

		return bestIdx, bestScore
	}

	// Test prompts
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

	var results []SpanChainingEntry

	for _, p := range prompts {
		console.Info(fmt.Sprintf("\n  ┌─ %s", p.desc))
		console.Info(fmt.Sprintf("  │  Prefix: %s", p.prefix))

		usedSpans := make(map[int]bool)
		currentContext := p.prefix
		var chain []ChainedSpan
		allText := p.prefix

		for step := 0; step < maxChains; step++ {
			// Build query from accumulated context
			queryFP := numeric.EncodeText(currentContext)
			contextTokens := tokenize(currentContext)

			bestIdx, bestScore := retrieveBest(queryFP, contextTokens, usedSpans)
			if bestIdx < 0 {
				break
			}

			meta := spanIndex[bestIdx]
			usedSpans[bestIdx] = true

			// Check if this span is an exact substring of the corpus
			exactMatch := false
			for _, fn := range corpus {
				if strings.Contains(fn, meta.text) {
					exactMatch = true
					break
				}
			}

			// Continuity check: does the last token of the current context
			// overlap with the first token of the new span?
			prevTokens := tokenize(currentContext)
			continuity := false
			if len(prevTokens) > 0 && len(meta.tokens) > 0 {
				lastTok := prevTokens[len(prevTokens)-1]
				firstTok := meta.tokens[0]
				// Overlap if they share any substring > 1 char
				if lastTok == firstTok || strings.HasSuffix(currentContext, meta.tokens[0]) {
					continuity = true
				}
			}

			chain = append(chain, ChainedSpan{
				Step:       step + 1,
				Text:       meta.text,
				Length:     meta.length,
				SimScore:   bestScore,
				SourceIdx:  meta.source,
				ExactMatch: exactMatch,
				Continuity: continuity,
			})

			marker := fmt.Sprintf("  │  Step %d", step+1)
			console.Info(fmt.Sprintf("%s (sim=%.4f, %d tok, exact=%v, cont=%v): %s",
				marker, bestScore, meta.length, exactMatch, continuity, meta.text))

			// Extend context: use the tail of previous + new span
			// to avoid the query growing too long and losing specificity
			allText += " " + meta.text
			// Use last 2 spans worth of tokens as context for next retrieval
			allTokens := tokenize(allText)
			contextWindow := 20
			if len(allTokens) > contextWindow {
				currentContext = detokenize(allTokens[len(allTokens)-contextWindow:])
			} else {
				currentContext = allText
			}
		}

		// Assemble full generated text
		fullText := allText
		console.Info(fmt.Sprintf("  │  Full: %s", fullText))

		// Quality assessment
		hasReturn := strings.Contains(fullText, "return")
		hasColon := strings.Count(fullText, ":") > 1
		hasLoop := strings.Contains(fullText, "for") || strings.Contains(fullText, "while")

		// Check if the full text is a valid-looking function
		// (starts with def, has return, has indentation-like structure)
		looksValid := strings.HasPrefix(fullText, "def ") && hasReturn

		// Count unique source functions used
		sources := make(map[int]bool)
		for _, c := range chain {
			sources[c.SourceIdx] = true
		}

		// Check if all spans come from the same source function
		singleSource := len(sources) == 1

		console.Info(fmt.Sprintf("  └─ Steps: %d, return: %v, loop: %v, valid: %v, single-source: %v, sources: %d",
			len(chain), hasReturn, hasLoop, looksValid, singleSource, len(sources)))

		entry := SpanChainingEntry{
			Desc:         p.desc,
			Prefix:       p.prefix,
			FullText:     fullText,
			Chain:        chain,
			ChainLength:  len(chain),
			HasReturn:    hasReturn,
			HasColon:     hasColon,
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
	}

	console.Info("\n  ── Summary ──")
	console.Info(fmt.Sprintf("  Valid-looking: %d/%d", validCount, nPrompts))
	console.Info(fmt.Sprintf("  Has return: %d/%d", returnCount, nPrompts))
	console.Info(fmt.Sprintf("  Has loop: %d/%d", loopCount, nPrompts))
	console.Info(fmt.Sprintf("  Single-source: %d/%d", singleSrcCount, nPrompts))

	return SpanChainingResult{
		TotalSpans:     len(spanIndex),
		MaxChains:      maxChains,
		Entries:        results,
		ValidCount:     validCount,
		ReturnCount:    returnCount,
		LoopCount:      loopCount,
		SingleSrcCount: singleSrcCount,
	}
}
