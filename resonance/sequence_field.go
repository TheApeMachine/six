package resonance

import (
	"math"
	"sort"

	"github.com/theapemachine/six/data"
)

/*
SequenceField stores lightweight lexical-adjacency support over chord sequences.
It is deliberately simple: the boundary-value composer only needs enough local
statistics to score whether a candidate span coheres with its left and right
boundaries.
*/
type SequenceField struct {
	pairCounts   map[chordPair]int
	tripleCounts map[chordTriple]int
	leftCounts   map[data.Chord]int
	rightCounts  map[data.Chord]int
	followerSets map[data.Chord]map[data.Chord]int
	leaderSets   map[data.Chord]map[data.Chord]int
	middleSets   map[chordPair]map[data.Chord]int
	unigram      map[data.Chord]int
	totalPairs   int
	totalTriples int
}

type chordPair struct {
	Left  data.Chord
	Right data.Chord
}

type chordTriple struct {
	Left  data.Chord
	Mid   data.Chord
	Right data.Chord
}

/*
ChordScore ranks a chord by support within a SequenceField query.
*/
type ChordScore struct {
	Chord data.Chord
	Score float64
	Count int
}

/*
NewSequenceField builds a field from a corpus of lexical chord sequences.
*/
func NewSequenceField(sequences [][]data.Chord) *SequenceField {
	field := &SequenceField{
		pairCounts:   make(map[chordPair]int),
		tripleCounts: make(map[chordTriple]int),
		leftCounts:   make(map[data.Chord]int),
		rightCounts:  make(map[data.Chord]int),
		followerSets: make(map[data.Chord]map[data.Chord]int),
		leaderSets:   make(map[data.Chord]map[data.Chord]int),
		middleSets:   make(map[chordPair]map[data.Chord]int),
		unigram:      make(map[data.Chord]int),
	}

	for _, seq := range sequences {
		field.Observe(seq)
	}

	return field
}

/*
Observe adds one lexical sequence to the field.
*/
func (field *SequenceField) Observe(sequence []data.Chord) {
	for _, chord := range sequence {
		if chord.ActiveCount() == 0 {
			continue
		}
		field.unigram[chord]++
	}

	for i := 0; i+1 < len(sequence); i++ {
		left := sequence[i]
		right := sequence[i+1]
		if left.ActiveCount() == 0 || right.ActiveCount() == 0 {
			continue
		}

		pair := chordPair{Left: left, Right: right}
		field.pairCounts[pair]++
		field.leftCounts[left]++
		field.rightCounts[right]++
		field.totalPairs++

		if field.followerSets[left] == nil {
			field.followerSets[left] = make(map[data.Chord]int)
		}
		field.followerSets[left][right]++

		if field.leaderSets[right] == nil {
			field.leaderSets[right] = make(map[data.Chord]int)
		}
		field.leaderSets[right][left]++
	}

	for i := 0; i+2 < len(sequence); i++ {
		left := sequence[i]
		mid := sequence[i+1]
		right := sequence[i+2]

		if left.ActiveCount() == 0 || mid.ActiveCount() == 0 || right.ActiveCount() == 0 {
			continue
		}

		triple := chordTriple{Left: left, Mid: mid, Right: right}
		field.tripleCounts[triple]++
		field.totalTriples++

		bridge := chordPair{Left: left, Right: right}
		if field.middleSets[bridge] == nil {
			field.middleSets[bridge] = make(map[data.Chord]int)
		}
		field.middleSets[bridge][mid]++
	}
}

/*
PairScore returns a smoothed log-support score in [0, +∞) for the adjacency
left -> right. Zero means the pair has never been observed.
*/
func (field *SequenceField) PairScore(left, right data.Chord) float64 {
	if field == nil || left.ActiveCount() == 0 || right.ActiveCount() == 0 {
		return 0
	}

	count := field.pairCounts[chordPair{Left: left, Right: right}]
	if count == 0 {
		return 0
	}

	base := math.Log1p(float64(count))
	norm := math.Log1p(float64(max(field.leftCounts[left], field.rightCounts[right], 1)))
	if norm == 0 {
		return base
	}

	return base / norm
}

/*
SpanScore sums adjacency support across an entire span including its optional
left and right boundaries.
*/
func (field *SequenceField) SpanScore(leftBoundary []data.Chord, span []data.Chord, rightBoundary []data.Chord) float64 {
	if field == nil || len(span) == 0 {
		return 0
	}

	score := 0.0
	if len(leftBoundary) > 0 {
		score += field.PairScore(leftBoundary[len(leftBoundary)-1], span[0])
	}

	for i := 0; i+1 < len(span); i++ {
		score += field.PairScore(span[i], span[i+1])
	}

	if len(rightBoundary) > 0 {
		score += field.PairScore(span[len(span)-1], rightBoundary[0])
	}

	return score
}

