package kadabra

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/numeric"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/store/markovtrie"
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
	Tries         []*markovtrie.Store
	triesMu       sync.RWMutex
	Affinity      *primitive.Affinity
	affinityCount uint64
	Field         *Field
	routing       *RoutingTable
	gossip        *Gossip
	random        *rand.Rand

	bucketSize        int
	replicationFactor int
	epochQueries      int
	securityThreshold float64
}

type nodeOption func(*Node)

/*
NewNode constructs a Kadabra DHT node.
*/
func NewNode(
	ctx context.Context, id string, options ...nodeOption,
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
		Tries:             make([]*markovtrie.Store, 0, 8),
		bucketSize:        core.Cfg.Kadabra.BucketSize,
		replicationFactor: core.Cfg.Kadabra.ReplicationFactor,
		epochQueries:      core.Cfg.Kadabra.EpochQueries,
	}

	for _, option := range options {
		option(node)
	}

	if node.random == nil {
		node.random = rand.New(rand.NewSource(int64(node.ID) + 1))
	}

	node.routing = NewRoutingTable(node)
	node.gossip = &Gossip{
		digests: make(map[uint64]Digest),
		owner:   node,
	}
	node.Field = NewField(node)

	return node, validate.Require(map[string]any{
		"ctx":    node.ctx,
		"cancel": node.cancel,
		"ID":     node.ID,
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
Publish routes a labeled Value to the closest DHT nodes by affinity
and stores the sequence on each replica's MarkovTrie. The Value
must have a computed affinity (non-zero AffinityVector).
*/
func (node *Node) Publish(
	value Routable, label string,
) (SequenceRecord, error) {
	content := value.String()

	record := SequenceRecord{
		Sequence:  content,
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

	replicas := node.routing.Closest(
		valueAff, node.replicationFactor,
	)

	var storeErrs []error

	for _, replica := range replicas {
		if replica == nil {
			continue
		}

		var err error

		if replica == node || replica.ID == node.ID {
			err = node.routing.Store(record, valueAff)
		} else {
			err = replica.routing.Store(record, valueAff)
		}

		if err != nil {
			storeErrs = append(storeErrs, err)
		}
	}

	if len(storeErrs) > 0 {
		return record, errnie.Error(errors.Join(storeErrs...))
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
