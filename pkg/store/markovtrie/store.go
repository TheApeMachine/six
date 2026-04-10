package markovtrie

import (
	"cmp"
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/core"
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

const directSurfaceCompletionScore = 64.0

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

	merged.Continuations = append(
		merged.Continuations,
		store.partialSurfaceContinuations(token, value.String())...,
	)
	store.rescoreContinuations(merged)

	return merged.SetContinuationOrigin(store.ID), stackErr
}

func (store *Store) partialSurfaceContinuations(
	_ string,
	querySurface string,
) []algo.Continuation {
	if store == nil || store.root == nil || querySurface == "" {
		return nil
	}

	limit := store.beamLimit()
	candidates := make([]algo.Continuation, 0, limit)

	for _, rootChild := range store.root.Children() {
		store.Walk(rootChild, func(node *Node) {
			if node == nil {
				return
			}

			surface := node.value.String()
			overlap := directSurfaceOverlap(querySurface, surface)

			if overlap == 0 {
				return
			}

			suffix := surface[overlap:]
			childSurface := store.bestChildSurface(node)
			suffixes := make([]string, 0, 2)

			if suffix != "" {
				suffixes = append(suffixes, suffix)
			}

			if childSurface != "" {
				suffixes = append(suffixes, suffix+childSurface)
			}

			if len(suffixes) == 0 {
				return
			}

			for _, candidate := range suffixes {
				candidate = store.trimSurfaceContinuation(candidate)

				if candidate == "" {
					continue
				}

				visits := float64(node.TotalVisits.Load())
				score := directSurfaceCompletionScore + math.Log1p(visits)
				score += float64(overlap) / float64(max(1, core.Cfg.Value.Region.MaxTokenIngestBytes()))
				score += float64(len(candidate)) / float64(max(1, core.Cfg.Value.Region.MaxTokenIngestBytes()))

				candidates = appendSurfaceContinuation(
					candidates,
					candidate,
					score,
					store.ID,
					limit,
				)
			}
		})
	}

	return candidates
}

/*
directSurfaceOverlap returns the longest query suffix that is also a stored
surface prefix. This catches prompts whose final Value segment straddles the
ingest segment boundary.
*/
func directSurfaceOverlap(querySurface string, surface string) int {
	limit := min(len(querySurface), len(surface))

	if limit == 0 {
		return 0
	}

	minOverlap := min(6, limit)

	for overlap := limit; overlap >= minOverlap; overlap-- {
		querySuffix := querySurface[len(querySurface)-overlap:]

		if strings.TrimSpace(querySuffix) == "" {
			continue
		}

		if querySuffix == surface[:overlap] {
			return overlap
		}
	}

	return 0
}

func appendSurfaceContinuation(
	candidates []algo.Continuation,
	candidate string,
	score float64,
	origin uint64,
	limit int,
) []algo.Continuation {
	next := algo.Continuation{
		Sequence: []byte(candidate),
		Score:    score,
		Origin:   origin,
	}

	for idx := range candidates {
		if string(candidates[idx].Sequence) == candidate {
			if next.Score > candidates[idx].Score {
				candidates[idx] = next
				sortSurfaceContinuations(candidates)
			}

			return candidates
		}
	}

	if len(candidates) < limit {
		candidates = append(candidates, next)
		sortSurfaceContinuations(candidates)

		return candidates
	}

	if compareSurfaceContinuation(next, candidates[len(candidates)-1]) >= 0 {
		return candidates
	}

	candidates[len(candidates)-1] = next
	sortSurfaceContinuations(candidates)

	return candidates
}

func sortSurfaceContinuations(candidates []algo.Continuation) {
	slices.SortStableFunc(candidates, compareSurfaceContinuation)
}

func compareSurfaceContinuation(
	left algo.Continuation,
	right algo.Continuation,
) int {
	if left.Score != right.Score {
		return cmp.Compare(right.Score, left.Score)
	}

	return cmp.Compare(string(left.Sequence), string(right.Sequence))
}

func (store *Store) bestChildSurface(node *Node) string {
	if node == nil {
		return ""
	}

	children := node.Children()

	if len(children) == 0 {
		return ""
	}

	var best *Node
	bestKey := ""

	for key, child := range children {
		if child == nil {
			continue
		}

		if best == nil {
			best = child
			bestKey = key

			continue
		}

		childVisits := child.TotalVisits.Load()
		bestVisits := best.TotalVisits.Load()

		if childVisits > bestVisits || (childVisits == bestVisits && key < bestKey) {
			best = child
			bestKey = key
		}
	}

	if best == nil {
		return ""
	}

	return best.value.String()
}

func (store *Store) beamLimit() int {
	limit := core.Cfg.MarkovTrie.BeamWidth

	if limit <= 0 {
		return 3
	}

	return limit
}

/*
trimSurfaceContinuation keeps direct surface readout bounded to one
primitive.Value segment. It still bridges a short suffix into the next
segment, but it will not spill into the next dataset sample when the
target-sized span is already complete.
*/
func (store *Store) trimSurfaceContinuation(candidate string) string {
	limit := core.Cfg.Value.Region.MaxTokenIngestBytes()

	if limit > 0 && len(candidate) > limit {
		return candidate[:limit]
	}

	return candidate
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

	store.rescorePhaseContinuations(prediction)
	store.rescoreGeometricContinuations(prediction)

	slices.SortStableFunc(prediction.Continuations, func(leftCont algo.Continuation, rightCont algo.Continuation) int {
		return cmp.Compare(rightCont.Score, leftCont.Score)
	})
}

func (store *Store) rescorePhaseContinuations(prediction *algo.Prediction) {
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
}
