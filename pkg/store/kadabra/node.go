package kadabra

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/numeric"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/gossip"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/store/markovtrie"
	"github.com/theapemachine/six/pkg/viz"
)

const predictTrieFanout = 8

/*
meshLoadState is the atomically-swapped primary-ingest centroid snapshot.
*/
type meshLoadState struct {
	Affinity []uint64
	Count    uint64
}

/*
Node is a Kademlia-style DHT node. Its two public operations are
Publish (insert data, routed by affinity to the right MarkovTrie)
and Predict (run inference through the Field). Everything else is
internal plumbing delegated to composed specialist objects.
*/
type Node struct {
	ctx               context.Context
	cancel            context.CancelFunc
	err               error
	ID                uint64
	conn              *gossip.Conn
	field             *geometry.Field
	tries             atomic.Pointer[[]*markovtrie.Store]
	routing           *RoutingTable
	bucketSize        int
	replicationFactor int
	securityThreshold float64
	queue             *pool.Queue
	meshLoad          atomic.Value
	onMeshExpand      func([]uint64) bool
	trieGraphVizMu    sync.Mutex
	trieGraphVizLast  map[int]time.Time
}

type nodeOption func(*Node)

/*
NewNode constructs a Kadabra DHT node.
*/
func NewNode(
	ctx context.Context,
	id string,
	queue *pool.Queue,
	options ...nodeOption,
) (*Node, error) {
	if ctx == nil {
		return nil, errnie.Error(fmt.Errorf("kadabra.NewNode: nil context"))
	}

	if strings.TrimSpace(id) == "" {
		return nil, errnie.Error(fmt.Errorf("kadabra.NewNode: empty id"))
	}

	ctx, cancel := context.WithCancel(ctx)

	idHash, err := numeric.HashString(id)

	if err != nil {
		cancel()

		return nil, errnie.Error(err)
	}

	node := &Node{
		ctx:               ctx,
		cancel:            cancel,
		ID:                idHash,
		conn:              gossip.NewConn(nil, nil),
		field:             geometry.NewField(geometry.Mod8191),
		bucketSize:        core.Cfg.Kadabra.BucketSize,
		replicationFactor: core.Cfg.Kadabra.ReplicationFactor,
		queue:             queue,
	}

	emptyTries := make([]*markovtrie.Store, 0)
	node.tries.Store(&emptyTries)

	for _, option := range options {
		option(node)
	}

	node.routing = NewRoutingTable(node)

	viz.DefaultBus.Publish(viz.NodeCreated(node.ID, id))

	return node, validate.Require(map[string]any{
		"ctx":    node.ctx,
		"cancel": node.cancel,
		"ID":     node.ID,
		"queue":  node.queue,
	})
}

/*
Close cancels the node context and returns any accumulated error.
*/
func (node *Node) Close() error {
	node.cancel()
	return node.err
}

/*
Error returns the node's current error state.
*/
func (node *Node) Error() error {
	return node.err
}

/*
Publish stores a labeled Value: the local node applies it to its trie
cluster, then Store fans out a StoreReplica payload to affinity-nearest
routing peers (see replication.go). Gossip digests remain orthogonal.

The Value bytes are copied into the SequenceRecord before scheduling, so
returning from Publish does not mean the trie has applied the row yet —
Store runs on the node Queue. Callers that need durability for ingest
before proceeding (for example vm.Machine.Load) must follow Publish bursts
with pool.Queue.Drain on that same queue.
*/
func (node *Node) Publish(
	value *primitive.Value, label string,
) (err error) {
	if value == nil {
		return errnie.Error(fmt.Errorf("kadabra: nil Value"))
	}

	viz.DefaultBus.Publish(viz.ValuePublished(node.ID, value.ID(), label))

	return nil
}
