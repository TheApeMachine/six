package kadabra

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/algo/beam"
	"github.com/theapemachine/six/pkg/core/numeric"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/store/markovtrie"
	"github.com/theapemachine/six/pkg/viz"
)

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
	epoch             uint64
	tries             atomic.Pointer[[]*markovtrie.Store]
	Field             *Field
	beam              *beam.Search
	routing           *RoutingTable
	gossip            *Gossip
	random            *rand.Rand
	bucketSize        int
	replicationFactor int
	epochQueries      int
	securityThreshold float64
	queue             *pool.Queue
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
		bucketSize:        core.Cfg.Kadabra.BucketSize,
		replicationFactor: core.Cfg.Kadabra.ReplicationFactor,
		epochQueries:      core.Cfg.Kadabra.EpochQueries,
		queue:             queue,
	}

	emptyTries := make([]*markovtrie.Store, 0)
	node.tries.Store(&emptyTries)

	for _, option := range options {
		option(node)
	}

	if node.random == nil {
		node.random = rand.New(rand.NewSource(int64(node.ID) + 1))
	}

	node.routing = NewRoutingTable(node)

	node.gossip = &Gossip{
		owner: node,
	}

	node.Field = NewField(node)
	node.beam = beam.NewSearch()

	viz.DefaultBus.Publish(viz.NodeCreated(node.ID, id))

	return node, validate.Require(map[string]any{
		"ctx":    node.ctx,
		"cancel": node.cancel,
		"ID":     node.ID,
		"queue":  node.queue,
	})
}

/*
Gossip returns the node's gossip layer for digest propagation.
*/
func (node *Node) Gossip() *Gossip {
	return node.gossip
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
to remote peers is handled at the gossip layer, not here.
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

	viz.DefaultBus.Publish(viz.ValuePublished(node.ID, record.Key, label))

	return record, nil
}

/*
Predict fans the prompt to all tries and feeds their predictions
directly into the node-level beam. The beam selects which trie
continuations compose well (via Origin on each Continuation) and
breaks non-contributing trie beams so they re-search.
*/
func (node *Node) Predict(value Routable) (*algo.Prediction, error) {
	if node.err = validate.Require(map[string]any{
		"value": value,
	}); node.err != nil {
		return nil, errnie.Error(node.err)
	}

	pv, ok := value.(*primitive.Value)

	if !ok {
		return nil, errnie.Error(fmt.Errorf(
			"kadabra.Predict requires *primitive.Value",
		))
	}

	_, _ = node.Field.Project(pv)

	observation := algo.NewPrediction()
	observation.AddContext(*pv)

	for _, trie := range node.triesSnapshot() {
		triePred := trie.Predict(*pv)
		observation.Continuations = append(observation.Continuations, triePred.Continuations...)
		observation.Labels = append(observation.Labels, triePred.Labels...)
	}

	result, _ := node.beam.Update(observation)
	node.breakRejected(result.Rejected)

	return result, nil
}

/*
breakRejected sends a BreakBeam signal to tries whose Origins were
rejected by the node-level beam. Those tries reset their beam state
so they can re-search on the next round.
*/
func (node *Node) breakRejected(rejected []uint64) {
	if len(rejected) == 0 {
		return
	}

	rejectedSet := make(map[uint64]bool, len(rejected))

	for _, origin := range rejected {
		rejectedSet[origin] = true
	}

	breakSignal := algo.NewPrediction()
	breakSignal.Signals[algo.BreakBeam] = numeric.NewDerivedFrom(1)

	for _, trie := range node.triesSnapshot() {
		if !rejectedSet[trie.ID] {
			continue
		}

		for _, algorithm := range trie.Algorithms() {
			algorithm.Update(breakSignal)
		}
	}
}
