package process

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

func slog(count int) float64 {
	if count <= 0 {
		return 0
	}
	return float64(count) * math.Log(float64(count))
}

/*
NewDistribution allocates an empty Distribution.
*/
func NewDistribution() *Distribution {
	return &Distribution{}
}

/*
Clone returns a copy of the Distribution. Struct assignment copies all fields
including the fixed-size histogram, producing a full value-copy.
*/
func (dist *Distribution) Clone() *Distribution {
	cloned := *dist
	return &cloned
}

/*
N returns the total sample count.
*/
func (dist *Distribution) N() int {
	return dist.n
}

/*
NumDistinct returns the number of distinct byte values with non-zero count.
*/
func (dist *Distribution) NumDistinct() int {
	return dist.numDistinct
}

/*
Add increments the count for byteVal and updates sumSLogC for MDL Cost.
*/
func (dist *Distribution) Add(byteVal byte) {
	old := dist.counts[byteVal]
	dist.sumSLogC += slog(old+1) - slog(old)
	dist.counts[byteVal]++
	if old == 0 {
		dist.numDistinct++
	}
	dist.n++
}

/*
Remove decrements the count for byteVal. Caller must ensure byteVal was previously Add-ed.
*/
func (dist *Distribution) Remove(byteVal byte) {
	old := dist.counts[byteVal]
	dist.sumSLogC += slog(old-1) - slog(old)
	dist.counts[byteVal]--
	if old == 1 {
		dist.numDistinct--
	}
	dist.n--
}

/*
AddFrom merges other's histogram into dist. Updates n, numDistinct, and sumSLogC for Cost().
*/
func (dist *Distribution) AddFrom(other *Distribution) {
	for idx := range other.counts {
		old := dist.counts[idx]
		newCount := old + other.counts[idx]
		dist.sumSLogC += slog(newCount) - slog(old)
		if old == 0 && other.counts[idx] > 0 {
			dist.numDistinct++
		}
		dist.counts[idx] = newCount
	}
	dist.n += other.n
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
	var entropy float64
	total := float64(dist.n)
	for _, count := range dist.counts {
		if count == 0 {
			continue
		}
		probability := float64(count) / total
		entropy -= probability * math.Log(probability)
	}
	return entropy
}
