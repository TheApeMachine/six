package markovtrie

import (
	"context"
	"errors"
	"log"
	"sync"
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

	/*
		seqPath holds root-to-current-leaf context for sequential Load chains
		(labels empty, lastLeaf threading). Reset when labels force a branch
		from root so parent-pointer walks are not repeated for every token in
		a linear ingest.

		seqPathMu serializes seqPath mutations and reads so concurrent Load
		calls cannot corrupt the shared backing array.
	*/
	seqPathMu sync.Mutex
	seqPath   []primitive.Value
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
Load inserts a value into the trie. Edge keys use the UTF-8 token
region (same bytes as String) via trieEdgeKey so trie structure stays
aligned without an extra allocation per hop. If a path already
exists, the value lands at the existing branch; otherwise a new
child is created.

When labels are non-empty, algorithm updates may fail; failures are
joined and returned while trie structure and visits are already updated.
*/
func (store *Store) Load(
	value primitive.Value,
	labels ...string,
) error {
	if store == nil {
		return nil
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

	token := trieEdgeKey(value)
	existing := parent.Child(token)

	if existing != nil {
		existing.TotalVisits.Add(1)
		// Parent link was fixed when the child was first inserted.
	} else {
		child := NewNode(value)
		parent.storeChild(value, child)
		existing = child
	}

	chainParent := store.lastLeaf.Load()

	store.seqPathMu.Lock()

	if len(labels) > 0 {
		store.seqPath = append(store.seqPath[:0], store.root.value, existing.value)
		store.lastLeaf.Store(nil)
	} else {
		if chainParent == nil {
			store.seqPath = append(store.seqPath[:0], store.root.value, existing.value)
		} else {
			store.seqPath = append(store.seqPath, existing.value)
		}

		store.lastLeaf.Store(existing)
	}

	triePath := append([]primitive.Value(nil), store.seqPath...)

	store.seqPathMu.Unlock()

	observation := algo.NewPrediction()

	var loadErr error

	for _, label := range labels {
		observation.TruncateForUpdate()
		observation.AddLabels(algo.Label{
			Label:      []byte(label),
			Confidence: 1,
		})
		observation.AddContext(triePath...)

		if err := store.runAlgorithmStack(observation); err != nil {
			loadErr = errors.Join(loadErr, err)
		}
	}

	return loadErr
}

/*
runAlgorithmStack runs the Store's algo.Algorithm slice on one observation.

Trie mutation stays in Load/Predict; this is the only orchestration hook
needed to broadcast a labeled observation without scattering Update loops.
*/
func (store *Store) runAlgorithmStack(observation *algo.Prediction) error {
	if store == nil {
		return nil
	}

	var joined error

	for _, algorithm := range store.algorithms {
		_, err := algorithm.Update(observation)

		if err != nil {
			log.Printf(
				"markovtrie.Store.runAlgorithmStack: algorithm %T failed: %v",
				algorithm,
				err,
			)

			joined = errors.Join(joined, err)
		}
	}

	return joined
}

/*
Predict runs the same algorithm stack as Load, but with an unlabeled
observation built from the trie path for value's token. Classifier
posteriors and beam continuations are merged into one Prediction for
Prompt/VM callers; other algorithms update their internal signals without
writing into that merged view.
*/
func (store *Store) Predict(value primitive.Value) (*algo.Prediction, error) {
	if store == nil || store.root == nil {
		return algo.NewPrediction(), nil
	}

	token := trieEdgeKey(value)

	if token == "" {
		return algo.NewPrediction(), nil
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

	stackErr := store.runAlgorithmStack(observation)

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

	return merged, stackErr
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
(or whether) to respond to the pressure signals. Non-nil return means
one or more Update calls failed (errors joined).
*/
func (store *Store) ApplyFieldPressure(
	fieldSurprisal float64,
	fieldGrowth float64,
	decayMul float64,
) error {
	if store == nil {
		return nil
	}

	pressure := algo.NewPrediction()
	pressure.Signals[algo.FieldSurprisal] = numeric.NewDerivedFrom(fieldSurprisal)
	pressure.Signals[algo.FieldGrowth] = numeric.NewDerivedFrom(fieldGrowth)
	pressure.Signals[algo.FieldDecayMul] = numeric.NewDerivedFrom(decayMul)

	return store.runAlgorithmStack(pressure)
}
