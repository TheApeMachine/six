package kernel_test

import (
	"testing"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
TestAffinityWordsPerCandidateFitsPrimitive verifies that the SIMD-padded
candidate width is at least as wide as the actual affinity vector. The
kernel processes AffinityWordsPerCandidate uint64s per candidate; the
extra words beyond primitive.AffinityWords are zero-padded and contribute
nothing to Hamming distance.
*/
func TestAffinityWordsPerCandidateFitsPrimitive(t *testing.T) {
	t.Parallel()

	if kernel.AffinityWordsPerCandidate < primitive.AffinityWords {
		t.Fatalf(
			"kernel.AffinityWordsPerCandidate=%d < primitive.AffinityWords=%d — kernel cannot hold full affinity",
			kernel.AffinityWordsPerCandidate,
			primitive.AffinityWords,
		)
	}
}
