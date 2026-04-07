package kadabra

import (
	"github.com/theapemachine/six/pkg/compute/programmer"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/store/markovtrie"
)

func (node *Node) triesSnapshot() []*markovtrie.Store {
	p := node.tries.Load()
	if p == nil {
		return nil
	}
	return *p
}

/*
Store persists a replicated record locally and routes its content to
the best trie cluster via selectOrSpawnTrie. Fully lock-free: each
shard is an atomic pointer to an immutable map snapshot. On insert,
only the target shard's map (~1/256th of total records) is cloned
and CAS-swapped. Readers never block.
*/
func (node *Node) Store(record SequenceRecord) error {
	node.queue.Submit(func() {
		aff := primitive.NewAffinityFromVector(record.Affinity)
		trie := node.selectOrSpawnTrie(aff)

		if trie != nil {
			trie.Load(record.Value, record.Label)
		}
	})

	return nil
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

func (node *Node) spawnTrie(affinity *primitive.Affinity) *markovtrie.Store {
	store, err := markovtrie.NewStore(node.ctx)

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
		next = append(next, store)

		if node.tries.CompareAndSwap(old, &next) {
			break
		}
	}

	return store
}
