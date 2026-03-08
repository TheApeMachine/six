package tokenizer

import "math"

type Distribution struct {
	counts      [256]int
	sumSLogC    float64
	n           int
	numDistinct int
}

func NewDistribution() *Distribution {
	return &Distribution{}
}

func (dist *Distribution) Clone() *Distribution {
	c := *dist
	return &c
}

func (dist *Distribution) Add(b byte) {
	old := dist.counts[b]
	dist.sumSLogC += slog(old+1) - slog(old)
	dist.counts[b]++
	if old == 0 {
		dist.numDistinct++
	}
	dist.n++
}

func (dist *Distribution) Remove(b byte) {
	old := dist.counts[b]
	dist.sumSLogC += slog(old-1) - slog(old)
	dist.counts[b]--
	if old == 1 {
		dist.numDistinct--
	}
	dist.n--
}

func (dist *Distribution) Cost() float64 {
	if dist.n <= 0 {
		return 0
	}
	// MDL cost: the negative log-likelihood of the data given the MLE model.
	return float64(dist.n)*math.Log(float64(dist.n)) - dist.sumSLogC
}

// Entropy returns Shannon entropy in nats (natural log base).
// Useful as a parallel detector: large entropy jumps often mark boundaries
// (e.g. plain text → gzipped blob, metadata → payload).
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
