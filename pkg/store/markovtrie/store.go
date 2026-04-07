package markovtrie

import (
	"context"
	"log"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/algo/beam"
	"github.com/theapemachine/six/pkg/core/algo/classify"
	"github.com/theapemachine/six/pkg/core/algo/cooccurrence"
	"github.com/theapemachine/six/pkg/core/algo/episodic"
	"github.com/theapemachine/six/pkg/core/algo/train"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Store is a labeled trie with lazy-decayed counts. Higher-level concerns
(classification, episodic memory, co-occurrence, pattern extraction) are
composed as separate objects from the algo and numeric packages.

Trie shape and per-node statistics use atomic snapshots so concurrent
learners and readers do not serialize on a single RWMutex.
*/
type Store struct {
	ctx           context.Context
	cancel        context.CancelFunc
	root          *Node
	Affinity      primitive.Affinity
	AffinityCount atomic.Uint64
	cooccurrence  *cooccurrence.Matrix
	episodic      *episodic.Buffer
	algorithms    []algo.Algorithm
	generator     *beam.Search
	classifier    *classify.Classifier
	trainer       *train.Online
}

/*
Option configures a Store.
*/
type Option func(*Store)

/*
NewStore constructs a Store with token-level defaults.
*/
func NewStore(ctx context.Context, options ...Option) (*Store, error) {
	ctx, cancel := context.WithCancel(ctx)

	store := &Store{
		ctx:    ctx,
		cancel: cancel,
		cooccurrence: cooccurrence.NewMatrix(
			core.Cfg.MarkovTrie.CoOccurrenceWindow,
		),
		classifier: classify.NewClassifier(),
		Affinity:   primitive.Affinity{},
		algorithms: []algo.Algorithm{
			classify.NewClassifier(),
			beam.NewSearch(),
			train.NewOnline(),
		},
	}

	for _, option := range options {
		option(store)
	}

	return store, validate.Require(map[string]any{
		"ctx":    store.ctx,
		"cancel": store.cancel,
		"root":   store.root,
	})
}

/*
Load a value into the trie. Values are stored by value; edge keys use
Value.String() so the token region drives trie structure.
*/
func (store *Store) Load(
	value primitive.Value,
	labels ...string,
) {
	if store == nil {
		return
	}

	var triePath []primitive.Value

	store.Walk(store.root, func(node *Node) {
		triePath = append(triePath, node.value)
	})

	observation := algo.NewPrediction()

	for _, label := range labels {
		observation.TruncateForUpdate()
		observation.AddLabels(algo.Label{
			Label:      []byte(label),
			Confidence: 1,
		})
		observation.AddContext(triePath...)
		observation.AddContext(value)

		for _, algorithm := range store.algorithms {
			_, err := algorithm.Update(observation)

			if err != nil {
				log.Printf(
					"markovtrie.Store.Load: algorithm %T failed: %v",
					algorithm,
					err,
				)
			}
		}
	}

	if store.root == nil {
		store.root = NewNode(value)
	} else {
		store.root.storeChild(value, &Node{
			ID:    value.ID(),
			value: value,
		})
	}
}

/*
Signals returns the merged signal map from all algorithms on this
Store. Each algorithm populates the keys it owns on its Prediction;
this method flattens them into a single view. Duplicate keys are
last-writer-wins in algorithm order — the caller should not depend
on which algorithm "wins" a shared key.
*/
func (store *Store) Signals() map[algo.SignalType]float64 {
	out := make(map[algo.SignalType]float64)

	for _, algorithm := range store.algorithms {
		pred := algorithm.Value()

		if pred == nil || pred.Signals == nil {
			continue
		}

		for signal, derived := range pred.Signals {
			out[signal] = derived.Value()
		}
	}

	return out
}

/*
Signal returns a single signal value from the algorithm that owns it.
Returns 0 if no algorithm exposes the key.
*/
func (store *Store) Signal(signal algo.SignalType) float64 {
	for _, algorithm := range store.algorithms {
		pred := algorithm.Value()

		if pred == nil || pred.Signals == nil {
			continue
		}

		if derived, ok := pred.Signals[signal]; ok {
			return derived.Value()
		}
	}

	return 0
}

/*
ApplyFieldPressure sets external field forces
that modulate adaptive behavior.
*/
func (store *Store) ApplyFieldPressure(
	fieldSurprisal float64,
	fieldGrowth float64,
	decayMul float64,
) {
	if store == nil {
		return
	}
}
