package beam

import (
	"bytes"
	"math"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/numeric"
	"github.com/theapemachine/six/pkg/core/numeric/adaptive"
	"github.com/theapemachine/six/pkg/core/numeric/gf"
)

/*
Search runs beam search over token distributions. At the trie level,
it builds a next-token distribution from the trie's stored Values and
expands hypotheses to produce continuations (predicted sequences).

At the node level, the exact same Search type is reused — but instead
of ranking tokens from a trie's Context values, it ranks the
continuations that trie-level beams already produced. This means the
node's "vocabulary" is entire phrases from tries, not individual tokens.
The node beam selects which trie contributions compose well together
and can break (reset) trie beams that don't fit.

Signals produced:
  - Quality: EMA-smoothed best hypothesis score, measuring how
    confident the beam is in its top continuation.
*/
type Search struct {
	prediction *algo.Prediction
	quality    *numeric.Derived
	beamWidth  int
	maxHops    int
	phaseIndex atomic.Uint32
	phaseGain  atomic.Uint64
}

/*
NewSearch constructs a beam search algorithm. Width controls how many
hypotheses survive each expansion step — wider beams explore more but
cost more. MaxHops limits how many expansion rounds happen before the
beam commits to its best hypotheses.
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
Reset clears all continuations and zeroes the quality signal, returning
the beam to its initial empty state. The node-level beam calls this
(via the BreakBeam signal) on trie-level beams whose output doesn't
contribute to the composed result — forcing them to re-search from
scratch on the next Update, potentially finding different paths now
that the node has committed to contributions from other tries.
*/
func (search *Search) Reset() {
	search.prediction.Continuations = search.prediction.Continuations[:0]
	search.quality.Next(0)
}

/*
Update is the main entry point for one round of beam search. It does
four things in sequence:

 1. Check for a BreakBeam signal — if present, reset and return early.
 2. Rank the input — either from incoming continuations (node-level
    path) or from Context values (trie-level path).
 3. Expand hypotheses — take the current beam, branch each hypothesis
    by the top-ranked candidates, prune back to beamWidth.
 4. Emit continuations — convert surviving hypotheses into the output
    Continuations slice and update the Quality signal.
*/
func (search *Search) Update(
	prediction *algo.Prediction,
) (*algo.Prediction, error) {
	if prediction == nil {
		return search.prediction, nil
	}

	search.capturePhaseSignals(prediction)

	if search.shouldBreak(prediction) {
		return search.prediction, nil
	}

	ranked := search.rank(prediction)

	if len(ranked) == 0 {
		return search.prediction, nil
	}

	beams := search.expand(ranked, prediction)
	search.emit(beams, prediction)

	return search.prediction, nil
}

/*
Value returns the beam's current prediction state. Other components
read this to see what continuations the beam has produced and what
the quality signal looks like.
*/
func (search *Search) Value() *algo.Prediction {
	return search.prediction
}

/*
shouldBreak checks whether the incoming prediction carries a BreakBeam
signal. This is how the node-level beam communicates "your output was
rejected" to a trie-level beam. When broken, the beam resets all its
state so the next Update starts fresh — this is the backtracking
mechanism that lets the system explore alternative paths.
*/
func (search *Search) shouldBreak(prediction *algo.Prediction) bool {
	if _, breaking := prediction.Signals[algo.BreakBeam]; !breaking {
		return false
	}

	search.Reset()

	return true
}

/*
rank produces a scored list of candidate tokens from the input. It
tries two sources in order:

First, incoming Continuations — these come from child beam searches
(trie-level beams feeding into a node-level beam). Each continuation's
sequence becomes a single "token" in the parent beam's vocabulary,
scored by converting log-scores to probabilities via softmax.

Second, Context values — these come from trie walks during learning.
The beam tokenizes each Value's string representation and counts
frequencies to build a distribution. This is the original trie-level
path where individual tokens are the vocabulary.

The two sources are mutually exclusive by design: when a node feeds
continuations in, there's no meaningful Context to rank against.
*/
func (search *Search) rank(prediction *algo.Prediction) RankedTokens {
	ranked := search.rankFromContinuations(prediction)

	if len(ranked) > 0 {
		return ranked
	}

	return search.rankFromContext(prediction)
}

/*
expand takes the ranked candidates and grows the hypothesis tree.
Starting from a seed hypothesis (built from the last Context value's
tokens as prefix), it repeatedly branches: each existing hypothesis
is extended by the top-ranked candidates, then the entire set is
pruned back to beamWidth. This repeats for maxHops rounds or until
all hypotheses have reached an end token.

The result is a set of surviving hypotheses — complete sequences
ranked by cumulative log-probability. Wider beams keep more
alternatives alive; more hops allow longer generated sequences.
*/
func (search *Search) expand(
	ranked RankedTokens, prediction *algo.Prediction,
) []*Hypothesis {
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

	return beams
}

