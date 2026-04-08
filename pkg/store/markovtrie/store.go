package markovtrie

import (
	"cmp"
	"context"
	"errors"
	"math"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/algo/beam"
	"github.com/theapemachine/six/pkg/core/algo/causal"
	"github.com/theapemachine/six/pkg/core/algo/classify"
	"github.com/theapemachine/six/pkg/core/algo/train"
	"github.com/theapemachine/six/pkg/core/numeric"
	"github.com/theapemachine/six/pkg/core/numeric/gf"
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
	stack         *algo.Stack
	lastLeaf      atomic.Pointer[Node]
	localPhase    atomic.Pointer[gf.Vector257]

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
		ctx:      ctx,
		cancel:   cancel,
		Affinity: affinity,
		stack: algo.NewStack(
			classify.NewClassifier(),
			beam.NewSearch(),
			train.NewOnline(),
			causal.NewGraph(),
		),
	}

	for _, option := range options {
		option(store)
	}

	store.localPhase.Store(gf.NewVector257())

	return store, validate.Require(map[string]any{
		"ctx":    store.ctx,
		"cancel": store.cancel,
	})
}

/*
WithAlgorithms replaces the Store's algorithm stack. The caller owns the
algorithm construction; the Store owns orchestration only through algo.Stack.
*/
func WithAlgorithms(algorithms ...algo.Algorithm) Option {
	return func(store *Store) {
		store.stack = algo.NewStack(algorithms...)
	}
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
	store.observeLocalPhase(value)

	observation := algo.NewPrediction()

	var loadErr error

	for _, label := range labels {
		observation.TruncateForUpdate()
		observation.AddTargets(algo.Label{
			Label:      []byte(label),
			Confidence: 1,
		})
		observation.AddContext(triePath...)

		if err := store.Observe(observation); err != nil {
			loadErr = errors.Join(loadErr, err)
		}
	}

	return loadErr
}

/*
Observe broadcasts one Prediction envelope through the Store's algorithm
stack. Callers use this for learning, inference, field pressure, and beam
feedback without iterating concrete algorithms in store or node code.
*/
func (store *Store) Observe(observation *algo.Prediction) error {
	if store == nil || store.stack == nil {
		return nil
	}

	_, err := store.stack.Update(observation)

	return err
}

/*
Predict runs the same algorithm stack as Load, but with an unlabeled
observation built from the trie path for value's token. Classifier
posteriors, beam continuations, and derived signals are merged into one
Prediction for Prompt/VM callers by the shared algo.Stack.
*/
func (store *Store) Predict(value primitive.Value) (*algo.Prediction, error) {
	if store == nil || store.root == nil || store.stack == nil {
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

	merged, stackErr := store.stack.Update(observation)

	if merged == nil {
		return algo.NewPrediction(), stackErr
	}

	store.rescoreContinuations(merged)

	return merged.SetContinuationOrigin(store.ID), stackErr
}

/*
Signals returns the merged signal map from all algorithms on this
Store. Each algorithm populates the keys it owns on its Prediction;
this method flattens them into a single view. Duplicate keys are
last-writer-wins in algorithm order — the caller should not depend
on which algorithm "wins" a shared key.
*/
func (store *Store) Signals() map[algo.SignalType]float64 {
	if store == nil || store.stack == nil {
		return make(map[algo.SignalType]float64)
	}

	return store.stack.Signals()
}

/*
Signal returns a single signal value from the algorithm that owns it.
Returns 0 if no algorithm exposes the key.
*/
func (store *Store) Signal(signal algo.SignalType) float64 {
	if store == nil || store.stack == nil {
		return 0
	}

	return store.stack.Signal(signal)
}

/*
Algorithms returns the algorithm stack for external iteration.
Used for tests and diagnostics without exposing Stack internals.
*/
func (store *Store) Algorithms() []algo.Algorithm {
	if store == nil || store.stack == nil {
		return nil
	}

	return store.stack.Algorithms()
}

/*
LocalPhase returns a trie-local GF(257) phase snapshot.
*/
func (store *Store) LocalPhase() gf.Vector257 {
	if store == nil {
		return gf.Vector257{}
	}

	phaseSnapshot := store.localPhase.Load()

	if phaseSnapshot == nil {
		return gf.Vector257{}
	}

	return *phaseSnapshot
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
	globalPhase float64,
	phaseConcentration float64,
) error {
	if store == nil {
		return nil
	}

	store.rotateLocalPhase(globalPhase, phaseConcentration)

	pressure := algo.NewPrediction()
	pressure.Signals[algo.FieldSurprisal] = numeric.NewDerivedFrom(fieldSurprisal)
	pressure.Signals[algo.FieldGrowth] = numeric.NewDerivedFrom(fieldGrowth)
	pressure.Signals[algo.FieldDecayMul] = numeric.NewDerivedFrom(decayMul)
	pressure.Signals[algo.GlobalPhase] = numeric.NewDerivedFrom(globalPhase)
	pressure.Signals[algo.PhaseConcentration] = numeric.NewDerivedFrom(phaseConcentration)

	return store.Observe(pressure)
}

func (store *Store) observeLocalPhase(value primitive.Value) {
	if store == nil {
		return
	}

	phaseBytes := value.TokenRegionBytes()

	if len(phaseBytes) == 0 {
		return
	}

	store.updateLocalPhase(func(phaseVector *gf.Vector257) {
		phaseVector.ObserveBytes(phaseBytes)
	})
}

func (store *Store) rotateLocalPhase(globalPhase float64, phaseConcentration float64) {
	if store == nil || phaseConcentration <= 0 {
		return
	}

	phaseIndex := int(math.Round(globalPhase))

	if phaseIndex < 0 {
		return
	}

	phaseIndex %= gf.PhaseWidth

	multiplier := gf.Reduce257(uint32(phaseIndex + 1))

	if multiplier == 0 {
		multiplier = 1
	}

	bias := gf.Reduce257(uint32(math.Round(phaseConcentration * float64(gf.Mod257-1))))

	store.updateLocalPhase(func(phaseVector *gf.Vector257) {
		phaseVector.Rotate(multiplier, bias)
	})
}

func (store *Store) updateLocalPhase(updateFn func(*gf.Vector257)) {
	if store == nil || updateFn == nil {
		return
	}

	for {
		currentPhase := store.localPhase.Load()
		nextPhase := gf.NewVector257()

		if currentPhase != nil {
			*nextPhase = *currentPhase
		}

		updateFn(nextPhase)

		if store.localPhase.CompareAndSwap(currentPhase, nextPhase) {
			return
		}
	}
}

func (store *Store) rescoreContinuations(prediction *algo.Prediction) {
	if store == nil || prediction == nil || len(prediction.Continuations) == 0 {
		return
	}

	localPhase := store.LocalPhase()
	localMode := localPhase.Dominant()

	if localMode.Index < 0 {
		return
	}

	for continuationIndex := range prediction.Continuations {
		candidate := &prediction.Continuations[continuationIndex]
		candidatePhase := gf.LiftBytes(candidate.Sequence)
		candidateMode := candidatePhase.Dominant()
		alignment := gf.Alignment(localMode.Index, candidateMode.Index)
		interference := localPhase.Dot(candidatePhase)
		gain := math.Max(localMode.Concentration, gf.Gain257(interference))
		bias := gf.InterferenceMultiplier(alignment, gain)

		candidate.Score += math.Log(bias)
	}

	slices.SortStableFunc(prediction.Continuations, func(leftCont algo.Continuation, rightCont algo.Continuation) int {
		return cmp.Compare(rightCont.Score, leftCont.Score)
	})
}
