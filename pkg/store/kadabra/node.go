package kadabra

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"

	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/numeric"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Node is a Kademlia-style DHT node. Its two public operations are
Publish (insert data, routed by affinity to the right MarkovTrie)
and Predict (run inference through the Field). Everything else is
internal plumbing delegated to composed specialist objects.
*/
type Node struct {
	ctx           context.Context
	cancel        context.CancelFunc
	err           error
	ID            uint64
	epoch         uint64
	tries         sync.Map
	Affinity      *primitive.Affinity
	affinityCount uint64
	Field         *Field
	routing       *RoutingTable
	gossip        *Gossip
	random        *rand.Rand

	backend           *compute.Backend
	bucketSize        int
	replicationFactor int
	epochQueries      int
	securityThreshold float64
	queue             *pool.Queue
}

type nodeOption func(*Node)

/*
WithBackend injects the compute backend so the routing table can
dispatch affinity distance work to GPU/SIMD substrates.
*/
func WithBackend(backend *compute.Backend) nodeOption {
	return func(node *Node) {
		node.backend = backend
	}
}

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
		Affinity:          primitive.NewAffinity(),
		bucketSize:        core.Cfg.Kadabra.BucketSize,
		replicationFactor: core.Cfg.Kadabra.ReplicationFactor,
		epochQueries:      core.Cfg.Kadabra.EpochQueries,
		queue:             queue,
	}

	for _, option := range options {
		option(node)
	}

	if node.random == nil {
		node.random = rand.New(rand.NewSource(int64(node.ID) + 1))
	}

	node.routing = NewRoutingTable(node, node.backend)
	node.gossip = &Gossip{
		owner: node,
	}
	node.Field = NewField(node)

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
Publish stores a labeled Value in the local routing table, which
routes it to the correct MarkovTrie cluster by affinity. Replication
to remote peers is handled at the gossip layer, not here — calling
Closest per-value to find replicas was burning a full BatchDistances
pass 50M times only to return the local node every time.
*/
func (node *Node) Publish(
	value *primitive.Value, label string,
) (SequenceRecord, error) {
	if value == nil {
		return SequenceRecord{}, errnie.Error(fmt.Errorf("kadabra: nil Value"))
	}

	record := SequenceRecord{
		Value:     *value,
		Affinity:  value.AffinityVector(),
		Label:     label,
		Publisher: node.ID,
	}

	record.Key = record.Hash()

	valueAff := primitive.NewAffinityFromVector(
		value.AffinityVector(),
	)

	if valueAff.IsZero() {
		return record, errnie.Error(fmt.Errorf(
			"kadabra: refusing to publish Value with zero affinity — call ComputeAffinityLSH first",
		))
	}

	if err := node.Store(record); err != nil {
		return record, errnie.Error(err)
	}

	return record, nil
}

/*
Predict runs inference on the given value through the Field.
*/
func (node *Node) Predict(value Routable) (*algo.Prediction, error) {
	if node.err = validate.Require(map[string]any{
		"value": value,
	}); node.err != nil {
		return nil, errnie.Error(node.err)
	}

	return node.Field.Project(value)
}
