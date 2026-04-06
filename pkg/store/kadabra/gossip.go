package kadabra

import (
	"sync"
	"sync/atomic"
)

/*
Gossip manages the digest state for a Kadabra node's trie population.
Each local trie cluster produces its own digest — its voice in the
emergent field.
*/
type Gossip struct {
	mu      sync.RWMutex
	digests map[uint64]Digest
	owner   *Node
}

/*
Digests produces one Digest per local trie cluster. Each trie is a
participant in the field with its own affinity and adaptive signals.
*/
func (gossip *Gossip) Digests() []Digest {
	gossip.owner.triesMu.RLock()
	defer gossip.owner.triesMu.RUnlock()

	epoch := atomic.LoadUint64(&gossip.owner.epoch)
	out := make([]Digest, 0, len(gossip.owner.Tries))

	for trieIdx := range gossip.owner.Tries {
		cluster := gossip.owner.Tries[trieIdx]
		sig := cluster.Adaptive
		origin := uint64(gossip.owner.ID) + uint64(trieIdx) + 1

		var prevSurprisal float64

		gossip.owner.Field.mu.RLock()

		if prev, ok := gossip.owner.Field.digests[origin]; ok {
			prevSurprisal = prev.SurprisalMean
		}

		gossip.owner.Field.mu.RUnlock()

		out = append(out, Digest{
			Origin:          origin,
			Affinity:        cluster.Affinity.Vector(),
			SurprisalMean:   sig.SurprisalStats.Value(),
			SurprisalGrowth: sig.GrowthRateSmooth.Value(),
			SurprisalPrev:   prevSurprisal,
			ClassEntropy:    sig.EntropySmooth.Value(),
			GrowthRate:      sig.GrowthRateSmooth.Value(),
			Depth:           len(sig.DepthWeights),
			Epoch:           epoch,
		})
	}

	return out
}
