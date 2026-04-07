package beam

import (
	"math"
	"sort"
	"strings"
)

/*
Hypothesis is one beam path: token prefix and cumulative natural log-score.
*/
type Hypothesis struct {
	Tokens []string
	Score  float64
}

/*
Extend branches one hypothesis using ranked masses; keeps up to branchFactor
branches with positive mass. If none qualify, returns a single-copy slice of
the input hypothesis.
*/
func (hypothesis *Hypothesis) Extend(
	ranked RankedTokens, branchFactor int,
) []*Hypothesis {
	if branchFactor <= 0 {
		return []*Hypothesis{hypothesis}
	}

	ranked.SortDescending()

	limit := min(branchFactor, len(ranked))
	out := make([]*Hypothesis, 0, limit*2+1)

	for index := range limit {
		entry := ranked[index]

		if entry.Probability <= 0 {
			continue
		}

		out = append(out, &Hypothesis{
			Tokens: append(append(
				[]string(nil), hypothesis.Tokens...,
			), entry.Token),
			Score: hypothesis.Score + math.Log(entry.Probability),
		})
	}

	if len(out) == 0 {
		return []*Hypothesis{hypothesis}
	}

	return out
}

/*
Prune sorts by descending Score and keeps at most width hypotheses.
*/
func (hypothesis *Hypothesis) Prune(
	hyps []*Hypothesis, width int,
) []*Hypothesis {
	if width <= 0 || len(hyps) == 0 {
		return hyps
	}

	sort.Slice(hyps, func(left, right int) bool {
		return hyps[left].Score > hyps[right].Score
	})

	if len(hyps) > width {
		return append([]*Hypothesis(nil), hyps[:width]...)
	}

	return hyps
}

/*
LayerOpen is true when some hypothesis is not yet closed on endToken.
*/
func (hypothesis *Hypothesis) LayerOpen(
	hyps []*Hypothesis, endToken string,
) bool {
	for _, hyp := range hyps {
		if len(hyp.Tokens) == 0 {
			return true
		}

		if hyp.Tokens[len(hyp.Tokens)-1] != endToken {
			return true
		}
	}

	return false
}

/*
Continuations turns surviving hypotheses into surface strings; strips endToken
from the emitted run and joins with joiner. initialPrefixLen is len(prefix)
before generation so only generated suffix contributes to Sequence.
*/
func Continuations(
	initialPrefixLen int, hyps []*Hypothesis, endToken, joiner string,
) []*Continuation {
	out := make([]*Continuation, 0, len(hyps))

	for _, hyp := range hyps {
		generated := make([]string, 0, len(hyp.Tokens))
		start := min(initialPrefixLen, len(hyp.Tokens))

		for _, token := range hyp.Tokens[start:] {
			if token == endToken {
				continue
			}

			generated = append(generated, token)
		}

		out = append(out, &Continuation{
			Sequence: strings.Join(generated, joiner),
			Score:    hyp.Score,
		})
	}

	return out
}
