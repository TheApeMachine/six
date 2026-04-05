package kadabra

import "sync/atomic"

/*
Digest produces a FieldDigest from this node's current trie state and
affinity vector. This is the unit of gossip.
*/
func (node *KadabraNode) Digest() FieldDigest {
	signals := node.Store.AdaptiveDigest()

	// Carry forward previous surprisal for phase velocity computation.
	var prevSurprisal float64
	node.Field.mu.RLock()
	if prev, ok := node.Field.digests[node.ID]; ok {
		prevSurprisal = prev.SurprisalMean
	}
	node.Field.mu.RUnlock()

	return FieldDigest{
		Origin:        node.ID,
		Affinity:      node.Affinity,
		SurprisalMean: signals.SurprisalMean,
		SurprisalVar:  signals.SurprisalVar,
		SurprisalPrev: prevSurprisal,
		ClassEntropy:  signals.ClassEntropy,
		GrowthRate:    signals.GrowthRate,
		Depth:         signals.EffectiveDepth,
		Epoch:         atomic.LoadUint64(&node.epoch),
	}
}

/*
Gossip broadcasts this node's current digest to all peers in the routing
table. Each receiving peer absorbs the digest into its FieldView. Called
automatically at the end of each Kadabra epoch.
*/
func (node *KadabraNode) Gossip() {
	atomic.AddUint64(&node.epoch, 1)
	digest := node.Digest()

	// Absorb own digest so the field view includes self.
	node.Field.Absorb(digest)

	for _, bucket := range node.buckets {
		if bucket == nil {
			continue
		}

		bucket.mu.RLock()
		entriesCopy := append([]*kadabraPeer(nil), bucket.Entries...)
		bucket.mu.RUnlock()

		for _, peer := range entriesCopy {
			if peer == nil || peer.Node == nil {
				continue
			}

			peer.Node.Field.Absorb(digest)
		}
	}
}
