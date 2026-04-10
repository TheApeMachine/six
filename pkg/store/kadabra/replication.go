package kadabra

import (
	"fmt"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/viz"
)

/*
Replication protocol — mesh v1 (in-process)

A successful Store(record) runs applyRecordToTrie on this node, then
schedules StoreReplica(record) on up to ReplicationFactor−1 distinct
routing peers. Peers are chosen by RoutingTable.Closest on the record
affinity, skipping the local node. Closest orders candidates by Hamming
distance to the record so replicas land on affinity-adjacent nodes.

StoreReplica uses the same trie path but sets fanout=false so replicas
are single-hop: no echo storms across the mesh.

All work uses the Node's pool.Queue (typically shared across an entire
Machine), so one Drain() flushes primary inserts and downstream
replica applies.

Admission is idempotent on SequenceRecord.Key: claimRecordIfNew rejects
duplicates before trie.Load, so replica retries and hash-identical
republishes do not inflate trie counters or emit duplicate viz inserts.
Primary fanout runs only when the local claim succeeds.

Future wire transport: freeze SequenceRecord as
(Key, Publisher uint64; Label string; Affinity [N]uint64; Value frame as
core.Cfg.Value.Bytes). Receivers decode, call StoreReplica, and must
not set fanout without an explicit signed hop TTL.
*/

/*
Store applies the record locally then fans out StoreReplica to routing peers.
*/
func (node *Node) Store(record SequenceRecord) error {
	return node.enqueueRecord(record, true)
}

/*
StoreReplica applies a record originating from another mesh member.
It must not trigger another fanout round (fanout=false).
*/
func (node *Node) StoreReplica(record SequenceRecord) error {
	return node.enqueueRecord(record, false)
}

func (node *Node) enqueueRecord(record SequenceRecord, fanout bool) error {
	if node == nil {
		return errnie.Error(fmt.Errorf("kadabra: nil node"))
	}

	if node.queue == nil {
		return errnie.Error(fmt.Errorf("kadabra: nil queue"))
	}

	node.queue.SubmitTracked(func() {
		inserted := node.applyRecordToTrie(record, fanout)

		if !inserted || !fanout || node.replicationFactor <= 1 {
			return
		}

		additionalCopies := node.replicationFactor - 1

		if additionalCopies < 1 {
			return
		}

		aff := primitive.AffinityWithVector(record.Affinity)
		candidates := node.routing.Closest(&aff, node.replicationFactor)
		scheduled := 0

		for _, remote := range candidates {
			if remote == nil || remote.ID == node.ID {
				continue
			}

			_ = remote.enqueueRecord(record, false)
			scheduled++

			if scheduled >= additionalCopies {
				break
			}
		}
	})

	return nil
}

func (node *Node) applyRecordToTrie(record SequenceRecord, primaryIngest bool) bool {
	if node == nil || node.routing == nil {
		return false
	}

	if !node.routing.claimRecordIfNew(record) {
		return false
	}

	aff := primitive.AffinityWithVector(record.Affinity)

	if primaryIngest {
		if !node.blendMeshLoadCentroid(&aff) {
			node.routing.releaseRecordKey(record.Key)

			return false
		}
	}

	trie := node.selectOrSpawnTrie(&aff)

	if trie == nil {
		node.routing.releaseRecordKey(record.Key)

		return false
	}

	if err := trie.Load(record.Value, record.Label); err != nil {
		node.routing.releaseRecordKey(record.Key)

		return false
	}

	trieIdx := node.trieIndex(trie)

	viz.DefaultBus.Publish(viz.TrieInsertEvent(
		node.ID, trieIdx, record.Value.String(), record.Label,
	))

	node.publishTrieGraphViz(trie)

	return true
}

/*
blendMeshLoadCentroid folds primary-ingest affinities toward meshLoadAffinity.
While the centroid popcount stays below ShannonLimit it blends like a trie
cluster; at saturation it invokes onMeshExpand and resets when expansion
succeeds, matching selectOrSpawnTrie’s spawnTrie decision at node scale.
*/
func (node *Node) blendMeshLoadCentroid(incoming *primitive.Affinity) bool {
	shannonLimit := core.Cfg.Kadabra.ShannonLimit

	node.meshLoadMu.Lock()
	defer node.meshLoadMu.Unlock()

	if node.meshLoadCount == 0 {
		node.meshLoadAffinity = *incoming
		node.meshLoadCount = 1

		return true
	}

	if node.meshLoadAffinity.Popcount() < shannonLimit {
		count := node.meshLoadCount
		node.meshLoadCount = node.meshLoadAffinity.Blend(incoming, count, shannonLimit)

		return true
	}

	if node.onMeshExpand != nil && !node.onMeshExpand(incoming) {
		return false
	}

	node.meshLoadAffinity = *incoming
	node.meshLoadCount = 1

	return true
}
