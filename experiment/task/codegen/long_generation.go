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

// longCorpus provides additional longer Python functions (80–150 tokens each)
// for testing long-range span chaining stability.
func longCorpus() []string {
	return []string{
		// Quicksort (~90 tokens)
		"def quicksort(lst):\n    if len(lst) <= 1:\n        return lst\n    pivot = lst[len(lst) // 2]\n    left = [x for x in lst if x < pivot]\n    middle = [x for x in lst if x == pivot]\n    right = [x for x in lst if x > pivot]\n    return quicksort(left) + middle + quicksort(right)",

		// Matrix transpose (~80 tokens)
		"def transpose(matrix):\n    if not matrix:\n        return []\n    rows = len(matrix)\n    cols = len(matrix[0])\n    result = []\n    for j in range(cols):\n        row = []\n        for i in range(rows):\n            row.append(matrix[i][j])\n        result.append(row)\n    return result",

		// Two sum (~85 tokens)
		"def two_sum(nums, target):\n    seen = {}\n    for i in range(len(nums)):\n        complement = target - nums[i]\n        if complement in seen:\n            return [seen[complement], i]\n        seen[nums[i]] = i\n    return []",

		// Run-length encoding (~100 tokens)
		"def rle_encode(s):\n    if not s:\n        return ''\n    result = []\n    count = 1\n    for i in range(1, len(s)):\n        if s[i] == s[i - 1]:\n            count += 1\n        else:\n            result.append(s[i - 1] + str(count))\n            count = 1\n    result.append(s[-1] + str(count))\n    return ''.join(result)",

		// Depth-first search (~110 tokens)
		"def dfs(graph, start):\n    visited = set()\n    stack = [start]\n    result = []\n    while stack:\n        node = stack.pop()\n        if node not in visited:\n            visited.add(node)\n            result.append(node)\n            for neighbor in graph.get(node, []):\n                if neighbor not in visited:\n                    stack.append(neighbor)\n    return result",

		// Breadth-first search (~110 tokens)
		"def bfs(graph, start):\n    visited = set()\n    queue = [start]\n    visited.add(start)\n    result = []\n    while queue:\n        node = queue.pop(0)\n        result.append(node)\n        for neighbor in graph.get(node, []):\n            if neighbor not in visited:\n                visited.add(neighbor)\n                queue.append(neighbor)\n    return result",

		// Merge sort full (~120 tokens)
		"def merge_sort(lst):\n    if len(lst) <= 1:\n        return lst\n    mid = len(lst) // 2\n    left = merge_sort(lst[:mid])\n    right = merge_sort(lst[mid:])\n    result = []\n    i = 0\n    j = 0\n    while i < len(left) and j < len(right):\n        if left[i] <= right[j]:\n            result.append(left[i])\n            i += 1\n        else:\n            result.append(right[j])\n            j += 1\n    result.extend(left[i:])\n    result.extend(right[j:])\n    return result",

		// LRU cache simplified (~100 tokens)
		"def group_by(lst, key_fn):\n    groups = {}\n    for item in lst:\n        key = key_fn(item)\n        if key not in groups:\n            groups[key] = []\n        groups[key].append(item)\n    return groups",
	}
}

