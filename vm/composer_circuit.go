package vm

import (
	"math"
	"sort"

	"github.com/theapemachine/six/data"
)

type beamHypothesis struct {
	Span  []data.Chord
	Score float64
}

func (composer *BoundaryComposer) solveCircuitBeam(
	boundary SpanBoundary,
	domains [][]data.Chord,
	priors []map[data.Chord]float64,
	pairPriors []map[pairPriorKey]float64,
	tripletPriors []map[triplePriorKey]float64,
	logicField *composerLogicField,
) fieldSolution {
	width := boundary.Width
	if width <= 0 {
		return fieldSolution{}
	}

	beamLimit := max(48, composer.domainLimit*max(composer.topK, 4))
	beamLimit = min(beamLimit, 256)

	beam := []beamHypothesis{{}}
	leftAnchor := lastChord(boundary.Left)

	for pos := 0; pos < width; pos++ {
		next := make([]beamHypothesis, 0, len(beam)*len(domains[pos]))

		for _, hypothesis := range beam {
			for _, chord := range domains[pos] {
				if chord.ActiveCount() == 0 {
					continue
				}

				score := hypothesis.Score
				score += composer.unaryPotential(boundary, pos, chord, priors, logicField)

				if pos == 0 && leftAnchor.ActiveCount() > 0 {
					score += composer.transitionPotential(-1, leftAnchor, chord, pairPriors, logicField)
				}

				if pos > 0 {
					prev := hypothesis.Span[pos-1]
					score += composer.transitionPotential(pos-1, prev, chord, pairPriors, logicField)
				}

				if pos == 1 && leftAnchor.ActiveCount() > 0 {
					score += composer.triplePotential(-1, leftAnchor, hypothesis.Span[0], chord, tripletPriors, logicField)
				} else if pos > 1 {
					score += composer.triplePotential(pos-2, hypothesis.Span[pos-2], hypothesis.Span[pos-1], chord, tripletPriors, logicField)
				}

				span := append(append([]data.Chord(nil), hypothesis.Span...), chord)
				if logicField != nil {
					score += logicField.prefixScore(span)
				}

				next = append(next, beamHypothesis{Span: span, Score: score})
			}
		}

		if len(next) == 0 {
			return fieldSolution{}
		}

		sort.Slice(next, func(i, j int) bool {
			if next[i].Score == next[j].Score {
				return spanLess(next[i].Span, next[j].Span)
			}
			return next[i].Score > next[j].Score
		})

		if len(next) > beamLimit {
			next = next[:beamLimit]
		}

		beam = next
	}

	bestScore := math.Inf(-1)
	var bestSpan []data.Chord

	for _, hypothesis := range beam {
		score := hypothesis.Score
		if len(boundary.Right) > 0 {
			rightAnchor := boundary.Right[0]
			last := hypothesis.Span[width-1]
			score += composer.transitionPotential(width-1, last, rightAnchor, pairPriors, logicField)

			if width > 1 {
				score += composer.triplePotential(-1, hypothesis.Span[width-2], last, rightAnchor, tripletPriors, logicField)
			} else if leftAnchor.ActiveCount() > 0 {
				score += composer.triplePotential(-1, leftAnchor, last, rightAnchor, tripletPriors, logicField)
			}
		}

		if logicField != nil {
			score += logicField.spanScore(hypothesis.Span)
		}

		if score > bestScore || (score == bestScore && spanLess(hypothesis.Span, bestSpan)) {
			bestScore = score
			bestSpan = append([]data.Chord(nil), hypothesis.Span...)
		}
	}

	if len(bestSpan) == 0 || math.IsInf(bestScore, -1) {
		return fieldSolution{}
	}

	return fieldSolution{Span: bestSpan, Score: bestScore}
}

func spanLess(left, right []data.Chord) bool {
	if len(right) == 0 {
		return true
	}

	limit := min(len(left), len(right))
	for idx := 0; idx < limit; idx++ {
		leftRank := chordRank(left[idx])
		rightRank := chordRank(right[idx])
		if leftRank == rightRank {
			continue
		}
		return leftRank < rightRank
	}

	return len(left) < len(right)
}
