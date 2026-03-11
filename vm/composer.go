package vm

import (
	"sort"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/resonance"
	"github.com/theapemachine/six/vm/cortex"
)

/*
SpanBoundary describes a masked sequence region to be solved.
Left and Right are the fixed boundary conditions around the gap.
Width is the number of missing chords. When Width <= 0, the composer falls back
to a single-step prediction, which is the natural boundary-value analogue of
"next token prediction".
*/
type SpanBoundary struct {
	Left  []data.Chord
	Right []data.Chord
	Width int
	Hints []data.Chord
	Logic cortex.LogicSnapshot
}

/*
BoundaryComposer solves missing spans globally rather than autoregressively.
It combines forward and reverse substrate retrieval with lexical adjacency
support from resonance.SequenceField. Because every slot is re-scored against
both boundaries at each iteration, earlier choices can be revised when later
slots reveal a better global continuation.
*/
type BoundaryComposer struct {
	forward     *geometry.HybridSubstrate
	reverse     *geometry.HybridSubstrate
	field       *resonance.SequenceField
	topK        int
	iterations  int
	domainLimit int
}

type composerOpts func(*BoundaryComposer)

type spanCandidate struct {
	Chords []data.Chord
	Score  float64
	Source string
}

/*
NewBoundaryComposer creates a new boundary-value span solver from a Loader.
*/
func NewBoundaryComposer(loader *Loader, opts ...composerOpts) *BoundaryComposer {
	composer := &BoundaryComposer{
		topK:        12,
		iterations:  8,
		domainLimit: 10,
		field:       resonance.NewSequenceField(nil),
	}

	if loader != nil {
		composer.forward = loader.Substrate()
		composer.reverse = loader.ReverseSubstrate()
		composer.field = resonance.NewSequenceField(loader.Sequences())
	}

	for _, opt := range opts {
		opt(composer)
	}

	return composer
}

/*
Compose solves a missing span under the supplied boundary conditions.
*/
func (composer *BoundaryComposer) Compose(boundary SpanBoundary) []data.Chord {
	width := boundary.Width
	if width <= 0 {
		width = composer.estimateWidth(boundary)
	}
	if width <= 0 {
		return nil
	}
	boundary.Width = width

	candidates := composer.collectCandidates(boundary)
	priors := composer.buildPriors(boundary, candidates)
	pairPriors := composer.buildPairPriors(boundary, candidates)
	tripletPriors := composer.buildTripletPriors(boundary, candidates)
	logicField := newComposerLogicField(boundary)
	domains := composer.buildDomains(boundary, priors, logicField)
	solution := composer.solveField(boundary, domains, priors, pairPriors, tripletPriors, logicField)
	if len(solution.Span) == 0 {
		return composer.fallbackSpan(boundary)
	}

	for iter := 0; iter < composer.iterations; iter++ {
		repair := logicField.repairSuggestions(solution.Span)
		if !composer.mergeRepairSuggestions(domains, priors, repair) {
			break
		}

		next := composer.solveField(boundary, domains, priors, pairPriors, tripletPriors, logicField)
		if len(next.Span) == 0 || next.Score <= solution.Score {
			break
		}
		solution = next
	}

	return solution.Span
}

/*
PredictNext returns the best single next chord for the visible prompt.
Although the output width is one chord, the decision is still made through the
same boundary-value machinery rather than an irreversible left-to-right commit.
*/
func (composer *BoundaryComposer) PredictNext(prompt []data.Chord, hints []data.Chord) data.Chord {
	result := composer.Compose(SpanBoundary{
		Left:  prompt,
		Width: 1,
		Hints: hints,
	})
	if len(result) == 0 {
		return data.Chord{}
	}
	return result[0]
}

/*
Complete predicts a fixed-width continuation to the right of prompt.
*/
func (composer *BoundaryComposer) Complete(prompt []data.Chord, width int, hints []data.Chord) []data.Chord {
	return composer.Compose(SpanBoundary{
		Left:  prompt,
		Width: width,
		Hints: hints,
	})
}

func (composer *BoundaryComposer) estimateWidth(boundary SpanBoundary) int {
	if boundary.Width > 0 {
		return boundary.Width
	}
	return 1
}