// testLongGeneration implements Test 5: Long Program Generation.
//
// Uses the same overlap-aware chaining mechanism from Test 4 but with:
//   - Extended corpus including 80–120 token functions
//   - Higher chain limit (12 steps)
//   - Longer span lengths (up to 16 tokens)
//   - Targets: quicksort, merge_sort, dfs, bfs
func (experiment *Experiment) testLongGeneration(corpus []string) LongGenResult {
	D := numeric.NBasis

	// Include longer span lengths for better coverage
	spanLengths := numeric.FibWindows
	const topK = 64
	const nDial = 8
	const maxChains = 12
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

	console.Info(fmt.Sprintf("  Span memory: %d spans (lengths %v, corpus %d)", len(spanIndex), spanLengths, len(corpus)))

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

	// Test prompts — longer functions
	type testPrompt struct {
		prefix string
		desc   string
	}
	prompts := []testPrompt{
		{"def quicksort(lst):", "Quicksort — recursive partition"},
		{"def merge_sort(lst):", "Merge sort — divide and conquer"},
		{"def dfs(graph, start):", "DFS — graph traversal"},
		{"def bfs(graph, start):", "BFS — graph traversal"},
		{"def bubble_sort(lst):", "Bubble sort — nested loop"},
		{"def rle_encode(s):", "RLE — string encoding"},
		{"def two_sum(nums, target):", "Two sum — hash map lookup"},
	}

	var results []LongGenEntry

	for _, p := range prompts {
		console.Info(fmt.Sprintf("\n  ┌─ %s", p.desc))
		console.Info(fmt.Sprintf("  │  Prefix: %s", p.prefix))

		outTokens := tokenize(p.prefix)
		lockedName := extractFuncName(p.prefix)
		usedSpans := make(map[int]bool)
		var chain []LongGenStep
		reachedReturn := false

		for step := 0; step < maxChains; step++ {
			contextWindow := 20
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

			// Score and filter
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

				// Name lock
				if step > 0 && lockedName != "" {
					spanFuncName := extractFuncName(meta.text)
					if spanFuncName != "" && spanFuncName != lockedName {
						continue
					}
				}

				ovl := overlapLen(outTokens, meta.tokens)
				newToks := len(meta.tokens) - ovl

				if newToks < minNewTokens {
					continue
				}

				score := sim(entry.Fingerprint, queryFP)
				score += float64(newToks) * 0.005

				newText := detokenize(meta.tokens[ovl:])
				if strings.Contains(newText, "return") {
					score += 0.01
				}
				if strings.Contains(newText, ":") {
					score += 0.005
				}

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

			// Sort
			for i := 0; i < len(viable); i++ {
				for j := i + 1; j < len(viable); j++ {
					if viable[j].score > viable[i].score {
						viable[i], viable[j] = viable[j], viable[i]
					}
				}
			}

			best := viable[0]
			usedSpans[best.idx] = true

			newTokens := best.meta.tokens[best.overlap:]
			outTokens = append(outTokens, newTokens...)
			newText := detokenize(newTokens)

			chain = append(chain, LongGenStep{
				Step:      step + 1,
				SpanText:  best.meta.text,
				NewText:   newText,
				NewTokens: best.newToks,
				Overlap:   best.overlap,
				SimScore:  best.score,
				SourceIdx: best.meta.source,
			})

			console.Info(fmt.Sprintf("  │  Step %d (sim=%.4f, ovl=%d, new=%d): +[%s]",
				step+1, best.score, best.overlap, best.newToks, newText))

			if strings.Contains(newText, "return") && step > 0 {
				reachedReturn = true
				console.Info("  │  → return found, stopping")
				break
			}
		}

		fullText := detokenize(outTokens)
		totalTokens := len(outTokens)

		console.Info(fmt.Sprintf("  │  Full (%d tokens): %s", totalTokens, fullText))

		// Quality
		hasReturn := strings.Contains(fullText, "return")
		hasLoop := strings.Contains(fullText, "for") || strings.Contains(fullText, "while")
		hasConditional := strings.Contains(fullText, "if")
		looksValid := strings.HasPrefix(fullText, "def ") && hasReturn

		sources := make(map[int]bool)
		for _, c := range chain {
			sources[c.SourceIdx] = true
		}

		totalNew := 0
		for _, c := range chain {
			totalNew += c.NewTokens
		}

		console.Info(fmt.Sprintf("  └─ Steps: %d, tokens: %d, new: %d, return: %v, loop: %v, if: %v, valid: %v, sources: %d",
			len(chain), totalTokens, totalNew, hasReturn, hasLoop, hasConditional, looksValid, len(sources)))

		entry := LongGenEntry{
			Desc:           p.desc,
			Prefix:         p.prefix,
			FullText:       fullText,
			Chain:          chain,
			ChainLength:    len(chain),
			TotalTokens:    totalTokens,
			TotalNew:       totalNew,
			HasReturn:      hasReturn,
			HasLoop:        hasLoop,
			HasConditional: hasConditional,
			LooksValid:     looksValid,
			ReachedReturn:  reachedReturn,
			SourceCount:    len(sources),
		}
		results = append(results, entry)
	}

	// Summary
	nPrompts := len(prompts)
	validCount := 0
	returnCount := 0
	loopCount := 0
	totalTokenSum := 0
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
		totalTokenSum += e.TotalTokens
		totalNewSum += e.TotalNew
	}

	console.Info("\n  ── Summary ──")
	console.Info(fmt.Sprintf("  Valid-looking: %d/%d", validCount, nPrompts))
	console.Info(fmt.Sprintf("  Has return: %d/%d", returnCount, nPrompts))
	console.Info(fmt.Sprintf("  Has loop: %d/%d", loopCount, nPrompts))
	console.Info(fmt.Sprintf("  Mean total tokens: %.1f", float64(totalTokenSum)/float64(nPrompts)))
	console.Info(fmt.Sprintf("  Mean new tokens: %.1f", float64(totalNewSum)/float64(nPrompts)))

	return LongGenResult{
		TotalSpans:    len(spanIndex),
		CorpusSize:    len(corpus),
		MaxChains:     maxChains,
		SpanLengths:   spanLengths,
		Entries:       results,
		ValidCount:    validCount,
		ReturnCount:   returnCount,
		LoopCount:     loopCount,
		MeanTokens:    float64(totalTokenSum) / float64(nPrompts),
		MeanNewTokens: float64(totalNewSum) / float64(nPrompts),
	}
}
