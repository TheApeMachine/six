package markovtrie

import (
	"context"
	"math"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/algo/bpe"
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
	ctx          context.Context
	cancel       context.CancelFunc
	root         *Node
	nodeCount    atomic.Uint64
	decayFactor  float64
	currentStep  atomic.Int32
	cooccurrence *cooccurrence.Matrix
	episodic     *episodic.Buffer
	classifier   *classify.Classifier
	Adaptive     *AdaptiveState
	trainer      *train.Online
	bpe          *bpe.Encoder
	beamWidth    int

	Affinity      *primitive.Affinity
	AffinityCount atomic.Uint64
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
		root: &Node{
			ID: "root",
		},
		cooccurrence: cooccurrence.NewMatrix(core.Cfg.MarkovTrie.CoOccurrenceWindow),
		classifier:   &classify.Classifier{},
		Adaptive:     NewAdaptiveState(),
		Affinity:     primitive.NewAffinity(),
	}

	for _, option := range options {
		option(store)
	}

	if store.decayFactor == 0 {
		store.decayFactor = core.Cfg.MarkovTrie.DecayFactor
	}

	if store.trainer == nil {
		store.trainer = train.NewOnline(store.decayFactor, store.cooccurrence)
	}

	if store.bpe == nil {
		encoder := bpe.NewEncoder()
		encoder.EndToken = core.Cfg.MarkovTrie.EndToken
		store.bpe = encoder
	}

	if store.beamWidth <= 0 {
		store.beamWidth = core.Cfg.MarkovTrie.BeamWidth

		if store.beamWidth <= 0 {
			store.beamWidth = 3
		}
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
func (store *Store) Load(value primitive.Value) {
	if store == nil {
		return
	}

	var zero primitive.Value

	if store.root.Value == zero {
		store.root.Value = value
		store.root.Token = value.String()

		return
	}

	store.root.storeChild(value.String(), &Node{
		ID:    value.String(),
		Value: value,
		Token: value.String(),
	})
}

/*
Insert records one labeled sequence at full learning rate. The sequence
text is taken from value.String() for BPE tokenization.
*/
func (store *Store) Insert(value primitive.Value, label string) {
	if store == nil {
		return
	}

	sequence := value.String()
	tokens := store.bpe.Encode(sequence)

	store.trainer.Step(label, 1, tokens, tokens)
	store.currentStep.Store(int32(store.trainer.CurrentStep))
}

/*
Flush applies pruning and rebuilds extracted pattern symbols.
*/
func (store *Store) Flush() {
	_ = store
}

/*
ApplyFieldPressure sets external field forces that modulate adaptive behavior.
*/
func (store *Store) ApplyFieldPressure(decay, learning, prune float64) {
	if store == nil || store.Adaptive == nil {
		return
	}

	store.Adaptive.fieldDecayPressure.Store(math.Float64bits(decay))
	store.Adaptive.fieldLearningPressure.Store(math.Float64bits(learning))
	store.Adaptive.fieldPrunePressure.Store(math.Float64bits(prune))
}
