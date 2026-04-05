package kadabra

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
	"strings"
	"sync"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/store/markovtrie"
)

/*
NodeID is the 64-bit identifier used by the Kadabra routing layer.
*/
type NodeID uint64

/*
NodeOption configures a KadabraNode.
*/
type NodeOption func(*KadabraNode)

const (
	defaultKadabraBucketSize = 20
	defaultReplicationFactor = 3
	defaultLookupParallelism = 3
	defaultEpochQueries      = 100
)

/*
AffinityWords is the number of uint64 words in an affinity vector.
*/
const AffinityWords = 8

/*
KadabraNode wraps a SequenceStore in a Kademlia-style DHT node with per-bucket
adaptive peer selection inspired by Kadabra.
*/
type KadabraNode struct {
	ID                       NodeID
	epoch                    uint64 // gossip / digest generation; atomically updated (see Gossip, Digest)
	Store                    *markovtrie.Store
	Affinity                 [AffinityWords]uint64
	affinityCount            uint64
	BucketSize               int
	ReplicationFactor        int
	LookupParallelism        int
	EpochQueries             int
	Penalty                  float64
	SecurityThreshold        float64
	BucketSecurityThresholds []float64
	random                   *rand.Rand
	recordsMu                sync.RWMutex
	records                  map[uint64]SequenceRecord
	routingBits              int
	buckets                  []*kadabraBucket
	Field                    *FieldView

	// gossipCh sequences Gossip work on a single goroutine so node.epoch and
	// Digest remain race-free when multiple buckets finish epochs concurrently.
	gossipCh chan struct{}
}

/*
bucketIndexForPeer maps remote XOR distance into this node's bucket slice.
*/
func (node *KadabraNode) bucketIndexForPeer(remote NodeID) int {
	if node == nil {
		return 0
	}

	return computeKadabraBucketIndex(node.ID, remote, node.routingBits)
}

func (node *KadabraNode) peerTableCapacity() int {
	if node == nil || node.BucketSize <= 0 {
		return 0
	}

	bits := node.routingBits

	if bits <= 0 {
		bits = defaultKadabraRoutingBits
	}

	return node.BucketSize * bits
}

/*
NewKadabraNode constructs a Kadabra DHT node backed by a SequenceStore.
*/
func NewKadabraNode(id NodeID, options ...NodeOption) *KadabraNode {
	bits := defaultKadabraRoutingBits
	bucketSize := defaultKadabraBucketSize
	replication := defaultReplicationFactor
	parallelism := defaultLookupParallelism
	epochQueries := defaultEpochQueries

	if core.Cfg != nil {
		if core.Cfg.Kadabra.Bits > 0 {
			bits = core.Cfg.Kadabra.Bits
		}

		if core.Cfg.Kadabra.BucketSize > 0 {
			bucketSize = core.Cfg.Kadabra.BucketSize
		}

		if core.Cfg.Kadabra.ReplicationFactor > 0 {
			replication = core.Cfg.Kadabra.ReplicationFactor
		}

		if core.Cfg.Kadabra.Alpha > 0 {
			parallelism = core.Cfg.Kadabra.Alpha
		}

		if core.Cfg.Kadabra.EpochQueries > 0 {
			epochQueries = core.Cfg.Kadabra.EpochQueries
		}
	}

	node := &KadabraNode{
		ID:                id,
		BucketSize:        bucketSize,
		ReplicationFactor: replication,
		LookupParallelism: parallelism,
		EpochQueries:      epochQueries,
		routingBits:       bits,
		records:           make(map[uint64]SequenceRecord),
		buckets:           make([]*kadabraBucket, bits),
		Field:             nil, // set after node is constructed
	}

	for bucketIndex := range node.buckets {
		node.buckets[bucketIndex] = &kadabraBucket{
			Index:         bucketIndex,
			Candidates:    make(map[NodeID]*kadabraPeer),
			PreviousScore: math.Inf(-1),
			Samples:       make(map[NodeID]*kadabraPeerSample),
		}
	}

	for _, option := range options {
		option(node)
	}

	if node.Store == nil {
		node.Store = markovtrie.NewStore()
	}

	if node.random == nil {
		node.random = rand.New(rand.NewSource(int64(id) + 1))
	}

	node.Field = newFieldView(node)

	node.gossipCh = make(chan struct{}, 256)
	go node.runGossipWorker()

	return node
}

/*
Publish stores a labeled sequence on the closest DHT nodes by affinity
and trains each replica's markovtrie.Store. If the Value has a computed
affinity, routing uses affinity distance; otherwise it falls back to
XOR distance on the record key.
*/
func (node *KadabraNode) Publish(
	value primitive.Value, label string,
) (SequenceRecord, error) {
	record := SequenceRecord{
		Key:       HashSequenceRecord(value.String(), label),
		Sequence:  value.String(),
		Label:     label,
		Publisher: node.ID,
	}

	valueAff := ValueAffinity(&value)
	hasAffinity := false
	for _, w := range valueAff {
		if w != 0 {
			hasAffinity = true
			break
		}
	}

	if !hasAffinity {
		return record, fmt.Errorf(
			"kadabra: refusing to publish Value with zero affinity — call ComputeAffinityLSH first",
		)
	}

	replicas := node.closestNodesByAffinity(valueAff, node.ReplicationFactor)

	for _, replica := range replicas {
		if err := replica.storeRecordWithAffinity(record, valueAff); err != nil {
			return record, err
		}
	}

	return record, nil
}

