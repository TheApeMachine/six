package cpu

import "github.com/theapemachine/six/pkg/primitive"

const affinityDistanceVectorWords = 8

/*
AffinityDistances computes Hamming distance from one 5-word affinity query to
every candidate row. Candidates are laid out in the same packed row-major form
the routing table keeps in mesh.Field.fingers, so CPU SIMD and GPU kernels can
consume the same ordering.
*/
func AffinityDistances(
	query *[primitive.AffinityWords]uint64,
	candidates [][primitive.AffinityWords]uint64,
) []uint32 {
	if query == nil || len(candidates) == 0 {
		return nil
	}

	var packedQuery [affinityDistanceVectorWords]uint64
	copy(packedQuery[:], query[:])

	packedCandidates := make([]uint64, len(candidates)*affinityDistanceVectorWords)
	for idx, candidate := range candidates {
		base := idx * affinityDistanceVectorWords
		copy(packedCandidates[base:base+primitive.AffinityWords], candidate[:])
	}

	out := make([]uint32, len(candidates))
	batchAffinityDistances(&packedQuery[0], &packedCandidates[0], len(candidates), &out[0])

	return out
}