/*
emit converts the surviving hypotheses into the output format
(algo.Continuation with byte sequence and score) and updates the
Quality signal. It also computes Rejected — the set of Origins that
contributed continuations to the input but whose contributions did
not survive beam pruning. The node reads Rejected to know which
trie beams to break.
*/
func (search *Search) emit(beams []*Hypothesis, prediction *algo.Prediction) {
	prefix := search.extractPrefix(prediction)
	endToken := core.Cfg.MarkovTrie.EndToken

	continuations := Continuations(len(prefix), beams, endToken, " ")

	if search.prediction.Continuations == nil {
		search.prediction.Continuations = make([]algo.Continuation, 0, len(continuations))
	}

	search.prediction.Continuations = search.prediction.Continuations[:0]

	selected := make(map[string]bool, len(continuations))

	for _, cont := range continuations {
		selected[cont.Sequence] = true

		search.prediction.Continuations = append(
			search.prediction.Continuations,
			algo.Continuation{
				Sequence: []byte(cont.Sequence),
				Score:    cont.Score,
			},
		)
	}

	allOrigins := make(map[uint64]bool)
	selectedOrigins := make(map[uint64]bool)

	for _, cont := range prediction.Continuations {
		if cont.Origin == 0 {
			continue
		}

		allOrigins[cont.Origin] = true

		if selected[string(cont.Sequence)] {
			selectedOrigins[cont.Origin] = true
		}
	}

	search.prediction.Rejected = search.prediction.Rejected[:0]

	for origin := range allOrigins {
		if !selectedOrigins[origin] {
			search.prediction.Rejected = append(search.prediction.Rejected, origin)
		}
	}

	if len(beams) > 0 {
		search.quality.Next(beams[0].Score)
	}
}

/*
rankFromContinuations converts incoming continuations from child beam
searches into ranked tokens. This is the node-level beam path: each
trie has already produced its best continuations, and the node treats
each one as a candidate "token" in its own beam search.

Scores are log-probabilities, so we apply log-softmax normalization:
subtract the maximum score (for numerical stability) and exponentiate
to get relative probabilities. A trie continuation with score -2.0
vs another at -5.0 means the first is ~e^3 ≈ 20x more likely.
*/
func (search *Search) rankFromContinuations(
	prediction *algo.Prediction,
) RankedTokens {
	if len(prediction.Continuations) == 0 {
		return nil
	}

	ranked := make(RankedTokens, 0, len(prediction.Continuations))
	var maxScore float64

	for _, cont := range prediction.Continuations {
		if cont.Score > maxScore {
			maxScore = cont.Score
		}
	}

	for _, cont := range prediction.Continuations {
		prob := math.Exp(cont.Score - maxScore)
prob = search.phaseWeightedProbability(cont.Sequence, prob)

		ranked = append(ranked, RankedToken{
			Token:       string(cont.Sequence),
			Probability: prob,
		})
	}

	ranked.SortDescending()

	return ranked
}

/*
rankFromContext builds a frequency-based token distribution from the
Context values in the prediction. Each Value's string representation
is split into whitespace-delimited tokens, and their frequencies
become the probability distribution. This is how a trie-level beam
discovers what tokens are available in its local data — high-frequency
tokens get higher probability and are more likely to survive pruning.
*/
func (search *Search) rankFromContext(
	prediction *algo.Prediction,
) RankedTokens {
	freq := make(map[string]float64)
	var total float64

	for _, value := range prediction.Context {
		slab := value.TokenRegionBytes()

		for _, field := range bytes.Fields(slab) {
			token := string(field)
			freq[token]++
			total++
		}
	}

	if total == 0 {
		return nil
	}

	ranked := make(RankedTokens, 0, len(freq))

	for token, count := range freq {
		probability := count / total
		probability *= search.phaseWeightedProbability([]byte(token), 1)

		ranked = append(ranked, RankedToken{
			Token:       token,
			Probability: probability,
		})
	}

	ranked.SortDescending()

	return ranked
}

/*
extractPrefix pulls the tokens from the last Context value to seed
the beam's starting hypothesis. The beam doesn't generate from
nothing — it extends an existing token prefix. For trie-level beams,
this is the most recent observation's tokens. For node-level beams,
this is the prompt value's token content.
*/
func (search *Search) extractPrefix(
	prediction *algo.Prediction,
) []string {
	if len(prediction.Context) == 0 {
		return nil
	}

	last := prediction.Context[len(prediction.Context)-1]
	slab := last.TokenRegionBytes()
	fields := bytes.Fields(slab)

	if len(fields) == 0 {
		return nil
	}

	out := make([]string, len(fields))

	for idx, field := range fields {
		out[idx] = string(field)
	}

	return out
}

func (search *Search) capturePhaseSignals(prediction *algo.Prediction) {
	if prediction == nil || prediction.Signals == nil {
		return
	}

	if globalPhase, ok := prediction.Signals[algo.GlobalPhase]; ok && globalPhase != nil {
		phaseIndex := int(math.Round(globalPhase.Value()))

		if phaseIndex < 0 {
			search.phaseIndex.Store(0)
		} else {
			search.phaseIndex.Store(uint32((phaseIndex % gf.PhaseWidth) + 1))
		}
	}

	if phaseConcentration, ok := prediction.Signals[algo.PhaseConcentration]; ok && phaseConcentration != nil {
		phaseGain := phaseConcentration.Value()

		if phaseGain < 0 {
			phaseGain = 0
		}

		search.phaseGain.Store(math.Float64bits(phaseGain))
	}
}

func (search *Search) phaseState() (int, float64) {
	encodedPhase := search.phaseIndex.Load()
	phaseGain := math.Float64frombits(search.phaseGain.Load())

	if encodedPhase == 0 {
		return -1, phaseGain
	}

	return int(encodedPhase) - 1, phaseGain
}

func (search *Search) phaseWeightedProbability(sequence []byte, probability float64) float64 {
	phaseIndex, phaseGain := search.phaseState()

	if phaseIndex < 0 || phaseGain <= 0 || len(sequence) == 0 {
		return probability
	}

	candidatePhase := gf.DominantForBytes(sequence)
	alignment := gf.Alignment(candidatePhase.Index, phaseIndex)
	bias := gf.InterferenceMultiplier(alignment, phaseGain)

	return probability * bias
}