/*
StoreRecord stores a replicated sequence record locally and trains
the backing markovtrie.Store on each replica once per key.
*/
func (node *KadabraNode) StoreRecord(record SequenceRecord) error {
	return node.storeRecordWithAffinity(record, [AffinityWords]uint64{})
}

func (node *KadabraNode) storeRecordWithAffinity(record SequenceRecord, valueAffinity [AffinityWords]uint64) error {
	node.recordsMu.Lock()
	defer node.recordsMu.Unlock()

	if existing, exists := node.records[record.Key]; exists {
		if existing.Sequence == record.Sequence && existing.Label == record.Label {
			return nil
		}

		return fmt.Errorf(
			"kadabra: record %d conflict: stored sequence %q label %q vs incoming sequence %q label %q",
			record.Key,
			existing.Sequence,
			existing.Label,
			record.Sequence,
			record.Label,
		)
	}

	node.records[record.Key] = record
	node.Store.Insert(record.Sequence, record.Label)

	hasAffinity := false
	for _, w := range valueAffinity {
		if w != 0 {
			hasAffinity = true
			break
		}
	}

	if hasAffinity {
		node.updateAffinity(valueAffinity)
	}

	return nil
}

/*
HasRecord reports whether the node stores the given DHT key locally.
*/
func (node *KadabraNode) HasRecord(key uint64) bool {
	node.recordsMu.RLock()
	defer node.recordsMu.RUnlock()

	_, exists := node.records[key]

	return exists
}

/*
NodeIDFromBytes derives a 64-bit node identifier from up to eight bytes.
*/
func NodeIDFromBytes(value []byte) NodeID {
	var buffer [8]byte
	copy(buffer[:], value)
	return NodeID(binary.BigEndian.Uint64(buffer[:]))
}

/*
NodeIDFromString hashes an arbitrary string into a 64-bit node identifier.
*/
func NodeIDFromString(value string) NodeID {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(strings.TrimSpace(value)))
	return NodeID(hasher.Sum64())
}

/*
WithLocalStore installs the local markovtrie.Store backing the node.
*/
func WithLocalStore(store *markovtrie.Store) NodeOption {
	return func(node *KadabraNode) {
		node.Store = store
	}
}

/*
WithBucketSize overrides the maximum peers held in each bucket.
*/
func WithBucketSize(bucketSize int) NodeOption {
	return func(node *KadabraNode) {
		if bucketSize <= 0 {
			return
		}

		node.BucketSize = bucketSize
	}
}

/*
WithReplicationFactor overrides how many closest nodes store each record.
*/
func WithReplicationFactor(replicationFactor int) NodeOption {
	return func(node *KadabraNode) {
		if replicationFactor <= 0 {
			return
		}

		node.ReplicationFactor = replicationFactor
	}
}

/*
WithLookupParallelism overrides how many peers are queried in each lookup step.
*/
func WithLookupParallelism(alpha int) NodeOption {
	return func(node *KadabraNode) {
		if alpha <= 0 {
			return
		}

		node.LookupParallelism = alpha
	}
}

/*
WithEpochQueries overrides how many peer queries form one Kadabra epoch.
*/
func WithEpochQueries(epochQueries int) NodeOption {
	return func(node *KadabraNode) {
		if epochQueries <= 0 {
			return
		}

		node.EpochQueries = epochQueries
	}
}

/*
WithPenalty overrides the scoring penalty for peers not used during an epoch.
When zero or negative, the node derives the penalty from the epoch mean.
*/
func WithPenalty(penalty float64) NodeOption {
	return func(node *KadabraNode) {
		node.Penalty = penalty
	}
}

/*
WithSecurityThreshold rejects exploration candidates below the given RTT floor.
*/
func WithSecurityThreshold(threshold float64) NodeOption {
	return func(node *KadabraNode) {
		if threshold < 0 {
			return
		}

		node.SecurityThreshold = threshold
	}
}

/*
WithBucketSecurityThresholds sets per-bucket exploration RTT floors.
*/
func WithBucketSecurityThresholds(thresholds []float64) NodeOption {
	return func(node *KadabraNode) {
		node.BucketSecurityThresholds = append([]float64(nil), thresholds...)
	}
}

/*
WithNodeRandomSource installs the random source used by Kadabra exploration.
*/
func WithNodeRandomSource(source rand.Source) NodeOption {
	return func(node *KadabraNode) {
		if source == nil {
			return
		}

		node.random = rand.New(source)
	}
}
