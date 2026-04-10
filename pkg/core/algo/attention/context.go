package attention

import (
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/algo/semantic"
)

/*
Context runs vocabulary attention over a pre-tokenized slice: each token is
mapped through semantic.Equivalent. Callers (e.g. a Markov trie) supply tokens
and co-occurrence state; this package does not know about nodes or the DHT.
*/
type Context struct {
	tokens          []string
	vocabularyOrder []string
	coOccurrence    map[string]map[string]float64
}

/*
NewContext builds attention state for one forward pass over tokens.
*/
func NewContext(
	tokens []string,
	vocabularyOrder []string,
	coOccurrence map[string]map[string]float64,
) *Context {
	return &Context{
		tokens:          tokens,
		vocabularyOrder: vocabularyOrder,
		coOccurrence:    filterCoOccurrence(vocabularyOrder, coOccurrence),
	}
}

/*
Update co-occurrence state for a new token.
*/
func (context *Context) Update(prediction *algo.Prediction) {
	for _, value := range prediction.Context {
		token := value.String()
		mapped := semantic.NewEquivalent(
			token,
			token,
			0,
			context.vocabularyOrder,
			context.coOccurrence,
		).Run(token).Mapped

		if _, exists := context.coOccurrence[mapped]; !exists {
			context.coOccurrence[mapped] = make(map[string]float64)
		}

		for _, otherToken := range context.tokens {
			otherMapped := semantic.NewEquivalent(
				otherToken,
				otherToken,
				0,
				context.vocabularyOrder,
				context.coOccurrence,
			).Run(otherToken).Mapped

			context.coOccurrence[mapped][otherMapped]++
		}
	}

	out := make([]semantic.Equivalent, 0, len(context.tokens))

	for _, token := range context.tokens {
		match := semantic.NewEquivalent(
			token,
			token,
			context.coOccurrence[token][token],
			context.vocabularyOrder,
			context.coOccurrence,
		)

		out = append(out, *match)
	}
}

func filterCoOccurrence(
	vocabularyOrder []string,
	coOccurrence map[string]map[string]float64,
) map[string]map[string]float64 {
	filtered := make(map[string]map[string]float64, len(vocabularyOrder))
	allowed := make(map[string]struct{}, len(vocabularyOrder))

	for _, token := range vocabularyOrder {
		allowed[token] = struct{}{}
		filtered[token] = make(map[string]float64)
	}

	for _, token := range vocabularyOrder {
		row := coOccurrence[token]

		if row == nil {
			continue
		}

		for other, score := range row {
			if _, ok := allowed[other]; !ok {
				continue
			}

			filtered[token][other] = score
		}
	}

	return filtered
}
