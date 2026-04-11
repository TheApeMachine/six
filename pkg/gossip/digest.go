package gossip

import (
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
)

// digestAffinityLanes is the fixed uint64 lane count for the Value affinity
// region (words 123–127); kept local so gossip does not depend on routing types.
const digestAffinityLanes = 5

type Digest struct {
	Origin          uint64
	Affinity        [digestAffinityLanes]uint64
	NodePhase       *geometry.Field
	SurprisalMean   float64
	SurprisalGrowth float64
	SurprisalPrev   float64
	ClassEntropy    float64
	GrowthRate      float64
	Epoch           uint64
}
