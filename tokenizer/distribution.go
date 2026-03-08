package tokenizer

import "math"

/*
Distribution holds a byte histogram and incremental MDL cost (sumSLogC).
Used by Sequencer for MDL-based boundary detection; Cost() = n*log(n) - sumSLogC.
*/
type Distribution struct {
	counts      [256]int
	sumSLogC    float64
	n           int
	numDistinct int
}

/*
NewDistribution allocates an empty Distribution.
*/
func NewDistribution() *Distribution {
	return &Distribution{}
}

/*
Clone returns a shallow copy of the Distribution.
*/
func (dist *Distribution) Clone() *Distribution {
	c := *dist
	return &c
}

/*
Add increments the count for b and updates sumSLogC for MDL Cost.
*/
func (dist *Distribution) Add(b byte) {
	old := dist.counts[b]
	dist.sumSLogC += slog(old+1) - slog(old)
	dist.counts[b]++
	if old == 0 {
		dist.numDistinct++
	}
	dist.n++
}

/*
Remove decrements the count for b. Caller must ensure b was previously Add-ed.
*/
func (dist *Distribution) Remove(b byte) {
	old := dist.counts[b]
	dist.sumSLogC += slog(old-1) - slog(old)
	dist.counts[b]--
	if old == 1 {
		dist.numDistinct--
	}
	dist.n--
}

/*
Cost returns the MDL cost: n*log(n) - sumSLogC. Used for boundary gain calculation.
*/
func (dist *Distribution) Cost() float64 {
	if dist.n <= 0 {
		return 0
	}
	// MDL cost: the negative log-likelihood of the data given the MLE model.
	return float64(dist.n)*math.Log(float64(dist.n)) - dist.sumSLogC
}

/*
Entropy returns Shannon entropy in nats. H = -sum(p*log(p)) over non-zero counts.
*/
func (dist *Distribution) Entropy() float64 {
	if dist.n <= 0 {
		return 0
	}
	var h float64
	n := float64(dist.n)
	for _, c := range dist.counts {
		if c == 0 {
			continue
		}
		p := float64(c) / n
		h -= p * math.Log(p)
	}
	return h
}
