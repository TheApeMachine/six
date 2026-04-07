package markovtrie

import (
	"sync/atomic"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/numeric"
	"github.com/theapemachine/six/pkg/core/numeric/adaptive"
)

/*
AdaptiveState tracks online statistics for self-tuned trie behavior.

Field pressure scalars are stored as atomic float bit patterns so routing
threads can publish pressure without contending on a trie-wide mutex.
*/
type AdaptiveState struct {
	DepthHits             []float64
	DepthTotal            float64
	DepthDecay            float64
	DepthWeights          []float64
	SurprisalStats        *numeric.Derived
	EntropySmooth         *numeric.Derived
	EntropyNormSmooth     *numeric.Derived
	LastPosteriorLabels   int
	EpisodicQualitySmooth *numeric.Derived
	ClassifyAccuracy      *numeric.Derived
	LastNodeCount         uint64
	LastPruneStep         int
	GrowthRateSmooth      *numeric.Derived
	PruneThreshold        float64
	fieldDecayPressure    atomic.Uint64
	fieldLearningPressure atomic.Uint64
	fieldPrunePressure    atomic.Uint64

	enabled bool
}

/*
NewAdaptiveState allocates adaptive trackers.
*/
func NewAdaptiveState() *AdaptiveState {
	maxDepth := core.Cfg.MarkovTrie.AdaptiveMaxDepth

	if maxDepth < 1 {
		maxDepth = 1
	}

	return &AdaptiveState{
		DepthHits:    make([]float64, maxDepth),
		DepthTotal:   0,
		DepthDecay:   core.Cfg.MarkovTrie.AdaptiveMaxDepthDecay,
		DepthWeights: make([]float64, maxDepth),
		SurprisalStats: numeric.NewDerived(
			numeric.WithDynamics(
				adaptive.NewEMA(),
			),
		),
		EntropySmooth: numeric.NewDerived(
			numeric.WithDynamics(
				adaptive.NewEMA(),
			),
		),
		EntropyNormSmooth: numeric.NewDerived(
			numeric.WithDynamics(
				adaptive.NewEMA(),
			),
		),
		EpisodicQualitySmooth: numeric.NewDerived(
			numeric.WithDynamics(
				adaptive.NewEMA(),
			),
		),
		ClassifyAccuracy: numeric.NewDerived(
			numeric.WithDynamics(
				adaptive.NewEMA(),
			),
		),
		GrowthRateSmooth: numeric.NewDerived(
			numeric.WithDynamics(
				adaptive.NewEMA(),
			),
		),
		enabled:        true,
		PruneThreshold: core.Cfg.MarkovTrie.PruneMinimumCount,
	}
}
