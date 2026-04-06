package markovtrie

import (
	"context"
	"sync"

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
*/
type Store struct {
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.RWMutex
	root         *Node
	nodeCount    uint64
	decayFactor  float64
	currentStep  int
	cooccurrence *cooccurrence.Matrix
	episodic     *episodic.Buffer
	classifier   *classify.Classifier
	Adaptive     *AdaptiveState
	trainer      *train.Online
	bpe          *bpe.Encoder
	beamWidth    int

	Affinity *primitive.Affinity
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
			ID:          "root",
			Children:    make(map[string]*Node),
			ClassCounts: make(map[string]float64),
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
Load a value into the trie.
*/
func (store *Store) Load(value *primitive.Value) {
	if store == nil || value == nil {
		return
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	if store.root.Value == nil {
		store.root.Value = value
		store.root.Token = value.String()

		return
	}

	store.root.Children[value.String()] = &Node{
		Value:       value,
		Token:       value.String(),
		Children:    make(map[string]*Node),
		ClassCounts: make(map[string]float64),
	}
}

/*
Insert records one labeled sequence at full learning rate.
*/
func (store *Store) Insert(sequence string, label string) {
	if store == nil {
		return
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	tokens := store.bpe.Encode(sequence)
	store.trainer.Step(label, 1, tokens, tokens)
}

/*
Flush applies pruning and rebuilds extracted pattern symbols.
*/
func (store *Store) Flush() {
	if store == nil {
		return
	}

	store.mu.Lock()
	defer store.mu.Unlock()
}

/*
ApplyFieldPressure sets external field forces that modulate adaptive behavior.
*/
func (store *Store) ApplyFieldPressure(decay, learning, prune float64) {
	if store == nil || store.Adaptive == nil {
		return
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	store.Adaptive.fieldDecayPressure = decay
	store.Adaptive.fieldLearningPressure = learning
	store.Adaptive.fieldPrunePressure = prune
}
