package kadabra

import (
	"github.com/theapemachine/six/pkg/core/numeric"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Digest captures a snapshot of a trie cluster's adaptive state for
gossip propagation. Each trie emits one digest per epoch; the field
uses these to detect eigenmodes and project attention pressure.
*/
type Digest struct {
	Origin          uint64
	Affinity        [primitive.AffinityWords]uint64
	SurprisalMean   float64
	SurprisalGrowth float64
	SurprisalPrev   float64
	ClassEntropy    float64
	GrowthRate      float64
	Epoch           uint64
}

/*
Couple scores how strongly two digests' surprisal velocities line up.
The product va*vb is divided by the square of the geometric mean of the
two magnitudes, giving equal log-scale weight to both — so a pair
with velocities (0.1, 10.0) moving in the same direction still produces
strong positive coupling.

magEps (0.01) prevents division by zero and suppresses spurious phase
coupling for quiescent nodes whose velocity magnitude is below the
measurement noise floor of the surprisal EMA.
*/
func (digest *Digest) Couple(other Digest) float64 {
	return numeric.SurprisalVelocityCouple(
		digest.SurprisalGrowth, other.SurprisalGrowth,
	)
}
