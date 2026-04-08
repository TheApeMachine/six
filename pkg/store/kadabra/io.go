package kadabra

import (
	"math/bits"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/programmer"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/store/markovtrie"
	"github.com/theapemachine/six/pkg/viz"
)

func (node *Node) triesSnapshot() []*markovtrie.Store {
	p := node.tries.Load()
	if p == nil {
		return nil
	}
	return *p
}

/*
selectOrSpawnTrie picks the closest trie within ClusterThreshold that
has not reached ShannonLimit. When nothing qualifies a fresh trie is
created, seeded with the incoming affinity, and atomically appended to
the node's tries slice.

The entire distance computation and argmin reduction happen in-band
inside the Value frame. The programmer compiles a batch distance
layout, the backend dispatches it to the best substrate, and the
kernel writes the winning index and distance into words 22 and 23.
Go never touches raw distance arrays.
*/
func (node *Node) selectOrSpawnTrie(
	affinity *primitive.Affinity,
) *markovtrie.Store {
	threshold := core.Cfg.Kadabra.ClusterThreshold
	shannonLimit := core.Cfg.Kadabra.ShannonLimit
	tries := node.triesSnapshot()

	if len(tries) == 0 {
		return node.spawnTrie(affinity)
	}

	if len(tries) > kernel.MaxNearestAffinityCandidates {
		return node.selectOrSpawnTrieScalar(affinity, tries, threshold, shannonLimit)
	}

	candidates := make([][]uint64, len(tries))

	for idx, cluster := range tries {
		vec := cluster.Affinity.Vector()
		candidates[idx] = vec[:]
	}

	queryVec := affinity.Vector()

	var frame primitive.Value

	for idx, word := range queryVec {
		frame.Set(idx, word)
	}

	compiler := programmer.New(
		&frame,
		programmer.CompilerWithIntent(
			programmer.Intent{
				Operation: programmer.Distance,
				Assets:    candidates,
			},
		),
		programmer.CompilerWithBatchAffinityLayout(),
	)

	node.queue.ExecuteSync(compiler)

	bestIdx := int(frame[22])
	bestDist := int(frame[23])

	if bestIdx >= 0 && bestIdx < len(tries) && bestDist <= threshold {
		cluster := tries[bestIdx]

		if cluster.Affinity.Popcount() < shannonLimit {
			count := cluster.AffinityCount.Load()
			cluster.AffinityCount.Store(cluster.Affinity.Blend(affinity, count, shannonLimit))

			return cluster
		}
	}

	return node.spawnTrie(affinity)
}

/*
selectOrSpawnTrieScalar performs the same argmin-and-threshold policy as the
batch Value path when too many tries exist to pack below word 124.
*/
func (node *Node) selectOrSpawnTrieScalar(
	affinity *primitive.Affinity,
	tries []*markovtrie.Store,
	threshold int,
	shannonLimit int,
) *markovtrie.Store {
	query := affinity.Vector()
	bestIdx := -1
	bestDist := int(^uint(0) >> 1)

	for idx, cluster := range tries {
		cand := cluster.Affinity.Vector()
		dist := 0

		for wordIdx := range primitive.AffinityWords {
			xor := query[wordIdx] ^ cand[wordIdx]

			if wordIdx == primitive.AffinityWords-1 {
				xor &= primitive.AffinityLastWordMask
			}

			dist += bits.OnesCount64(xor)
		}

		if dist < bestDist {
			bestDist = dist
			bestIdx = idx
		}
	}

	if bestIdx >= 0 && bestIdx < len(tries) && bestDist <= threshold {
		cluster := tries[bestIdx]

		if cluster.Affinity.Popcount() < shannonLimit {
			count := cluster.AffinityCount.Load()
			cluster.AffinityCount.Store(cluster.Affinity.Blend(affinity, count, shannonLimit))

			return cluster
		}
	}

	return node.spawnTrie(affinity)
}

func (node *Node) spawnTrie(affinity *primitive.Affinity) *markovtrie.Store {
	store, err := markovtrie.NewStore(node.ctx, *affinity)

	if err != nil {
		tries := node.triesSnapshot()
		if len(tries) > 0 {
			return tries[0]
		}
		return nil
	}

	for {
		old := node.tries.Load()
		var prev []*markovtrie.Store

		if old != nil {
			prev = *old
		}

		next := make([]*markovtrie.Store, len(prev), len(prev)+1)
		copy(next, prev)
		store.ID = (node.ID << 32) | uint64(uint32(len(next)+1))
		next = append(next, store)

		if node.tries.CompareAndSwap(old, &next) {
			viz.DefaultBus.Publish(viz.NodeUpdated(node.ID, map[string]float64{
				"trie_count": float64(len(next)),
			}))

			break
		}
	}

	return store
}
