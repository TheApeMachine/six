package frankentrie

import (
	"math/rand"
	"strings"
)

const (
	defaultDecayFactor              = 0.995
	defaultEndToken                 = "$"
	defaultMaximumPathLength        = 5
	defaultInterpolationSuffixDepth = 4
	defaultClassificationContext    = 3
	defaultCoOccurrenceWindow       = 2
	defaultPruneInterval            = 10
	defaultPruneMinimumCount        = 0.05
	defaultReplayLength             = 10
	defaultReplayThreshold          = 85
	defaultUnknownProbability       = 0.001
	defaultAdditiveSmoothing        = 0.1
	defaultRecentPenalty            = 0.5
	defaultRecentWindow             = 3
	defaultEditDistance             = 1
	defaultEditSimilarity           = 0.95
	defaultSymbolMinimumTotal       = 2
	defaultSymbolMinimumScore       = 1.5
	defaultSymbolLimit              = 50
	defaultBaselineLearningRate     = 0.1
	defaultMaxLearningRate          = 1.0
	defaultSurprisalScaleBits       = 2.0
	defaultConceptLabelPrefix       = "Concept_"
	defaultUnsupervisedConfidence   = 50.0
	defaultExperienceEmptyLabel     = "None"
	defaultEpisodicCapacity         = 1000
	defaultEpisodicNeighborLimit    = 16
	defaultEpisodicBlendWeight      = 0.35
	defaultInitialConceptCounter    = 1
	bpeEndOfWordToken               = "</w>"
	bpePairDelimiter                = "\x00"
)

/*
Store stores labeled token sequences in a trie, applies lazy decay, and
derives interpolated next-token probabilities for classification and generation.
*/
type Store struct {
	root                     *Node
	labels                   []string
	labelSet                 map[string]struct{}
	classTotals              map[string]float64
	ClassCounts              map[string]float64
	nodeCount                uint64
	decayFactor              float64
	currentStep              int
	vocabulary               map[string]struct{}
	vocabularyOrder          []string
	coOccurrence             map[string]map[string]float64
	extractedSymbols         []ExtractedSymbol
	endToken                 string
	maximumPathLength        int
	interpolationSuffixDepth int
	classificationContext    int
	pruneInterval            int
	random                   *rand.Rand
	conceptCounter           int
	unsupervisedThreshold    float64
	bpe                      *BytePairEncoder
	episodicCapacity         int
	episodicAlpha            float64
	episodicNeighborLimit    int
	episodicBuffer           []episodicEvent
	linearInterpolation      bool
}

/*
Option configures a Store.
*/
type Option func(*Store)

/*
NewStore constructs a Store with token-level defaults.
*/
func NewStore(options ...Option) *Store {
	store := &Store{
		root: &Node{
			ID:          "root",
			Children:    make(map[string]*Node),
			ClassCounts: make(map[string]float64),
		},
		labelSet:                 make(map[string]struct{}),
		classTotals:              make(map[string]float64),
		nodeCount:                1,
		decayFactor:              defaultDecayFactor,
		vocabulary:               make(map[string]struct{}),
		coOccurrence:             make(map[string]map[string]float64),
		endToken:                 defaultEndToken,
		maximumPathLength:        defaultMaximumPathLength,
		interpolationSuffixDepth: defaultInterpolationSuffixDepth,
		classificationContext:    defaultClassificationContext,
		pruneInterval:            defaultPruneInterval,
		random:                   rand.New(rand.NewSource(1)),
		conceptCounter:           defaultInitialConceptCounter,
		unsupervisedThreshold:    defaultUnsupervisedConfidence,
		episodicCapacity:         0,
		episodicAlpha:            defaultEpisodicBlendWeight,
		episodicNeighborLimit:    defaultEpisodicNeighborLimit,
	}

	for _, option := range options {
		option(store)
	}

	if store.random == nil {
		store.random = rand.New(rand.NewSource(1))
	}

	return store
}

/*
Insert records one labeled sequence at full learning rate.
*/
func (store *Store) Insert(sequence string, label string) {
	store.Train(sequence, label, 1)
}

/*
WithDecayFactor overrides the multiplicative decay applied per train step.
*/
func WithDecayFactor(decayFactor float64) Option {
	return func(store *Store) {
		if decayFactor <= 0 || decayFactor > 1 {
			return
		}

		store.decayFactor = decayFactor
	}
}

/*
WithEndToken overrides the token appended at the end of each trained sequence.
*/
func WithEndToken(endToken string) Option {
	return func(store *Store) {
		endToken = strings.TrimSpace(endToken)
		if endToken == "" {
			return
		}

		store.endToken = endToken
	}
}

/*
WithRandomSource overrides the random source used by sampling methods.
*/
func WithRandomSource(source rand.Source) Option {
	return func(store *Store) {
		if source == nil {
			return
		}

		store.random = rand.New(source)
	}
}

/*
WithUnsupervisedConfidenceThreshold sets the minimum softmax percentage that an
unsupervised Experience call needs before it reinforces an existing label.
*/
func WithUnsupervisedConfidenceThreshold(threshold float64) Option {
	return func(store *Store) {
		if threshold <= 0 {
			return
		}

		store.unsupervisedThreshold = threshold
	}
}

/*
WithEpisodicMemory enables a rolling buffer of recent token sequences used as a
fast KNN tail for next-token interpolation. Set capacity to zero to disable.
*/
func WithEpisodicMemory(capacity int) Option {
	return func(store *Store) {
		if capacity < 0 {
			return
		}

		store.episodicCapacity = capacity
	}
}

/*
WithEpisodicBlend sets how much mass shifts from the trie toward episodic KNN
predictions when episodic memory is enabled.
*/
func WithEpisodicBlend(alpha float64) Option {
	return func(store *Store) {
		if alpha < 0 || alpha > 1 {
			return
		}

		store.episodicAlpha = alpha
	}
}

/*
WithEpisodicNeighborLimit caps how many recent episodic hits contribute per
query so that one-shots stay cheap.
*/
func WithEpisodicNeighborLimit(limit int) Option {
	return func(store *Store) {
		if limit <= 0 {
			return
		}

		store.episodicNeighborLimit = limit
	}
}

/*
WithLinearInterpolationWeights switches suffix mixing from exponential depth
weights to the linear schedule used by the reference cognitive trainer.
*/
func WithLinearInterpolationWeights() Option {
	return func(store *Store) {
		store.linearInterpolation = true
	}
}

/*
WithBytePairEncoder attaches a trained subword tokenizer ahead of trie walks.
*/
func WithBytePairEncoder(encoder *BytePairEncoder) Option {
	return func(store *Store) {
		if encoder == nil {
			return
		}

		store.bpe = encoder
	}
}
