package kadabra

import (
	"unsafe"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/store/markovtrie"
)

/*
Store persists a replicated record locally and routes its content to
the best trie cluster via selectOrSpawnTrie. Fully lock-free: each
shard is an atomic pointer to an immutable map snapshot. On insert,
only the target shard's map (~1/256th of total records) is cloned
and CAS-swapped. Readers never block.
*/
func (node *Node) Store(
	record SequenceRecord,
) error {
	keys := make([]uint64, 0)

	node.tries.Range(func(key any, value any) bool {
		keys = append(keys, key.(uint64))
		return true
	})

	// Compare keys (affinity) with the record's affinity
	node.queue.Submit(func() {
		

	return nil
}

/*
selectOrSpawnTrie picks the closest trie within ClusterThreshold that
has not reached ShannonLimit. When nothing qualifies a fresh trie is
created, seeded with the incoming affinity, and atomically appended to
the node's tries slice.
*/
func (node *Node) selectOrSpawnTrie(
	affinity *primitive.Affinity,
) *markovtrie.Store {
	threshold := core.Cfg.Kadabra.ClusterThreshold
	shannonLimit := core.Cfg.Kadabra.ShannonLimit
	tries := rt.node.triesSnapshot()

	vectors := make([][primitive.AffinityWords]uint64, len(tries))

	for idx, cluster := range tries {
		if cluster != nil {
			vectors[idx] = cluster.Affinity.Vector()
		}
	}

	bestIdx := -1
	bestDist := primitive.AffinityWords * 64

	if len(vectors) > 0 {
		queryVec := affinity.Vector()
		udist := make([]uint32, len(vectors))

		rt.backend.BatchDistances(
			unsafe.Pointer(&queryVec[0]),
			unsafe.Pointer(&vectors[0][0]),
			len(vectors),
			udist,
		)

		for idx, dist := range udist {
			if int(dist) < bestDist {
				bestDist = int(dist)
				bestIdx = idx
			}
		}
	}

	if bestIdx >= 0 && bestDist <= threshold {
		cluster := tries[bestIdx]

		if cluster.Affinity.Popcount() < shannonLimit {
			count := cluster.AffinityCount.Load()
			cluster.AffinityCount.Store(cluster.Affinity.Blend(affinity, count, shannonLimit))

			return cluster
		}
	}

	store, err := markovtrie.NewStore(rt.node.ctx)

	if err != nil {
		return tries[0]
	}

	store.Affinity.SetVector(affinity.Vector())
	store.AffinityCount.Store(1)

	for {
		old := rt.node.tries.Load()
		var prev []*markovtrie.Store

		if old != nil {
			prev, _ = old.([]*markovtrie.Store)
		}

		next := make([]*markovtrie.Store, len(prev), len(prev)+1)
		copy(next, prev)
		next = append(next, store)

		if rt.node.tries.CompareAndSwap(old, next) {
			break
		}
	}

	return store
}
