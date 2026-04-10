package markovtrie

import (
	"math"

	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/primitive"
)

const geometricContinuationScoreGain = 1.5

func transitionMotor(
	from primitive.FrameMultivector,
	to primitive.FrameMultivector,
) primitive.FrameMultivector {
	if from.IsZero() || to.IsZero() {
		return primitive.FrameMultivector{}
	}

	fromMv := geometry.Multivector(from).Normalize()
	toMv := geometry.Multivector(to).Normalize()
	motor := toMv.GeometricProduct(fromMv.Reverse()).Normalize()

	return primitive.FrameMultivector(motor)
}

func (store *Store) rescoreGeometricContinuations(prediction *algo.Prediction) {
	if store == nil || prediction == nil || len(prediction.Context) == 0 {
		return
	}

	current := prediction.Context[len(prediction.Context)-1].ContextMultivector()
	attractor := meanContextMultivector(prediction.Context)

	if current.IsZero() || attractor.IsZero() {
		return
	}

	for continuationIndex := range prediction.Continuations {
		candidate := &prediction.Continuations[continuationIndex]
		candidateMv := primitive.NewFrameMultivector(candidate.Sequence)
		boost := geometricContinuationBoost(current, candidateMv, attractor)

		if boost <= 0 {
			continue
		}

		candidate.Score += math.Log1p(boost)
	}
}

func meanContextMultivector(context []primitive.Value) primitive.FrameMultivector {
	var sum geometry.Multivector
	var count float64

	for valueIndex := range context {
		mv := context[valueIndex].ContextMultivector()

		if mv.IsZero() {
			continue
		}

		geom := geometry.Multivector(mv).Normalize()

		for lane := range primitive.RegionWords {
			sum[lane] += geom[lane]
		}

		count++
	}

	if count == 0 {
		return primitive.FrameMultivector{}
	}

	inv := 1.0 / count

	for lane := range primitive.RegionWords {
		sum[lane] *= inv
	}

	return primitive.FrameMultivector(sum.Normalize())
}

func geometricContinuationBoost(
	currentFrame primitive.FrameMultivector,
	candidateFrame primitive.FrameMultivector,
	attractorFrame primitive.FrameMultivector,
) float64 {
	if currentFrame.IsZero() || candidateFrame.IsZero() || attractorFrame.IsZero() {
		return 0
	}

	current := geometry.Multivector(currentFrame).Normalize()
	candidate := geometry.Multivector(candidateFrame).Normalize()
	attractor := geometry.Multivector(attractorFrame).Normalize()
	motor := candidate.GeometricProduct(current.Reverse()).Normalize()
	steered := motor.Sandwich(current).Normalize()
	alignment := (multivectorCosine(steered, attractor) + multivectorCosine(candidate, attractor)) * 0.5

	if alignment <= 0 {
		return 0
	}

	return alignment * geometricContinuationScoreGain
}

func frameMultivectorCosine(
	left primitive.FrameMultivector,
	right primitive.FrameMultivector,
) float64 {
	return multivectorCosine(
		geometry.Multivector(left),
		geometry.Multivector(right),
	)
}

func multivectorCosine(left geometry.Multivector, right geometry.Multivector) float64 {
	var dot, leftNorm, rightNorm float64

	for lane := range primitive.RegionWords {
		dot += left[lane] * right[lane]
		leftNorm += left[lane] * left[lane]
		rightNorm += right[lane] * right[lane]
	}

	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}

	similarity := dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))

	if similarity < -1 {
		return -1
	}

	if similarity > 1 {
		return 1
	}

	return similarity
}
