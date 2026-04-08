package markovtrie

import (
	"context"
	"log"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/algo/beam"
	"github.com/theapemachine/six/pkg/core/algo/causal"
	"github.com/theapemachine/six/pkg/core/algo/classify"
	"github.com/theapemachine/six/pkg/core/algo/cooccurrence"
	"github.com/theapemachine/six/pkg/core/algo/episodic"
	"github.com/theapemachine/six/pkg/core/algo/train"
	"github.com/theapemachine/six/pkg/core/numeric"
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
	ID            uint64
	Affinity      primitive.Affinity
	AffinityCount atomic.Uint64
	cooccurrence  *cooccurrence.Matrix
	episodic      *episodic.Buffer
	algorithms    []algo.Algorithm
	generator     *beam.Search
	classifier    *classify.Classifier
	trainer       *train.Online
	lastLeaf      atomic.Pointer[Node]
}

/*
Option configures a Store.
*/
type Option func(*Store)

/*
NewStore constructs a Store with token-level defaults.
*/
func NewStore(
	ctx context.Context,
	affinity primitive.Affinity,
	options ...Option,
) (*Store, error) {
	ctx, cancel := context.WithCancel(ctx)

	store := &Store{
		ctx:    ctx,
		cancel: cancel,
		cooccurrence: cooccurrence.NewMatrix(
			core.Cfg.MarkovTrie.CoOccurrenceWindow,
		),
		classifier: classify.NewClassifier(),
		Affinity:   affinity,
		algorithms: []algo.Algorithm{
			classify.NewClassifier(),
			beam.NewSearch(),
			train.NewOnline(),
			causal.NewGraph(),
		},
	}

	for _, option := range options {
		option(store)
	}

	return store, validate.Require(map[string]any{
		"ctx":    store.ctx,
		"cancel": store.cancel,
	})
}

/*
Load inserts a value into the trie. Edge keys use Value.String()
so the token region drives trie structure. If a path already
exists, the value lands at the existing branch; otherwise a new
child is created.
*/
func (store *Store) Load(
	value primitive.Value,
	labels ...string,
) {
	if store == nil {
		return
	}

	if store.root == nil {
		store.root = NewNode(value)
	}

	parent := store.root

	if len(labels) > 0 {
		parent = store.root
	} else if leaf := store.lastLeaf.Load(); leaf != nil {
		parent = leaf
	}

	token := value.String()
	existing := parent.Child(token)

	if existing != nil {
		existing.TotalVisits.Add(1)
		// Parent link was fixed when the child was first inserted.
	} else {
		child := NewNode(value)
		parent.storeChild(value, child)
		existing = child
	}

	if len(labels) > 0 {
		store.lastLeaf.Store(nil)
	} else {
		store.lastLeaf.Store(existing)
	}

	triePath := pathFromRoot(existing)

	observation := algo.NewPrediction()

	for _, label := range labels {
		observation.TruncateForUpdate()
		observation.AddLabels(algo.Label{
			Label:      []byte(label),
			Confidence: 1,
		})
		observation.AddContext(triePath...)

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
}

func pathFromRoot(leaf *Node) []primitive.Value {
	if leaf == nil {
		return nil
	}

	rev := make([]primitive.Value, 0, leaf.Depth+1)

	for node := leaf; node != nil; node = node.parent.Load() {
		rev = append(rev, node.value)
	}

	for left, right := 0, len(rev)-1; left < right; left, right = left+1, right-1 {
		rev[left], rev[right] = rev[right], rev[left]
	}

	return rev
}

/*
Predict runs the same algorithm stack as Load, but with an unlabeled
observation built from the trie path for value's token. Classifier
posteriors and beam continuations are merged into one Prediction for
Prompt/VM callers; other algorithms update their internal signals without
writing into that merged view.
*/
func (store *Store) Predict(value primitive.Value) *algo.Prediction {
	if store == nil || store.root == nil {
		return algo.NewPrediction()
	}

	token := value.String()

	if token == "" {
		return algo.NewPrediction()
	}

	var pathVals []primitive.Value

	store.WalkPath([]string{token}, func(node *Node) {
		pathVals = append(pathVals, node.value)
	})

	observation := algo.NewPrediction()
	observation.TruncateForUpdate()

	for _, step := range pathVals {
		observation.AddContext(step)
	}

	for _, algorithm := range store.algorithms {
		_, _ = algorithm.Update(observation)
	}

	merged := algo.NewPrediction()

	for _, algorithm := range store.algorithms {
		partial := algorithm.Value()

		if partial == nil {
			continue
		}

		switch algorithm.(type) {
		case *classify.Classifier:
			merged.Labels = append(merged.Labels, partial.Labels...)
		case *beam.Search:
			for _, cont := range partial.Continuations {
				cont.Origin = store.ID
				merged.Continuations = append(merged.Continuations, cont)
			}
		}
	}

	return merged
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
Algorithms returns the algorithm stack for external iteration.
The node-level beam uses this to send break signals directly.
*/
func (store *Store) Algorithms() []algo.Algorithm {
	return store.algorithms
}

/*
ApplyFieldPressure broadcasts field forces to all algorithms via
the standard Update path. Each algorithm decides internally how
(or whether) to respond to the pressure signals.
*/
func (store *Store) ApplyFieldPressure(
	fieldSurprisal float64,
	fieldGrowth float64,
	decayMul float64,
) {
	if store == nil {
		return
	}

	pressure := algo.NewPrediction()
	pressure.Signals[algo.FieldSurprisal] = numeric.NewDerivedFrom(fieldSurprisal)
	pressure.Signals[algo.FieldGrowth] = numeric.NewDerivedFrom(fieldGrowth)
	pressure.Signals[algo.FieldDecayMul] = numeric.NewDerivedFrom(decayMul)

	for _, algorithm := range store.algorithms {
		algorithm.Update(pressure)
	}
}
