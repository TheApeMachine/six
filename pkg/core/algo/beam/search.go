package beam

import (
	"context"
	"math"
	"sort"
	"strings"
)

/*
Search walks a factored distribution stepwise: at each depth it keeps only
the best beamWidth prefixes under summed log-score. A store or model injects
conditional next-token masses via nextRanked, which keeps this type independent
of trie layout, BPE, or episodic blending.
*/
type Search struct {
	ctx           context.Context
	cancel        context.CancelFunc
	err           error
	initialTokens []string
	beamWidth     int
	maxSteps      int
	endToken      string
	tokenJoiner   string
}

/*
opts configures BeamSearch after construction (reserved for optional tuning
without growing the constructor arity).
*/
type opts func(*Search)

/*
beamHypothesis is transient search state: a token prefix and its cumulative
log-score. It stays unexported because only BeamSearch mutates these slices.
*/
type beamHypothesis struct {
	tokens []string
	score  float64
}

/*
NewSearch builds a search instance. initialTokens must match whatever the
nextRanked callback expects (including tokenizer and content filtering).
tokenJoiner is used only when joining generated tokens into Sequence; an empty
string concatenates raw token strings.
*/
func NewSearch(
	ctx context.Context,
	initialTokens []string,
	beamWidth int,
	maxSteps int,
	endToken string,
	tokenJoiner string,
	options ...opts,
) *Search {
	ctx, cancel := context.WithCancel(ctx)

	search := &Search{
		ctx:           ctx,
		cancel:        cancel,
		initialTokens: append([]string(nil), initialTokens...),
		beamWidth:     beamWidth,
		maxSteps:      maxSteps,
		endToken:      endToken,
		tokenJoiner:   tokenJoiner,
	}

	for _, option := range options {
		option(search)
	}

	return search
}

/*
Close releases the derived context; call once when the search object is done.
Any cancellation during Run surfaces through Error after Run returns.
*/
func (search *Search) Close() error {
	search.cancel()
	return search.err
}

/*
Error exposes cancellation or other fatal state after Run.
*/
func (search *Search) Error() error {
	return search.err
}

/*
Run performs beam expansion until maxSteps or until every beam ends on
endToken. Distributions need not be re-sorted here if nextRanked already
returns mass in descending order (the trie does), preserving work.
*/
func (search *Search) Run() []BeamContinuation {
	if search.beamWidth <= 0 || search.maxSteps <= 0 {
		return nil
	}

	initial := search.initialTokens
	beams := []beamHypothesis{{
		tokens: append([]string(nil), initial...),
		score:  0,
	}}

	for stepIndex := 0; stepIndex < search.maxSteps; stepIndex++ {
		select {
		case <-search.ctx.Done():
			search.err = search.ctx.Err()

			return nil
		default:
		}

		candidateCapacity := max(search.beamWidth*search.beamWidth, search.beamWidth)
		nextBeams := make([]beamHypothesis, 0, candidateCapacity)

		for _, beam := range beams {
			if len(beam.tokens) > 0 && beam.tokens[len(beam.tokens)-1] == search.endToken {
				nextBeams = append(nextBeams, beam)
				continue
			}

			ranked := NewRankedTokens(beam.tokens)
			ranked.SortDescending()

			if len(ranked) == 0 {
				nextBeams = append(nextBeams, beam)
				continue
			}

			limit := min(search.beamWidth, len(ranked))
			expandedAny := false

			for probabilityIndex := range limit {
				entry := ranked[probabilityIndex]

				if entry.Probability <= 0 {
					continue
				}

				expandedAny = true

				nextBeams = append(nextBeams, beamHypothesis{
					tokens: append(append([]string(nil), beam.tokens...), entry.Token),
					score: beam.score + math.Log(
						entry.Probability,
					),
				})
			}

			if !expandedAny {
				nextBeams = append(nextBeams, beam)
			}
		}

		sort.Slice(nextBeams, func(leftIndex int, rightIndex int) bool {
			return nextBeams[leftIndex].score > nextBeams[rightIndex].score
		})

		if len(nextBeams) > search.beamWidth {
			nextBeams = nextBeams[:search.beamWidth]
		}

		beams = nextBeams

		if !search.hypothesesOpenPastEnd(beams) {
			break
		}
	}

	continuations := make([]BeamContinuation, 0, len(beams))

	for _, beam := range beams {
		generatedTokens := make([]string, 0, len(beam.tokens))

		for _, token := range beam.tokens[len(initial):] {
			if token == search.endToken {
				continue
			}

			generatedTokens = append(generatedTokens, token)
		}

		sequence := strings.Join(generatedTokens, search.tokenJoiner)
		continuations = append(continuations, BeamContinuation{
			Sequence: sequence,
			Score:    beam.score,
		})
	}

	return continuations
}

/*
hypothesesOpenPastEnd reports whether at least one beam still wants another
expansion step (last token is not the end marker). Inverted from a raw
“all closed” check so the loop exit reads as “nothing left to extend”.
*/
func (search *Search) hypothesesOpenPastEnd(beams []beamHypothesis) bool {
	for _, beam := range beams {
		if len(beam.tokens) == 0 {
			return true
		}

		if beam.tokens[len(beam.tokens)-1] != search.endToken {
			return true
		}
	}

	return false
}
