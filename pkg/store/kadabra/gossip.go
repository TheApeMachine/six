package kadabra

import (
	"sync/atomic"

	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/viz"
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
Digests produces one Digest per local trie cluster. Signals are pulled
directly from the algorithms that own them via Store.Signal — no
intermediate adaptive state. Each algorithm populates its Prediction
with the Derived chains it tracks; gossip just reads the latest values.
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

		origin := (gossip.owner.ID << 32) | uint64(uint32(trieIdx+1))

		surprisalMean := cluster.Signal(algo.Surprisal)
		classEntropy := cluster.Signal(algo.Entropy)
		growthRate := cluster.Signal(algo.GrowthRate)

		var prevSurprisal float64

		if prev, ok := gossip.owner.Field.digestLookup(origin); ok {
			prevSurprisal = prev.SurprisalMean
		}

		out = append(out, Digest{
			Origin:          origin,
			Affinity:        cluster.Affinity.Vector(),
			SurprisalMean:   surprisalMean,
			SurprisalGrowth: surprisalMean - prevSurprisal,
			SurprisalPrev:   prevSurprisal,
			ClassEntropy:    classEntropy,
			GrowthRate:      growthRate,
			Epoch:           epoch,
		})
	}

	for _, digest := range out {
		viz.DefaultBus.Publish(viz.FieldDigestEvent(
			gossip.owner.ID,
			digest.SurprisalMean,
			digest.ClassEntropy,
			digest.GrowthRate,
		))

		viz.DefaultBus.Publish(viz.GossipSent(gossip.owner.ID, digest.Epoch))
	}

	return out
}