func (composer *BoundaryComposer) collectCandidates(boundary SpanBoundary) []spanCandidate {
	candidates := make([]spanCandidate, 0, composer.topK*4)
	width := boundary.Width

	forwardContexts := composer.contextVariants(boundary.Left, true)
	for idx, context := range forwardContexts {
		if composer.forward == nil || len(context) == 0 {
			continue
		}

		weight := 1.0 - float64(idx)*0.12
		if weight < 0.4 {
			weight = 0.4
		}

		filter := contextFilter(context)
		dial := geometry.NewPhaseDial().EncodeFromChords(context)
		ranked := composer.forward.RetrieveRanked(filter, dial, composer.topK)

		for _, candidate := range ranked {
			lexical := candidate.Lexical
			if len(lexical) == 0 {
				lexical = candidate.Readout
			}
			if len(lexical) == 0 {
				continue
			}

			span := lexical
			if len(span) > width {
				span = span[:width]
			}
			if len(span) == 0 {
				continue
			}

			candidates = append(candidates, spanCandidate{
				Chords: append([]data.Chord(nil), span...),
				Score:  composer.normalizeRetrievalScore(candidate.Score) * weight,
				Source: "forward",
			})
		}
	}

	reverseContexts := composer.contextVariants(boundary.Right, false)
	for idx, context := range reverseContexts {
		if composer.reverse == nil || len(context) == 0 {
			continue
		}

		reversedContext := reverseChordSlice(context)
		weight := 1.0 - float64(idx)*0.12
		if weight < 0.4 {
			weight = 0.4
		}

		filter := contextFilter(reversedContext)
		dial := geometry.NewPhaseDial().EncodeFromChords(reversedContext)
		ranked := composer.reverse.RetrieveRanked(filter, dial, composer.topK)

		for _, candidate := range ranked {
			lexical := candidate.Lexical
			if len(lexical) == 0 {
				lexical = candidate.Readout
			}
			if len(lexical) == 0 {
				continue
			}

			revSpan := lexical
			if len(revSpan) > width {
				revSpan = revSpan[:width]
			}
			if len(revSpan) == 0 {
				continue
			}

			span := reverseChordSlice(revSpan)
			candidates = append(candidates, spanCandidate{
				Chords: span,
				Score:  composer.normalizeRetrievalScore(candidate.Score) * weight,
				Source: "reverse",
			})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			if len(candidates[i].Chords) == len(candidates[j].Chords) {
				return candidates[i].Source < candidates[j].Source
			}
			return len(candidates[i].Chords) > len(candidates[j].Chords)
		}
		return candidates[i].Score > candidates[j].Score
	})

	if len(candidates) > composer.topK*4 {
		candidates = candidates[:composer.topK*4]
	}

	return candidates
}

func (composer *BoundaryComposer) buildPriors(boundary SpanBoundary, candidates []spanCandidate) []map[data.Chord]float64 {
	priors := make([]map[data.Chord]float64, boundary.Width)
	for i := range priors {
		priors[i] = make(map[data.Chord]float64)
	}

	for _, candidate := range candidates {
		for pos, chord := range candidate.Chords {
			if pos >= boundary.Width || chord.ActiveCount() == 0 {
				break
			}
			priors[pos][chord] += candidate.Score
		}
	}

	if composer.field != nil {
		if len(boundary.Left) > 0 {
			followers := composer.field.TopFollowers(boundary.Left[len(boundary.Left)-1], composer.topK)
			for _, follower := range followers {
				priors[0][follower.Chord] += follower.Score * 0.75
			}
		}

		if len(boundary.Right) > 0 {
			predecessors := composer.field.TopPredecessors(boundary.Right[0], composer.topK)
			for _, predecessor := range predecessors {
				priors[boundary.Width-1][predecessor.Chord] += predecessor.Score * 0.75
			}
		}

		fallbacks := composer.field.TopChords(composer.topK)
		for pos := range priors {
			if len(priors[pos]) > 0 {
				continue
			}
			for _, fallback := range fallbacks {
				priors[pos][fallback.Chord] += fallback.Score * 0.1
			}
		}
	}

	for _, hint := range boundary.Hints {
		if hint.ActiveCount() == 0 {
			continue
		}

		for pos := range priors {
			for chord := range priors[pos] {
				overlap := resonance.OverlapScore(&hint, &chord)
				if overlap > 0 {
					priors[pos][chord] += overlap * 0.35
				}
			}
		}
	}

	for pos := range priors {
		priors[pos] = composer.trimPrior(priors[pos], composer.topK)
	}

	return priors
}

