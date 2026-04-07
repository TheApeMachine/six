package beam

import (
	"strings"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/numeric"
	"github.com/theapemachine/six/pkg/core/numeric/adaptive"
)

/*
Search runs beam search over token distributions derived from the
Prediction's Context values. The trie Walk populates Context with
every node's Value — Search builds a frequency-based next-token
distribution from that and expands hypotheses using the existing
Hypothesis and RankedTokens infrastructure.

Signals produced:
  - Quality: EMA-smoothed best hypothesis score, measuring how
    confident the beam is in its top continuation.
*/
type Search struct {
	prediction *algo.Prediction
	quality    *numeric.Derived
	beamWidth  int
	maxHops    int
}

/*
NewSearch constructs a beam search algorithm. Width and hop limit
are read from config at construction time.
*/
func NewSearch() *Search {
	quality := numeric.NewDerived(
		numeric.WithDynamics(adaptive.NewEMA()),
	)

	prediction := algo.NewPrediction()
	prediction.Signals[algo.Quality] = quality

	width := core.Cfg.MarkovTrie.BeamWidth

	if width <= 0 {
		width = 3
	}

	return &Search{
		prediction: prediction,
		quality:    quality,
		beamWidth:  width,
		maxHops:    core.Cfg.MarkovTrie.MaximumPathLength,
	}
}

/*
Update builds a token frequency distribution from Context values,
ranks them, and runs beam expansion to produce Continuations.
*/
func (search *Search) Update(
	prediction *algo.Prediction,
) (*algo.Prediction, error) {
	if prediction == nil || len(prediction.Context) == 0 {
		return search.prediction, nil
	}

	ranked := search.rankFromContext(prediction)

	if len(ranked) == 0 {
		return search.prediction, nil
	}

	prefix := search.extractPrefix(prediction)

	seed := &Hypothesis{
		Tokens: prefix,
		Score:  0,
	}

	beams := []*Hypothesis{seed}
	endToken := core.Cfg.MarkovTrie.EndToken
	maxHops := search.maxHops

	if maxHops <= 0 {
		maxHops = 5
	}

	for range maxHops {
		var next []*Hypothesis

		for _, hyp := range beams {
			next = append(next, hyp.Extend(ranked, search.beamWidth)...)
		}

		beams = seed.Prune(next, search.beamWidth)

		if !seed.LayerOpen(beams, endToken) {
			break
		}
	}

	continuations := Continuations(len(prefix), beams, endToken, " ")

	if search.prediction.Continuations == nil {
		search.prediction.Continuations = make([]algo.Continuation, 0, len(continuations))
	}

	search.prediction.Continuations = search.prediction.Continuations[:0]

	for _, cont := range continuations {
		search.prediction.Continuations = append(
			search.prediction.Continuations,
			algo.Continuation{
				Sequence: []byte(cont.Sequence),
				Score:    cont.Score,
			},
		)
	}

	if len(beams) > 0 {
		search.quality.Next(beams[0].Score)
	}

	return search.prediction, nil
}

func (search *Search) Value() *algo.Prediction {
	return search.prediction
}

/*
rankFromContext builds a frequency-based token distribution from all
Context values and returns it as RankedTokens.
*/
func (search *Search) rankFromContext(
	prediction *algo.Prediction,
) RankedTokens {
	freq := make(map[string]float64)
	var total float64

	for _, value := range prediction.Context {
		for _, token := range strings.Fields(value.String()) {
			freq[token]++
			total++
		}
	}

	if total == 0 {
		return nil
	}

	ranked := make(RankedTokens, 0, len(freq))

	for token, count := range freq {
		ranked = append(ranked, RankedToken{
			Token:       token,
			Probability: count / total,
		})
	}

	ranked.SortDescending()

	return ranked
}

/*
extractPrefix pulls the last few context tokens to seed the beam.
*/
func (search *Search) extractPrefix(
	prediction *algo.Prediction,
) []string {
	if len(prediction.Context) == 0 {
		return nil
	}

	last := prediction.Context[len(prediction.Context)-1]

	return strings.Fields(last.String())
}
