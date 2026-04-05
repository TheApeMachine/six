package markovtrie

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
	defaultEpisodicRecencyWeight    = 0.25
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
	root     *Node
	labels   []string
	labelSet map[string]struct{}
	// ClassTotals holds decayed cumulative training weight per label (global marginal).
	ClassTotals              map[string]float64
	nodeCount                uint64
	decayFactor              float64
	currentStep              int
	vocabulary               map[string]struct{}
	vocabularyOrder          []string
	coOccurrence             map[string]map[string]float64
	extractedSymbols         []ExtractedSymbol
	patternsDirty            bool
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
	episodicRecencyWeight    float64
	episodicBuffer           []episodicEvent
	linearInterpolation      bool
	wordTokensOnly           bool
	generationTokenJoiner    string
	episodicDecayGamma       float64
	episodicSequenceCounter  uint64
	adaptive                 *adaptiveState
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
		ClassTotals:              make(map[string]float64),
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
		episodicRecencyWeight:    defaultEpisodicRecencyWeight,
		adaptive:                 newAdaptiveState(),
	}

	for _, option := range options {
		option(store)
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
Flush applies pruning and rebuilds extracted pattern symbols. Training only
refreshes patterns on pruneInterval boundaries; batch callers should call Flush
when a sequence of Train steps finishes.
*/
func (store *Store) Flush() {
	store.applyPrune()
	store.rebuildExtractedPatterns()
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
WithEpisodicRecencyWeight scales how much newer episodic buffer rows inflate
next-token counts (0 disables the bias).
*/
func WithEpisodicRecencyWeight(weight float64) Option {
	return func(store *Store) {
		if weight < 0 {
			return
		}

		store.episodicRecencyWeight = weight
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
WithWordTokensOnly switches Tokenize (when no BPE is attached) to underscore and
whitespace word boundaries only, matching the browser CognitiveModel demo
tokenizer (split on [_ ]+ with no standalone separator tokens). Enable when
training data matches underscore-separated words rather than trie paths that
literalize "_" as its own symbol.
*/
func WithWordTokensOnly() Option {
	return func(store *Store) {
		store.wordTokensOnly = true
		store.generationTokenJoiner = "_"
	}
}

/*
WithGenerationTokenSeparator sets the string used between newly sampled tokens
in Generate and BeamSearch output (default "" for legacy trie tokens that may
include "_" as a child; "_" matches the demo dream / beam display).
*/
func WithGenerationTokenSeparator(separator string) Option {
	return func(store *Store) {
		store.generationTokenJoiner = separator
	}
}

/*
WithEpisodicRecencyGamma applies exponential decay by match order toward older
episodic hits (e.g. 0.9 matches the demo Math.pow(0.9, i) bias toward fresher
rows). When gamma is 0, only the linear recency bias from WithEpisodicRecencyWeight applies.
*/
func WithEpisodicRecencyGamma(gamma float64) Option {
	return func(store *Store) {
		if gamma < 0 || gamma >= 1 {
			return
		}

		store.episodicDecayGamma = gamma
	}
}

/*
WithAdaptive enables or disables adaptive self-tuning. When disabled, all
parameters use their configured or default fixed values.
*/
func WithAdaptive(enabled bool) Option {
	return func(store *Store) {
		store.adaptive.enabled = enabled
	}
}

/*
ApplyFieldPressure sets external field forces that modulate the trie's
adaptive behavior. The trie does not decide to look at these — they act
on it directly, like a physical field acts on a particle.

decay: positive = forget faster (volatile neighborhood), negative = retain more (stable).
learning: positive = learn more aggressively (field is novel nearby).
prune: positive = prune harder (field is growing fast nearby).
*/
func (store *Store) ApplyFieldPressure(decay, learning, prune float64) {
	if store == nil || store.adaptive == nil {
		return
	}

	store.adaptive.fieldDecayPressure = decay
	store.adaptive.fieldLearningPressure = learning
	store.adaptive.fieldPrunePressure = prune
}

/*
AdaptiveSignals holds the trie's current adaptive state summary, exported
for consumption by higher layers (e.g. Kadabra field gossip).
*/
type AdaptiveSignals struct {
	SurprisalMean   float64
	SurprisalVar    float64
	ClassEntropy    float64
	GrowthRate      float64
	EffectiveDepth  float64
	EpisodicQuality float64
}

/*
AdaptiveDigest returns a snapshot of the trie's adaptive signals.
*/
func (store *Store) AdaptiveDigest() AdaptiveSignals {
	if store == nil || store.adaptive == nil || !store.adaptive.enabled {
		return AdaptiveSignals{}
	}

	a := store.adaptive

	// Effective depth: weighted average of depth indices by hit rate.
	var depthSum, depthWeight float64
	if a.depthTotal >= adaptiveMinSamples {
		for i := range a.depthHits {
			rate := a.depthHits[i] / a.depthTotal
			depthSum += float64(i) * rate
			depthWeight += rate
		}
	}

	effectiveDepth := 0.0
	if depthWeight > 0 {
		effectiveDepth = depthSum / depthWeight
	}

	return AdaptiveSignals{
		SurprisalMean:   a.surprisalEMA,
		SurprisalVar:    a.surprisalVar,
		ClassEntropy:    a.entropyEMA,
		GrowthRate:      a.growthRateEMA,
		EffectiveDepth:  effectiveDepth,
		EpisodicQuality: a.episodicQualityEMA,
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
