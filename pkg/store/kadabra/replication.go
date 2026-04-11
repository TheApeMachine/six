package kadabra

import (
	"fmt"
	"math/bits"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/viz"
)

// meshSaturationRejections counts primary ingests dropped after the mesh-load
// centroid path declines expansion (operators: correlate with routing pressure).
var meshSaturationRejections atomic.Uint64

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
func (node *Node) Store(record *primitive.Value) error {
	return node.enqueueRecord(record, true)
}

/*
StoreReplica applies a record originating from another mesh member.
It must not trigger another fanout round (fanout=false).
*/
func (node *Node) StoreReplica(record *primitive.Value) error {
	return node.enqueueRecord(record, false)
}

func (node *Node) enqueueRecord(record *primitive.Value, fanout bool) error {
	if node == nil {
		return errnie.Error(fmt.Errorf("kadabra: nil node"))
	}

	if node.queue == nil {
		return errnie.Error(fmt.Errorf("kadabra: nil queue"))
	}

	if record == nil {
		return errnie.Error(fmt.Errorf("kadabra: nil record"))
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

		affWords := (*record)[core.Cfg.Value.Region.Affinity.Start:]
		candidates := node.routing.Closest(affWords, node.replicationFactor)
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

func (node *Node) applyRecordToTrie(record *primitive.Value, primaryIngest bool) bool {
	if node == nil || node.routing == nil || record == nil {
		return false
	}

	if primaryIngest && !node.routing.claimRecordIfNew(*record) {
		return false
	}

	aff := (*record)[core.Cfg.Value.Region.Affinity.Start:]

	if primaryIngest {
		if !node.blendMeshLoadCentroid(aff) {
			rejects := meshSaturationRejections.Add(1)

			errnie.Warn(
				"kadabra: primary ingest dropped (mesh load centroid / expansion)",
				"metric", "mesh_saturation_rejections_total",
				"rejections", rejects,
				"node_id", node.ID,
				"record_key", record.ID(),
				"affinity_popcount", affinityWordsPopcount(aff),
			)

			node.routing.releaseRecordKey(record.ID())

			return false
		}
	}

	trie := node.selectOrSpawnTrie(record)

	if trie == nil {
		node.routing.releaseRecordKey(record.ID())

		return false
	}

	if err := trie.Load(*record); err != nil {
		node.routing.releaseRecordKey(record.ID())

		return false
	}

	trieIdx := node.trieIndex(trie)

	viz.DefaultBus.Publish(viz.TrieInsertEvent(
		node.ID, trieIdx, record.String(), "",
	))

	node.publishTrieGraphViz(trie)

	return true
}

/*
blendMeshLoadCentroid folds primary-ingest affinities toward meshLoad (CAS-
updated). While the centroid popcount stays below ShannonLimit it blends like
a trie cluster; at saturation it invokes onMeshExpand and resets when expansion
succeeds, matching selectOrSpawnTrie’s spawnTrie decision at node scale.
*/
func (node *Node) blendMeshLoadCentroid(incoming []uint64) bool {
	if incoming == nil {
		return false
	}

	shannonLimit := core.Cfg.Kadabra.ShannonLimit

	for {
		curIface := node.meshLoad.Load()

		var cur *meshLoadState

		if curIface != nil {
			var ok bool

			cur, ok = curIface.(*meshLoadState)

			if !ok {
				return false
			}
		}

		if cur == nil || cur.Count == 0 {
			next := &meshLoadState{
				Affinity: cloneUint64Slice(incoming),
				Count:    1,
			}

			if curIface == nil {
				if node.meshLoad.CompareAndSwap(nil, next) {
					return true
				}

				continue
			}

			if node.meshLoad.CompareAndSwap(curIface, next) {
				return true
			}

			continue
		}

		if affinityWordsPopcount(cur.Affinity) < shannonLimit {
			newAff, newCount := blendAffinityWords(cur.Affinity, incoming, cur.Count)
			next := &meshLoadState{
				Affinity: newAff,
				Count:    newCount,
			}

			if node.meshLoad.CompareAndSwap(curIface, next) {
				return true
			}

			continue
		}

		if node.onMeshExpand != nil && !node.onMeshExpand(incoming) {
			return false
		}

		next := &meshLoadState{
			Affinity: cloneUint64Slice(incoming),
			Count:    1,
		}

		if node.meshLoad.CompareAndSwap(curIface, next) {
			return true
		}
	}
}

func cloneUint64Slice(src []uint64) []uint64 {
	if src == nil {
		return nil
	}

	return append([]uint64(nil), src...)
}

func affinityWordsPopcount(words []uint64) int {
	total := 0

	for _, word := range words {
		total += bits.OnesCount64(word)
	}

	return total
}

/*
blendAffinityWords merges centroid and incoming by bitwise OR (set union in
bit space), matching trie centroid growth under Shannon pressure.
*/
func blendAffinityWords(current []uint64, incoming []uint64, count uint64) ([]uint64, uint64) {
	if len(current) != len(incoming) {
		return cloneUint64Slice(incoming), 1
	}

	out := make([]uint64, len(current))

	for idx := range current {
		out[idx] = current[idx] | incoming[idx]
	}

	return out, count + 1
}
