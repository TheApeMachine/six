package attention

import "github.com/theapemachine/six/pkg/core/algo/semantic"

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
		coOccurrence:    coOccurrence,
	}
}

/*
Run maps every token in the context slice and returns the alignments. The DHT
and store layers only compose this; they do not implement the walk.
*/
func (context *Context) Run() []semantic.Equivalent {
	engine := semantic.NewEquivalent(
		"",
		"",
		0,
		context.vocabularyOrder,
		context.coOccurrence,
	)

	out := make([]semantic.Equivalent, 0, len(context.tokens))

	for _, token := range context.tokens {
		match := engine.Run(token)
		out = append(out, semantic.Equivalent{
			Original:   match.Original,
			Mapped:     match.Mapped,
			Similarity: match.Similarity,
		})
	}

	return out
}
