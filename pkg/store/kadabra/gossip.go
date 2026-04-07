package kadabra

import (
	"sync/atomic"
)

/*
Gossip manages the digest state for a Kadabra node's trie population.
Each local trie cluster produces its own digest — its voice in the
emergent field.
*/
type Gossip struct {
	owner *Node
}

/*
Digests produces one Digest per local trie cluster. Each trie is a
participant in the field with its own affinity and adaptive signals.
*/
func (gossip *Gossip) Digests() []Digest {
	tries := gossip.owner.triesSnapshot()
	epoch := atomic.LoadUint64(&gossip.owner.epoch)
	out := make([]Digest, 0, len(tries))

	for trieIdx := range tries {
		cluster := tries[trieIdx]

		if cluster == nil {
			continue
		}

		sig := cluster.Adaptive
		origin := (gossip.owner.ID << 32) | uint64(uint32(trieIdx+1))

		var prevSurprisal float64

		if prev, ok := gossip.owner.Field.digestLookup(origin); ok {
			prevSurprisal = prev.SurprisalMean
		}

		surprisalMean := sig.SurprisalStats.Value()

		out = append(out, Digest{
			Origin:          origin,
			Affinity:        cluster.Affinity.Vector(),
			SurprisalMean:   surprisalMean,
			SurprisalGrowth: surprisalMean - prevSurprisal,
			SurprisalPrev:   prevSurprisal,
			ClassEntropy:    sig.EntropySmooth.Value(),
			GrowthRate:      sig.GrowthRateSmooth.Value(),
			Depth:           len(sig.DepthWeights),
			Epoch:           epoch,
		})
	}

	return out
}
