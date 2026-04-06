package pattern

import (
	"math"
	"sort"
	"strings"
	"unicode/utf8"
)

/*
Symbol describes a label-skewed repeated sequence fragment.
*/
type Symbol struct {
	Symbol string
	Label  string
	Score  float64
}

/*
NodeVisitor walks a trie and returns sorted child tokens and effective
label counts for a node. This decouples extraction from the trie implementation.
*/
type NodeVisitor struct {
	SortedChildren func(nodeID any) []string
	Child          func(nodeID any, token string) any
	EffectiveCount func(nodeID any, label string) float64
}

/*
Extract walks the trie from root, scores label-skewed repeated fragments,
and returns the top symbols sorted by score.
*/
func Extract(
	root any,
	labels []string,
	visitor NodeVisitor,
	minTotal float64,
	minScore float64,
	limit int,
) []Symbol {
	candidates := make(map[string]map[string]float64)

	var traverse func(node any, path []string)

	traverse = func(node any, path []string) {
		if len(path) > 0 {
			symbol := strings.Join(path, "")

			if candidates[symbol] == nil {
				candidates[symbol] = make(map[string]float64)
			}

			for _, label := range labels {
				candidates[symbol][label] += visitor.EffectiveCount(node, label)
			}
		}

		for _, token := range visitor.SortedChildren(node) {
			child := visitor.Child(node, token)
			childPath := append(append([]string(nil), path...), token)
			traverse(child, childPath)
		}
	}

	for _, token := range visitor.SortedChildren(root) {
		traverse(visitor.Child(root, token), []string{token})
	}

	scored := make([]Symbol, 0, len(candidates))

	for symbol, counts := range candidates {
		total := 0.0

		for _, count := range counts {
			total += count
		}

		if total < minTotal {
			continue
		}

		for _, label := range labels {
			count := counts[label]

			if count == 0 {
				continue
			}

			score := count / total * math.Log1p(count) * math.Sqrt(
				float64(utf8.RuneCountInString(symbol)),
			)

			if score <= minScore {
				continue
			}

			scored = append(scored, Symbol{
				Symbol: symbol,
				Label:  label,
				Score:  score,
			})
		}
	}

	sort.Slice(scored, func(leftIndex int, rightIndex int) bool {
		left := scored[leftIndex]
		right := scored[rightIndex]

		if left.Score == right.Score {
			if left.Symbol == right.Symbol {
				return left.Label < right.Label
			}

			return left.Symbol < right.Symbol
		}

		return left.Score > right.Score
	})

	if len(scored) > limit {
		scored = scored[:limit]
	}

	return scored
}