/*
BridgeScore returns how well mid bridges left -> mid -> right.
This is a tiny but useful non-autoregressive primitive: a candidate can be
valuable even when neither local pair dominates alone, as long as it forms a
strong two-step bridge across the boundary gap.
*/
func (field *SequenceField) BridgeScore(left, mid, right data.Chord) float64 {
	if field == nil || mid.ActiveCount() == 0 {
		return 0
	}

	score := 0.0
	if left.ActiveCount() > 0 {
		score += field.PairScore(left, mid)
	}
	if right.ActiveCount() > 0 {
		score += field.PairScore(mid, right)
	}

	return score
}

/*
TripletScore returns a smoothed support score for the exact local triple
left -> mid -> right. This is stronger than separate pair scores because it
preserves the interior span structure directly.
*/
func (field *SequenceField) TripletScore(left, mid, right data.Chord) float64 {
	if field == nil || left.ActiveCount() == 0 || mid.ActiveCount() == 0 || right.ActiveCount() == 0 {
		return 0
	}

	count := field.tripleCounts[chordTriple{Left: left, Mid: mid, Right: right}]
	if count == 0 {
		return 0
	}

	bridgeTotal := 0
	for _, middleCount := range field.middleSets[chordPair{Left: left, Right: right}] {
		bridgeTotal += middleCount
	}

	norm := math.Log1p(float64(max(bridgeTotal, 1)))

	if norm == 0 {
		return math.Log1p(float64(count))
	}

	return math.Log1p(float64(count)) / norm
}

/*
TopMiddles returns the most strongly supported exact middle chords for
left -> ? -> right triples seen in the corpus.
*/
func (field *SequenceField) TopMiddles(left, right data.Chord, limit int) []ChordScore {
	if field == nil || limit <= 0 {
		return nil
	}

	middles := field.middleSets[chordPair{Left: left, Right: right}]
	if len(middles) == 0 {
		return nil
	}

	scored := make([]ChordScore, 0, len(middles))
	for chord, count := range middles {
		score := math.Log1p(float64(count)) + field.TripletScore(left, chord, right)
		scored = append(scored, ChordScore{Chord: chord, Count: count, Score: score})
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].Count > scored[j].Count
		}
		return scored[i].Score > scored[j].Score
	})

	if len(scored) > limit {
		scored = scored[:limit]
	}

	return scored
}

/*
TopBridges returns the best one-step bridge chords for left -> ? -> right.
It is intentionally lexical and cheap; the field solver uses it to seed domain
options that satisfy both boundaries before global optimization begins.
*/
func (field *SequenceField) TopBridges(left, right data.Chord, limit int) []ChordScore {
	if field == nil || limit <= 0 {
		return nil
	}

	scored := make([]ChordScore, 0, len(field.unigram))
	for chord, count := range field.unigram {
		if chord.ActiveCount() == 0 {
			continue
		}

		score := math.Log1p(float64(count))*0.05 + field.BridgeScore(left, chord, right)
		if score <= 0 {
			continue
		}

		scored = append(scored, ChordScore{
			Chord: chord,
			Count: count,
			Score: score,
		})
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].Count > scored[j].Count
		}
		return scored[i].Score > scored[j].Score
	})

	if len(scored) > limit {
		scored = scored[:limit]
	}

	return scored
}

/*
TopFollowers returns the most strongly supported immediate successors of chord.
*/
func (field *SequenceField) TopFollowers(chord data.Chord, limit int) []ChordScore {
	if field == nil || limit <= 0 {
		return nil
	}

	followers := field.followerSets[chord]
	return field.rankLocalSet(chord, followers, limit, true)
}

/*
TopPredecessors returns the most strongly supported immediate predecessors of chord.
*/
func (field *SequenceField) TopPredecessors(chord data.Chord, limit int) []ChordScore {
	if field == nil || limit <= 0 {
		return nil
	}

	leaders := field.leaderSets[chord]
	return field.rankLocalSet(chord, leaders, limit, false)
}

/*
TopChords returns the globally most frequent lexical chords in the field.
*/
func (field *SequenceField) TopChords(limit int) []ChordScore {
	if field == nil || limit <= 0 {
		return nil
	}

	scored := make([]ChordScore, 0, len(field.unigram))
	for chord, count := range field.unigram {
		scored = append(scored, ChordScore{
			Chord: chord,
			Count: count,
			Score: math.Log1p(float64(count)),
		})
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].Count > scored[j].Count
		}
		return scored[i].Score > scored[j].Score
	})

	if len(scored) > limit {
		scored = scored[:limit]
	}

	return scored
}

func (field *SequenceField) rankLocalSet(anchor data.Chord, local map[data.Chord]int, limit int, forward bool) []ChordScore {
	if len(local) == 0 {
		return nil
	}

	scored := make([]ChordScore, 0, len(local))
	for chord, count := range local {
		score := math.Log1p(float64(count))
		if forward {
			score += field.PairScore(anchor, chord)
		} else {
			score += field.PairScore(chord, anchor)
		}
		scored = append(scored, ChordScore{Chord: chord, Count: count, Score: score})
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].Count > scored[j].Count
		}
		return scored[i].Score > scored[j].Score
	})

	if len(scored) > limit {
		scored = scored[:limit]
	}

	return scored
}
