package markovtrie

import (
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
domainAlignment is an immutable Procrustes rotation between two Value
manifolds. It is rebuilt off the observation path and published atomically, so
policy inference can project sensory coordinates without locks.
*/
type domainAlignment struct {
	rotation [primitive.RegionWords][primitive.RegionWords]float64
	residual float64
	samples  int
}

func newDomainAlignment(stats map[string]coactivationStat) *domainAlignment {
	if len(stats) < primitive.RegionWords {
		return nil
	}

	matA := make([][]float64, 0, len(stats))
	matB := make([][]float64, 0, len(stats))

	for _, stat := range stats {
		if stat.Count <= 0 || stat.SensoryVector.IsZero() || stat.ActionVector.IsZero() {
			continue
		}

		matA = append(matA, frameMultivectorRow(stat.SensoryVector))
		matB = append(matB, frameMultivectorRow(stat.ActionVector))
	}

	if len(matA) < primitive.RegionWords {
		return nil
	}

	result, err := geometry.Procrustes(
		matA,
		matB,
		len(matA),
		primitive.RegionWords,
	)

	if err != nil || result == nil {
		return nil
	}

	alignment := &domainAlignment{
		residual: result.Residual,
		samples:  len(matA),
	}

	for row := range primitive.RegionWords {
		for col := range primitive.RegionWords {
			alignment.rotation[row][col] = result.R[row][col]
		}
	}

	return alignment
}

func (alignment *domainAlignment) Project(
	vector primitive.FrameMultivector,
) primitive.FrameMultivector {
	if alignment == nil || vector.IsZero() {
		return primitive.FrameMultivector{}
	}

	var out primitive.FrameMultivector

	for row := range primitive.RegionWords {
		for col := range primitive.RegionWords {
			out[row] += alignment.rotation[row][col] * vector[col]
		}
	}

	return out.Normalize()
}

func (coordinator *MultimodalCoordinator) projectSensoryAction(
	vector primitive.FrameMultivector,
) (primitive.FrameMultivector, bool) {
	if coordinator == nil || vector.IsZero() {
		return primitive.FrameMultivector{}, false
	}

	alignment := coordinator.actionAlign.Load()

	if alignment == nil {
		return primitive.FrameMultivector{}, false
	}

	projected := alignment.Project(vector)

	return projected, !projected.IsZero()
}

func frameMultivectorRow(vector primitive.FrameMultivector) []float64 {
	row := make([]float64, primitive.RegionWords)

	for idx := range primitive.RegionWords {
		row[idx] = vector[idx]
	}

	return row
}
