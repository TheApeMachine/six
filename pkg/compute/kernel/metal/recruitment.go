package metal

import (
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/primitive"
)

func BatchRecruitmentSaturation(
	folds [][primitive.AffinityWords]uint64,
	candidates [][primitive.AffinityWords]uint64,
	limit float64,
) []bool {
	return kernel.BatchRecruitmentSaturation(folds, candidates, limit)
}
