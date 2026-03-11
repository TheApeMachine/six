package resonance

import "github.com/theapemachine/six/data"

/*
OverlapScore returns a symmetric overlap ratio in [0, 1].
1.0 means identical support, 0 means no shared structure.
*/
func OverlapScore(left, right *data.Chord) float64 {
	shared := data.ChordSimilarity(left, right)

	if shared == 0 {
		return 0
	}

	leftActive := max(left.ActiveCount(), 1)
	rightActive := max(right.ActiveCount(), 1)

	return (2.0 * float64(shared)) / float64(leftActive+rightActive)
}

/*
AffineScore combines direct structural overlap with overlap in the associated
carrier chords, allowing rotational state to influence resonance without
overpowering the primary structural match.
*/
func AffineScore(query, candidate, queryCarrier, candidateCarrier *data.Chord) float64 {
	structural := OverlapScore(query, candidate)

	if queryCarrier == nil || candidateCarrier == nil {
		return structural
	}

	carrier := OverlapScore(queryCarrier, candidateCarrier)

	return structural*0.75 + carrier*0.25
}
