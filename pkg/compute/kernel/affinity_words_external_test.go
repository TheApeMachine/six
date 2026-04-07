package kernel_test

import (
	"testing"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
TestAffinityWordsPerCandidateMatchesPrimitive catches silent drift between
the kernel layout constant and primitive.AffinityWords without introducing
an import cycle inside package kernel.
*/
func TestAffinityWordsPerCandidateMatchesPrimitive(t *testing.T) {
	t.Parallel()

	if kernel.AffinityWordsPerCandidate != primitive.AffinityWords {
		t.Fatalf(
			"kernel.AffinityWordsPerCandidate=%d primitive.AffinityWords=%d — update constants together",
			kernel.AffinityWordsPerCandidate,
			primitive.AffinityWords,
		)
	}
}