func (composer *BoundaryComposer) seedState(boundary SpanBoundary, priors []map[data.Chord]float64) []data.Chord {
	if len(priors) == 0 {
		return nil
	}

	state := make([]data.Chord, len(priors))
	for pos := range priors {
		best, ok := composer.bestFromPrior(priors[pos])
		if !ok {
			return nil
		}
		state[pos] = best
	}
	return state
}

func (composer *BoundaryComposer) fallbackSpan(boundary SpanBoundary) []data.Chord {
	if boundary.Width <= 0 {
		return nil
	}

	span := make([]data.Chord, boundary.Width)
	for i := range span {
		if composer.field == nil {
			break
		}

		if i == boundary.Width-1 && len(boundary.Right) > 0 {
			predecessors := composer.field.TopPredecessors(boundary.Right[0], composer.topK)
			if len(predecessors) > 0 {
				span[i] = predecessors[0].Chord
				continue
			}
		}

		var anchor data.Chord
		if i == 0 {
			if len(boundary.Left) > 0 {
				anchor = boundary.Left[len(boundary.Left)-1]
			}
		} else {
			anchor = span[i-1]
		}

		followers := composer.field.TopFollowers(anchor, composer.topK)
		if len(followers) > 0 {
			span[i] = followers[0].Chord
			continue
		}

		top := composer.field.TopChords(1)
		if len(top) > 0 {
			span[i] = top[0].Chord
		}
	}

	return span
}

func (composer *BoundaryComposer) bestForPosition(
	boundary SpanBoundary,
	pos int,
	current []data.Chord,
	prior map[data.Chord]float64,
	candidates []spanCandidate,
) data.Chord {
	options := make(map[data.Chord]float64, len(prior)+8)
	for chord, score := range prior {
		options[chord] += score
	}

	if pos > 0 && composer.field != nil {
		for _, follower := range composer.field.TopFollowers(current[pos-1], 4) {
			options[follower.Chord] += follower.Score * 0.25
		}
	}

	if pos+1 < len(current) && composer.field != nil {
		for _, predecessor := range composer.field.TopPredecessors(current[pos+1], 4) {
			options[predecessor.Chord] += predecessor.Score * 0.25
		}
	}

	if current[pos].ActiveCount() > 0 {
		options[current[pos]] += 0.01
	}

	var (
		bestChord data.Chord
		bestScore = -1.0
		haveBest  bool
	)

	for chord, baseScore := range options {
		if chord.ActiveCount() == 0 {
			continue
		}

		score := baseScore
		if composer.field != nil {
			if pos == 0 && len(boundary.Left) > 0 {
				score += composer.field.PairScore(boundary.Left[len(boundary.Left)-1], chord) * 2.0
			}
			if pos > 0 {
				score += composer.field.PairScore(current[pos-1], chord) * 1.5
			}
			if pos+1 < len(current) {
				score += composer.field.PairScore(chord, current[pos+1]) * 1.5
			} else if len(boundary.Right) > 0 {
				score += composer.field.PairScore(chord, boundary.Right[0]) * 2.0
			}
		}

		score += composer.hintAffinity(chord, boundary.Hints)
		score += composer.spanConsensus(pos, chord, current, candidates)

		if !haveBest || score > bestScore || (score == bestScore && chordRank(chord) < chordRank(bestChord)) {
			bestChord = chord
			bestScore = score
			haveBest = true
		}
	}

	return bestChord
}

func (composer *BoundaryComposer) spanConsensus(pos int, chord data.Chord, current []data.Chord, candidates []spanCandidate) float64 {
	total := 0.0

	for _, candidate := range candidates {
		if pos >= len(candidate.Chords) || candidate.Chords[pos] != chord {
			continue
		}

		matches := 0
		compared := 0
		for i := 0; i < len(current) && i < len(candidate.Chords); i++ {
			if i == pos {
				continue
			}
			compared++
			if current[i] == candidate.Chords[i] {
				matches++
			}
		}

		total += candidate.Score * (0.25 + float64(matches)/float64(compared+1))
	}

	return total
}

