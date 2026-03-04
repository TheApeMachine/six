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

// testSpanSolver implements Test 1: Core BVP Span Solver.
//
// Algorithm:
//  1. Build a span memory: tokenize each corpus function, extract all
//     contiguous spans of length L, store each span's fingerprint (computed
//     from its text) alongside the span tokens as readout.
//  2. For each test prompt (function signature), build the boundary fingerprint:
//     F_boundary = Encode(prefix) [+ Encode(suffix) if present]
//  3. Retrieve top-K candidate spans by PhaseDial similarity.
//  4. For each token position in the output span, collect candidate tokens
//     weighted by their source span's similarity score. Select by weighted vote.
//  5. Refine: re-encode prefix+candidate_span, re-retrieve, re-vote.
//     Repeat for N iterations or until stable.
//  6. Evaluate: syntax validity, identifier reuse, indentation correctness.
func (experiment *Experiment) testSpanSolver(corpus []string) SpanSolverResult {
	D := numeric.NBasis

	// ── Step 1: Build span memory ──
	spanLengths := numeric.FibWindows // {3, 5, 8, 13, 21}
	const outLen = 8                  // output tokens for per-position voting (middle FibWindow)
	const topK = 16                   // candidate spans to retrieve
	const nRefine = 3                 // BVP refinement iterations
	const nDial = 6                   // PhaseDial sweep angles for diversity

	substrate := geometry.NewHybridSubstrate()
	var universalFilter data.Chord

	type spanMeta struct {
		tokens []string
		source int // corpus index
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
				spanIndex = append(spanIndex, spanMeta{tokens: span, source: corpIdx})
				totalSpans++
			}
		}
	}

	console.Info(fmt.Sprintf("  Span memory: %d spans across lengths %v", totalSpans, spanLengths))

	candidates := make([]int, len(substrate.Entries))
	for i := range candidates {
		candidates[i] = i
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

	// ── Step 2: Define test prompts ──
	type testPrompt struct {
		prefix  string
		suffix  string // optional boundary constraint
		spanLen int    // number of output tokens
		desc    string
	}

	prompts := []testPrompt{
		{
			prefix:  "def factorial(n):",
			suffix:  "",
			spanLen: outLen,
			desc:    "Factorial — arithmetic recursion",
		},
		{
			prefix:  "def find_max(lst):",
			suffix:  "",
			spanLen: outLen,
			desc:    "Find max — list iteration",
		},
		{
			prefix:  "def is_palindrome(s):",
			suffix:  "",
			spanLen: outLen,
			desc:    "Palindrome check — string operation",
		},
		{
			prefix:  "def binary_search(lst, target):",
			suffix:  "",
			spanLen: outLen,
			desc:    "Binary search — algorithm",
		},
		{
			prefix:  "def filter_list(fn, lst):",
			suffix:  "",
			spanLen: outLen,
			desc:    "Filter — higher-order function",
		},
	}

	// ── BVP Solver ──
	type solverResult struct {
		prompt          testPrompt
		generatedTokens []string
		generatedText   string
		iterations      int
		converged       bool
		topCandidates   []string  // top-5 retrieved span texts
		topScores       []float64 // their similarity scores
		// Quality metrics
		hasIndent        bool    // contains indentation
		hasReturn        bool    // contains return statement
		hasColon         bool    // contains colon (syntax marker)
		uniqueTokenRatio float64 // unique/total tokens (anti-repetition)
		prefixRelevance  float64 // similarity of output to prefix fingerprint
	}

	solve := func(prompt testPrompt) solverResult {
		// Build boundary fingerprint
		fpPrefix := geometry.NewPhaseDial().Encode(prompt.prefix)
		fpBoundary := make(geometry.PhaseDial, D)
		copy(fpBoundary, fpPrefix)

		if prompt.suffix != "" {
			fpSuffix := geometry.NewPhaseDial().Encode(prompt.suffix)
			// Combine: normalized sum
			var norm float64
			for k := 0; k < D; k++ {
				fpBoundary[k] = fpPrefix[k] + fpSuffix[k]
				r, im := real(fpBoundary[k]), imag(fpBoundary[k])
				norm += r*r + im*im
			}
			norm = math.Sqrt(norm)
			if norm > 0 {
				for k := 0; k < D; k++ {
					fpBoundary[k] /= complex(norm, 0)
				}
			}
		}

		// Current best span tokens
		currentTokens := make([]string, prompt.spanLen)
		var converged bool

		for iter := 0; iter < nRefine; iter++ {
			// If not first iteration, re-encode boundary with current span
			queryFP := make(geometry.PhaseDial, D)
			if iter == 0 {
				copy(queryFP, fpBoundary)
			} else {
				combined := prompt.prefix + "\n    " + detokenize(currentTokens)
				if prompt.suffix != "" {
					combined += "\n" + prompt.suffix
				}
				queryFP = geometry.NewPhaseDial().Encode(combined)
			}

			// Retrieve candidates via PhaseDial sweep for diversity
			type scoredSpan struct {
				idx   int
				score float64
			}
			var allCandidates []scoredSpan
			seen := make(map[int]bool)

			for d := 0; d < nDial; d++ {
				alpha := float64(d) * (2.0 * math.Pi / float64(nDial))
				rotated := make(geometry.PhaseDial, D)
				if d == 0 {
					copy(rotated, queryFP)
				} else {
					// Rotate using the 256/256 torus split (our best split)
					f1 := cmplx.Rect(1.0, alpha)
					f2 := cmplx.Rect(1.0, -alpha*0.5) // counter-rotate for diversity
					for k := 0; k < D; k++ {
						if k < D/2 {
							rotated[k] = queryFP[k] * f1
						} else {
							rotated[k] = queryFP[k] * f2
						}
					}
				}

				ranked := substrate.PhaseDialRank(candidates, rotated)
				for _, r := range ranked {
					if !seen[r.Idx] {
						seen[r.Idx] = true
						allCandidates = append(allCandidates, scoredSpan{r.Idx, r.Score})
					}
					if len(allCandidates) >= topK {
						break
					}
				}
				if len(allCandidates) >= topK {
					break
				}
			}

			// Sort by score
			for i := 0; i < len(allCandidates); i++ {
				for j := i + 1; j < len(allCandidates); j++ {
					if allCandidates[j].score > allCandidates[i].score {
						allCandidates[i], allCandidates[j] = allCandidates[j], allCandidates[i]
					}
				}
			}
			if len(allCandidates) > topK {
				allCandidates = allCandidates[:topK]
			}

			// Token voting: for each position, weighted vote across candidates
			newTokens := make([]string, prompt.spanLen)
			for pos := 0; pos < prompt.spanLen; pos++ {
				votes := make(map[string]float64)
				for _, c := range allCandidates {
					span := spanIndex[c.idx]
					if pos < len(span.tokens) {
						tok := span.tokens[pos]
						// Weight by similarity score (shifted to positive)
						weight := c.score + 1.0 // ensure positive
						votes[tok] += weight
					}
				}

				// Select highest-voted token
				bestTok := ""
				bestWeight := -1.0
				for tok, w := range votes {
					if w > bestWeight {
						bestWeight = w
						bestTok = tok
					}
				}
				newTokens[pos] = bestTok
			}

			// Check convergence
			if iter > 0 {
				same := true
				for i := range newTokens {
					if newTokens[i] != currentTokens[i] {
						same = false
						break
					}
				}
				if same {
					converged = true
					currentTokens = newTokens
					break
				}
			}
			currentTokens = newTokens
		}

		// Collect top-5 candidate texts for reporting
		var topTexts []string
		var topScores []float64
		// Re-retrieve to get final candidates
		finalQuery := geometry.NewPhaseDial().Encode(prompt.prefix + "\n    " + detokenize(currentTokens))
		finalRanked := substrate.PhaseDialRank(candidates, finalQuery)
		showN := 5
		if len(finalRanked) < showN {
			showN = len(finalRanked)
		}
		for i := 0; i < showN; i++ {
			topTexts = append(topTexts, string(substrate.Entries[finalRanked[i].Idx].Readout))
			topScores = append(topScores, finalRanked[i].Score)
		}

		generated := detokenize(currentTokens)

		// Quality metrics
		hasIndent := strings.Contains(generated, "   ") || strings.HasPrefix(currentTokens[0], " ")
		hasReturn := strings.Contains(generated, "return")
		hasColon := strings.Contains(generated, ":")

		unique := make(map[string]bool)
		for _, t := range currentTokens {
			if t != "" {
				unique[t] = true
			}
		}
		uniqueRatio := 0.0
		if len(currentTokens) > 0 {
			uniqueRatio = float64(len(unique)) / float64(len(currentTokens))
		}

		prefixRel := sim(fpPrefix, finalQuery)

		return solverResult{
			prompt:           prompt,
			generatedTokens:  currentTokens,
			generatedText:    generated,
			iterations:       nRefine,
			converged:        converged,
			topCandidates:    topTexts,
			topScores:        topScores,
			hasIndent:        hasIndent,
			hasReturn:        hasReturn,
			hasColon:         hasColon,
			uniqueTokenRatio: uniqueRatio,
			prefixRelevance:  prefixRel,
		}
	}

	// Run all prompts
	var results []SpanSolverEntry
	totalHasReturn := 0
	totalHasColon := 0
	totalConverged := 0
	sumUniqueRatio := 0.0
	sumRelevance := 0.0

	for _, p := range prompts {
		console.Info(fmt.Sprintf("\n  ┌─ %s", p.desc))
		console.Info(fmt.Sprintf("  │  Prefix: %s", p.prefix))

		r := solve(p)

		console.Info(fmt.Sprintf("  │  Generated: %s", r.generatedText))
		console.Info(fmt.Sprintf("  │  Converged: %v  Iterations: %d", r.converged, r.iterations))
		console.Info(fmt.Sprintf("  │  Has return: %v  Has colon: %v  Unique ratio: %.2f", r.hasReturn, r.hasColon, r.uniqueTokenRatio))
		console.Info(fmt.Sprintf("  │  Prefix relevance: %.4f", r.prefixRelevance))

		// Show top-3 retrieved spans
		showN := 3
		if len(r.topCandidates) < showN {
			showN = len(r.topCandidates)
		}
		for i := 0; i < showN; i++ {
			console.Info(fmt.Sprintf("  │  Top-%d (%.4f): %s", i+1, r.topScores[i], r.topCandidates[i]))
		}

		if r.hasReturn {
			totalHasReturn++
		}
		if r.hasColon {
			totalHasColon++
		}
		if r.converged {
			totalConverged++
		}
		sumUniqueRatio += r.uniqueTokenRatio
		sumRelevance += r.prefixRelevance

		entry := SpanSolverEntry{
			Desc:            p.desc,
			Prefix:          p.prefix,
			Generated:       r.generatedText,
			Converged:       r.converged,
			Iterations:      r.iterations,
			HasReturn:       r.hasReturn,
			HasColon:        r.hasColon,
			UniqueRatio:     r.uniqueTokenRatio,
			PrefixRelevance: r.prefixRelevance,
			TopSpans:        r.topCandidates,
			TopScores:       r.topScores,
		}
		results = append(results, entry)
	}

	n := float64(len(prompts))
	console.Info("\n  ── Summary ──")
	console.Info(fmt.Sprintf("  Converged: %d/%d", totalConverged, len(prompts)))
	console.Info(fmt.Sprintf("  Has return: %d/%d", totalHasReturn, len(prompts)))
	console.Info(fmt.Sprintf("  Has colon: %d/%d", totalHasColon, len(prompts)))
	console.Info(fmt.Sprintf("  Mean unique ratio: %.3f", sumUniqueRatio/n))
	console.Info(fmt.Sprintf("  Mean prefix relevance: %.4f", sumRelevance/n))

	return SpanSolverResult{
		SpanLength:       outLen,
		TopK:             topK,
		RefineIterations: nRefine,
		DialAngles:       nDial,
		TotalSpans:       totalSpans,
		Entries:          results,
		ConvergedCount:   totalConverged,
		ReturnCount:      totalHasReturn,
		ColonCount:       totalHasColon,
		MeanUniqueRatio:  sumUniqueRatio / n,
		MeanRelevance:    sumRelevance / n,
	}
}
