package markovtrie

import (
	"strings"

	"github.com/theapemachine/six/pkg/core/algo/beam"
	"github.com/theapemachine/six/pkg/core/numeric/probability"
)

/*
Generate samples a continuation with beam search; trie walks and ranking stay
in this package, beam supplies only data shapes and pure steps.
*/
func (store *Store) Generate(
	context string, label string, temperature float64, maxLength int,
) (string, error) {
	if store == nil {
		return "", nil
	}

	beamWidth := store.beamWidth

	if beamWidth <= 0 {
		beamWidth = 3
	}

	prefix := tokenizeContextWithoutBPE(context)
	endToken := "$"

	if store.bpe != nil {
		endToken = store.bpe.EndToken

		if enc := store.bpe.Encode(context); len(enc) > 0 {
			prefix = enc
		}
	}

	initialLen := len(prefix)

	hyps := []beam.Hypothesis{{
		Tokens: append([]string(nil), prefix...),
		Score:  0,
	}}

	for step := 0; step < maxLength; step++ {
		select {
		case <-store.ctx.Done():
			return "", store.ctx.Err()
		default:
		}

		candidateCap := beamWidth * beamWidth
		nextLayer := make([]beam.Hypothesis, 0, candidateCap)

		for _, hyp := range hyps {
			if len(hyp.Tokens) > 0 && hyp.Tokens[len(hyp.Tokens)-1] == endToken {
				nextLayer = append(nextLayer, hyp)
				continue
			}

			ranked := rankedChildrenForLabel(store, hyp.Tokens, label, temperature)

			branched := beam.Extend(hyp, ranked, beamWidth)
			nextLayer = append(nextLayer, branched...)
		}

		hyps = beam.Prune(nextLayer, beamWidth)

		if !beam.LayerOpen(hyps, endToken) {
			break
		}
	}

	cont := beam.Continuations(initialLen, hyps, endToken, "")

	if len(cont) == 0 {
		return "", nil
	}

	return cont[0].Sequence, nil
}

/*
rankedChildrenForLabel returns normalized label-conditional masses for outgoing
edges after walking prefix. It takes a read lock on store.mu for the walk.
*/
func rankedChildrenForLabel(
	store *Store, prefix []string, label string, temperature float64,
) beam.RankedTokens {
	if store == nil || store.root == nil {
		return nil
	}

	store.mu.RLock()
	defer store.mu.RUnlock()

	node := store.root

	for _, token := range prefix {
		child := node.Children[token]

		if child == nil {
			return nil
		}

		node = child
	}

	tokens := sortedChildTokens(node)

	if len(tokens) == 0 {
		return nil
	}

	ranked := beam.NewRankedTokens(tokens)
	sum := 0.0

	for index := range ranked {
		child := node.Children[ranked[index].Token]

		if child == nil {
			continue
		}

		weight := child.ClassCounts[label]

		if weight < 0 {
			weight = 0
		}

		ranked[index].Probability = weight
		sum += weight
	}

	if sum <= 0 {
		uniform := 1.0 / float64(len(ranked))

		for index := range ranked {
			ranked[index].Probability = uniform
		}
	} else {
		for index := range ranked {
			ranked[index].Probability /= sum
		}
	}

	if temperature >= 0 {
		shaped := make([]probability.Ranked, len(ranked))

		for index := range ranked {
			shaped[index] = probability.Ranked{
				Token:       ranked[index].Token,
				Probability: ranked[index].Probability,
			}
		}

		shaped = probability.TemperatureShape(shaped, temperature)

		if shaped == nil || len(shaped) != len(ranked) {
			return ranked
		}

		for index := range ranked {
			ranked[index].Probability = shaped[index].Probability
		}

		ranked.SortDescending()
	}

	return ranked
}

func tokenizeContextWithoutBPE(context string) []string {
	fields := strings.Fields(strings.TrimSpace(context))

	if len(fields) > 0 {
		return fields
	}

	if context == "" {
		return []string{""}
	}

	return []string{context}
}
