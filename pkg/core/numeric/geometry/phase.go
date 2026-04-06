package geometry

import "math"

/*
Phase provides phase-space coupling and velocity computations for
surprisal dynamics. Two nodes whose surprisal velocities move in the
same direction are phase-coupled; the strength of that coupling
determines whether the field treats them as part of the same
emergent eigenmode.
*/
type Phase struct{}

/*
NewPhase constructs a Phase instance.
*/
func NewPhase() *Phase {
	return &Phase{}
}

/*
Coupling scores how strongly two surprisal velocities align.
The product is divided by the square of the geometric mean of the
two magnitudes, giving equal log-scale weight to both — so a pair
with velocities (0.1, 10.0) moving in the same direction still
produces strong positive coupling, whereas max-magnitude would
yield near-zero.

magEps (0.01) prevents division by zero and suppresses spurious
coupling for quiescent nodes whose velocity magnitude is below
the measurement noise floor of the surprisal EMA.
*/
func (phase *Phase) Coupling(leftGrowth float64, rightGrowth float64) float64 {
	const magEps = 0.01

	geometricMean := math.Sqrt(math.Abs(leftGrowth) * math.Abs(rightGrowth))

	if geometricMean < magEps {
		return 0
	}

	return (leftGrowth * rightGrowth) / (geometricMean * geometricMean)
}

/*
Velocity returns the phase velocity of a node given its current and
previous surprisal means. Positive velocity means surprisal is
increasing (the node is encountering more novel input); negative
means it is converging.
*/
func (phase *Phase) Velocity(surprisalMean float64, surprisalPrev float64) float64 {
	return surprisalMean - surprisalPrev
}
