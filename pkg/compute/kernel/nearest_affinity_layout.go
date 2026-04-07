package kernel

/*
Nearest-affinity batch kernels (CPU NEON, CUDA, Metal) consume query
words at the Value head and a contiguous table of AFFINITY_WORDS-wide
candidate vectors starting at word 32. Word 124 holds the candidate
count. Data must not overlap word 124; words 120–121 are reserved for
frame metadata (correlation / residency).

affinityWordsPerCandidate is fixed at 8 and must stay aligned with
pkg/primitive.AffinityWords and AFFINITY_WORDS in compute/kernel/shared/primitives.h.
This package must not import pkg/primitive: primitive tests import
kernel/cpu, which imports kernel, which would create an import cycle.
*/
const (
	NearestAffinityCandidatesStartWord = 32
	NearestAffinityBatchWord           = 124
	affinityWordsPerCandidate          = 8
)

// MaxNearestAffinityCandidates is how many 8-word affinity rows fit below word 124.
const MaxNearestAffinityCandidates = (NearestAffinityBatchWord - NearestAffinityCandidatesStartWord) / affinityWordsPerCandidate
