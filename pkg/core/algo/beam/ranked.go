package beam

import "sort"

/*
RankedToken is one branch label with a non-negative mass; SortDescending orders
them before beam consumes the top-k slice.
*/
type RankedToken struct {
	Token       string
	Probability float64
}

/*
RankedTokens is a slice of ranked choices with sorting affordances.

It exists so ordering policy lives on a method rather than a package-level
function, keeping the surface consistent with the rest of the codebase.
*/
type RankedTokens []RankedToken

func NewRankedTokens(tokens []string) RankedTokens {
	ranked := make(RankedTokens, len(tokens))

	for index, token := range tokens {
		ranked[index] = RankedToken{
			Token:       token,
			Probability: 0,
		}
	}

	return ranked
}

/*
SortDescending sorts by descending Probability, breaking ties on Token so two
runs with identical masses always agree on relative order (stability for beam
ties and tests).
*/
func (ranked RankedTokens) SortDescending() {
	sort.Slice(ranked, func(leftIndex int, rightIndex int) bool {
		left := ranked[leftIndex]
		right := ranked[rightIndex]

		if left.Probability == right.Probability {
			return left.Token < right.Token
		}

		return left.Probability > right.Probability
	})
}
