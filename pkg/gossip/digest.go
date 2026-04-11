package gossip

import (
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/primitive"
)

type Digest struct {
	Origin          uint64
	Affinity        [primitive.AffinityWords]uint64
	NodePhase       *geometry.Field
	SurprisalMean   float64
	SurprisalGrowth float64
	SurprisalPrev   float64
	ClassEntropy    float64
	GrowthRate      float64
	Epoch           uint64
}
