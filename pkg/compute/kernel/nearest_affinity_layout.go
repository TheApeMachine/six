package kernel

/*
Nearest-affinity batch kernels (CPU NEON, CUDA, Metal) consume query
words at the Value head and a contiguous table of candidate vectors
starting at NearestAffinityCandidatesStartWord (after the meta block).
Word 124 holds the candidate count.

AffinityWordsPerCandidate is the SIMD-padded width: candidates are
stored as 8 uint64s even though primitive.AffinityWords is 5 (257 bits).
The 3 trailing zero words contribute nothing to Hamming distance but
keep the layout 64-byte aligned for AVX2 / NEON loads.

This package must not import pkg/primitive: primitive tests import
kernel/cpu, which imports kernel, which would create an import cycle.
Drift is guarded by package kernel_test (see affinity_words_external_test.go).
*/
const (
	ProgramStartWord                   = 16
	SignalsStartWord                   = 24
	NearestAffinityCandidatesStartWord = 56
	NearestAffinityBatchWord           = 124
	AffinityWordsPerCandidate          = 8
)

/*
MaxNearestAffinityCandidates is how many 8-word padded affinity rows
fit between the candidates start and the batch count word.
*/
const MaxNearestAffinityCandidates = (NearestAffinityBatchWord - NearestAffinityCandidatesStartWord) / AffinityWordsPerCandidate