func (composer *BoundaryComposer) hintAffinity(chord data.Chord, hints []data.Chord) float64 {
	total := 0.0
	for _, hint := range hints {
		if hint.ActiveCount() == 0 {
			continue
		}
		total += resonance.OverlapScore(&chord, &hint) * 0.2
	}
	return total
}

func (composer *BoundaryComposer) bestFromPrior(prior map[data.Chord]float64) (data.Chord, bool) {
	var (
		best  data.Chord
		score float64
		have  bool
	)

	for chord, value := range prior {
		if !have || value > score || (value == score && chordRank(chord) < chordRank(best)) {
			best = chord
			score = value
			have = true
		}
	}

	return best, have
}

func (composer *BoundaryComposer) trimPrior(prior map[data.Chord]float64, limit int) map[data.Chord]float64 {
	if len(prior) <= limit {
		return prior
	}

	type scored struct {
		Chord data.Chord
		Score float64
	}

	ranked := make([]scored, 0, len(prior))
	for chord, score := range prior {
		ranked = append(ranked, scored{Chord: chord, Score: score})
	}

	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Score == ranked[j].Score {
			return chordRank(ranked[i].Chord) < chordRank(ranked[j].Chord)
		}
		return ranked[i].Score > ranked[j].Score
	})

	trimmed := make(map[data.Chord]float64, limit)
	for i := 0; i < limit && i < len(ranked); i++ {
		trimmed[ranked[i].Chord] = ranked[i].Score
	}
	return trimmed
}

func (composer *BoundaryComposer) normalizeRetrievalScore(score float64) float64 {
	if score <= 0 {
		return 0.001
	}
	return score
}

func (composer *BoundaryComposer) contextVariants(chords []data.Chord, tail bool) [][]data.Chord {
	if len(chords) == 0 {
		return nil
	}

	sizes := []int{len(chords), 16, 8, 4}
	seen := make(map[int]struct{})
	variants := make([][]data.Chord, 0, len(sizes))

	for _, size := range sizes {
		if size <= 0 {
			continue
		}
		if size > len(chords) {
			size = len(chords)
		}
		if _, ok := seen[size]; ok {
			continue
		}
		seen[size] = struct{}{}

		var context []data.Chord
		if tail {
			context = chords[len(chords)-size:]
		} else {
			context = chords[:size]
		}
		variants = append(variants, append([]data.Chord(nil), context...))
	}

	return variants
}

func contextFilter(chords []data.Chord) data.Chord {
	var filter data.Chord
	for _, chord := range chords {
		filter = data.ChordOR(&filter, &chord)
	}
	return filter
}

func reverseChordSlice(chords []data.Chord) []data.Chord {
	if len(chords) == 0 {
		return nil
	}

	out := make([]data.Chord, len(chords))
	for i := range chords {
		out[len(chords)-1-i] = chords[i]
	}
	return out
}

func chordRank(chord data.Chord) int {
	face := chord.IntrinsicFace()
	if face != 256 {
		return face
	}
	return 256 + data.ChordBin(&chord)
}

/*
BoundaryComposerWithTopK sets how many competing substrate candidates are kept.
*/
func BoundaryComposerWithTopK(topK int) composerOpts {
	return func(composer *BoundaryComposer) {
		if topK > 0 {
			composer.topK = topK
		}
	}
}

/*
BoundaryComposerWithIterations sets the number of self-correction passes.
*/
func BoundaryComposerWithIterations(iterations int) composerOpts {
	return func(composer *BoundaryComposer) {
		if iterations > 0 {
			composer.iterations = iterations
		}
	}
}

/*
BoundaryComposerWithDomainLimit sets how many candidate chords are retained per
span slot before the global field solve runs.
*/
func BoundaryComposerWithDomainLimit(limit int) composerOpts {
	return func(composer *BoundaryComposer) {
		if limit > 0 {
			composer.domainLimit = limit
		}
	}
}
