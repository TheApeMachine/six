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

// testSpanRanking implements Test 2: Span Ranking BVP.
//
// Instead of per-position token voting, this retrieves whole candidate spans
// and scores them as complete units against the boundary conditions.
// The best-scoring span is selected directly — no decomposition, no voting.
//
// Score(span) = sim(span_fingerprint, boundary_fingerprint)
//   - prefix_overlap bonus
//   - structural bonus (indentation, return, colon)
func (experiment *Experiment) testSpanRanking(corpus []string) SpanRankingResult {
	D := numeric.NBasis

	// Build span memory at multiple lengths
	spanLengths := numeric.FibWindows
	const topK = 64
	const nDial = 8

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

	// Test prompts — same as Test 1 for direct comparison
	type testPrompt struct {
		prefix string
		suffix string
		desc   string
	}
	prompts := []testPrompt{
		{"def factorial(n):", "", "Factorial — arithmetic recursion"},
		{"def find_max(lst):", "", "Find max — list iteration"},
		{"def is_palindrome(s):", "", "Palindrome check — string operation"},
		{"def binary_search(lst, target):", "", "Binary search — algorithm"},
		{"def filter_list(fn, lst):", "", "Filter — higher-order function"},
	}

	// Span scoring function
	type scoredSpan struct {
		idx         int
		simScore    float64 // fingerprint similarity to boundary
		prefixOvl   float64 // prefix token overlap bonus
		structBonus float64 // structural quality bonus
		total       float64
	}

	scoreSpan := func(spanIdx int, fpBoundary geometry.PhaseDial, prefixTokens []string) scoredSpan {
		meta := spanIndex[spanIdx]
		entry := substrate.Entries[spanIdx]

		// 1. Fingerprint similarity (primary signal)
		simScore := sim(entry.Fingerprint, fpBoundary)

		// 2. Prefix token overlap — do any prefix tokens appear in the span?
		prefixOvl := 0.0
		for _, pt := range prefixTokens {
			for _, st := range meta.tokens {
				if pt == st && len(pt) > 1 { // skip single-char noise
					prefixOvl += 0.02
				}
			}
		}
		// Cap at 0.1
		if prefixOvl > 0.1 {
			prefixOvl = 0.1
		}

		// 3. Structural bonus — reward code-like patterns
		structBonus := 0.0
		text := meta.text
		if strings.Contains(text, "return") {
			structBonus += 0.01
		}
		if strings.Contains(text, ":") {
			structBonus += 0.005
		}
		// Reward indentation (code body indicators)
		if strings.Contains(text, "    ") {
			structBonus += 0.005
		}

		total := simScore + prefixOvl + structBonus

		return scoredSpan{
			idx:         spanIdx,
			simScore:    simScore,
			prefixOvl:   prefixOvl,
			structBonus: structBonus,
			total:       total,
		}
	}

	// Solve each prompt
	var results []SpanRankingEntry

	for _, p := range prompts {
		console.Info(fmt.Sprintf("\n  ┌─ %s", p.desc))
		console.Info(fmt.Sprintf("  │  Prefix: %s", p.prefix))

		// Build boundary fingerprint
		fpPrefix := geometry.NewPhaseDial().Encode(p.prefix)
		fpBoundary := make(geometry.PhaseDial, D)
		copy(fpBoundary, fpPrefix)

		if p.suffix != "" {
			fpSuffix := geometry.NewPhaseDial().Encode(p.suffix)
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

		prefixTokens := tokenize(p.prefix)

		// Retrieve diverse candidates via PhaseDial sweep
		seen := make(map[int]bool)
		var allCandidates []int

		for d := 0; d < nDial; d++ {
			alpha := float64(d) * (2.0 * math.Pi / float64(nDial))
			rotated := make(geometry.PhaseDial, D)
			if d == 0 {
				copy(rotated, fpBoundary)
			} else {
				f1 := cmplx.Rect(1.0, alpha)
				f2 := cmplx.Rect(1.0, -alpha*0.5)
				for k := 0; k < D; k++ {
					if k < D/2 {
						rotated[k] = fpBoundary[k] * f1
					} else {
						rotated[k] = fpBoundary[k] * f2
					}
				}
			}

			ranked := substrate.PhaseDialRank(candidates, rotated)
			perAngle := topK / nDial
			if perAngle < 4 {
				perAngle = 4
			}
			added := 0
			for _, r := range ranked {
				if !seen[r.Idx] {
					seen[r.Idx] = true
					allCandidates = append(allCandidates, r.Idx)
					added++
				}
				if added >= perAngle {
					break
				}
			}
		}

		// Score all candidates as whole spans
		scored := make([]scoredSpan, len(allCandidates))
		for i, idx := range allCandidates {
			scored[i] = scoreSpan(idx, fpBoundary, prefixTokens)
		}

		// Sort by total score descending
		for i := 0; i < len(scored); i++ {
			for j := i + 1; j < len(scored); j++ {
				if scored[j].total > scored[i].total {
					scored[i], scored[j] = scored[j], scored[i]
				}
			}
		}

		// Report top-20
		showN := 20
		if len(scored) < showN {
			showN = len(scored)
		}

		var topEntries []SpanCandidate
		for i := 0; i < showN; i++ {
			s := scored[i]
			meta := spanIndex[s.idx]
			marker := "  "
			if i == 0 {
				marker = "★ "
			}
			console.Info(fmt.Sprintf("  │  %s#%d (sim=%.4f ovl=%.3f str=%.3f total=%.4f) [%d tok]: %s",
				marker, i+1, s.simScore, s.prefixOvl, s.structBonus, s.total, meta.length, meta.text))

			topEntries = append(topEntries, SpanCandidate{
				Rank:        i + 1,
				Text:        meta.text,
				Length:      meta.length,
				SimScore:    s.simScore,
				PrefixOvl:   s.prefixOvl,
				StructBonus: s.structBonus,
				Total:       s.total,
				SourceIdx:   meta.source,
			})
		}

		// The winner
		winner := scored[0]
		winnerMeta := spanIndex[winner.idx]
		console.Info(fmt.Sprintf("  └─ Winner: %s", winnerMeta.text))

		// Quality assessment of the winner
		winText := winnerMeta.text
		hasReturn := strings.Contains(winText, "return")
		hasColon := strings.Contains(winText, ":")
		hasIndent := strings.Contains(winText, "    ")

		// Check if winner contains tokens from the prefix (identifier reuse)
		identReuse := 0
		for _, pt := range prefixTokens {
			if len(pt) > 2 { // skip short tokens like (, ), :
				for _, wt := range winnerMeta.tokens {
					if strings.Contains(wt, pt) || strings.Contains(pt, wt) {
						identReuse++
						break
					}
				}
			}
		}

		// Check if it's an exact corpus match (span exists in corpus)
		exactMatch := false
		for _, fn := range corpus {
			if strings.Contains(fn, winText) {
				exactMatch = true
				break
			}
		}

		entry := SpanRankingEntry{
			Desc:           p.desc,
			Prefix:         p.prefix,
			WinnerText:     winText,
			WinnerLength:   winnerMeta.length,
			WinnerSim:      winner.simScore,
			WinnerTotal:    winner.total,
			HasReturn:      hasReturn,
			HasColon:       hasColon,
			HasIndent:      hasIndent,
			IdentReuse:     identReuse,
			ExactCorpus:    exactMatch,
			TopCandidates:  topEntries,
			TotalRetrieved: len(allCandidates),
		}
		results = append(results, entry)
	}

	// Summary stats
	nPrompts := len(prompts)
	exactCount := 0
	returnCount := 0
	colonCount := 0
	indentCount := 0
	sumSim := 0.0
	for _, e := range results {
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

	console.Info("\n  ── Summary ──")
	console.Info(fmt.Sprintf("  Exact corpus match: %d/%d", exactCount, nPrompts))
	console.Info(fmt.Sprintf("  Has return: %d/%d", returnCount, nPrompts))
	console.Info(fmt.Sprintf("  Has colon: %d/%d", colonCount, nPrompts))
	console.Info(fmt.Sprintf("  Has indentation: %d/%d", indentCount, nPrompts))
	console.Info(fmt.Sprintf("  Mean winner similarity: %.4f", sumSim/float64(nPrompts)))

	return SpanRankingResult{
		TotalSpans:    len(spanIndex),
		SpanLengths:   spanLengths,
		DialAngles:    nDial,
		TopK:          topK,
		Entries:       results,
		ExactCount:    exactCount,
		ReturnCount:   returnCount,
		ColonCount:    colonCount,
		IndentCount:   indentCount,
		MeanWinnerSim: sumSim / float64(nPrompts),
	}
}
