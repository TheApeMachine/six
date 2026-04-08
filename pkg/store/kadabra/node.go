package kadabra

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math/bits"
	"math/rand"
	"slices"
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

const predictTrieFanout = 8

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

	affVec := value.AffinityVector()

	record := SequenceRecord{
		Value:     *value,
		Affinity:  affVec,
		Label:     label,
		Publisher: node.ID,
	}

	record.Key = record.Hash()

	if primitive.AffinityVectorIsZero(affVec) {
		return errnie.Error(fmt.Errorf(
			"kadabra: refusing to publish Value with zero affinity — call ComputeAffinityLSH first",
		))
	}

	if err := node.Store(record); err != nil {
		return errnie.Error(err)
	}

	viz.DefaultBus.Publish(viz.ValuePublished(node.ID, record.Key, label))

	return nil
}

/*
Predict projects through the field, scores local tries by affinity to
the prompt, runs Predict only on the nearest subset, merges
continuations into the node-level beam, and breaks non-contributing
trie beams so they re-search.
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

	tries := node.triesSnapshot()
	selected := node.selectTriesForPredict(pv, tries, predictTrieFanout)

	var predictErr error

	for _, trie := range selected {
		triePred, err := trie.Predict(*pv)
		if err != nil {
			predictErr = errors.Join(predictErr, err)
		}

		if triePred != nil {
			observation.Continuations = append(observation.Continuations, triePred.Continuations...)
			observation.Labels = append(observation.Labels, triePred.Labels...)
		}
	}

	viz.DefaultBus.Publish(viz.BeamCollectEvent(
		node.ID, len(selected), len(observation.Continuations),
	))

	result, _ := node.beam.Update(observation)

	bestScore := 0.0

	if len(result.Continuations) > 0 {
		bestScore = result.Continuations[0].Score
	}

	viz.DefaultBus.Publish(viz.BeamComposeEvent(
		node.ID,
		len(result.Continuations),
		len(result.Rejected),
		bestScore,
	))

	node.breakRejected(result.Rejected)

	if len(result.Continuations) > 0 {
		viz.DefaultBus.Publish(viz.BeamConvergeEvent(
			node.ID,
			string(result.Continuations[0].Sequence),
			bestScore,
		))
	}

	return result, predictErr
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

		viz.DefaultBus.Publish(viz.BeamBreakEvent(node.ID, trie.ID))
	}
}

func (node *Node) selectTriesForPredict(
	query *primitive.Value,
	tries []*markovtrie.Store,
	maxPick int,
) []*markovtrie.Store {
	if len(tries) <= maxPick {
		return tries
	}

	queryVec := query.AffinityVector()

	type scored struct {
		idx  int
		dist int
	}

	ranked := make([]scored, len(tries))

	for idx, trie := range tries {
		ranked[idx] = scored{
			idx:  idx,
			dist: affinityPopcountDistance(queryVec, trie.Affinity.Vector()),
		}
	}

	slices.SortFunc(ranked, func(left, right scored) int {
		return cmp.Compare(left.dist, right.dist)
	})

	out := make([]*markovtrie.Store, 0, maxPick)

	for pickIdx := 0; pickIdx < maxPick && pickIdx < len(ranked); pickIdx++ {
		out = append(out, tries[ranked[pickIdx].idx])
	}

	return out
}

func affinityPopcountDistance(
	query, candidate [primitive.AffinityWords]uint64,
) int {
	dist := 0

	for wordIdx := range primitive.AffinityWords {
		xor := query[wordIdx] ^ candidate[wordIdx]

		if wordIdx == primitive.AffinityWords-1 {
			xor &= primitive.AffinityLastWordMask
		}

		dist += bits.OnesCount64(xor)
	}

	return dist
}
