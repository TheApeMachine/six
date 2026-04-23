package cpu

import "github.com/theapemachine/six/pkg/primitive"

const affinityDistanceVectorWords = 8

/*
PerCandidateAffinityDistances returns the Hamming distance from query to each
packed candidate row. Layout matches the mesh routing table
(mesh.Field.fingers) and the GPU NearestAffinity buffers.
*/
func PerCandidateAffinityDistances(
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

/*
AffinityDistances computes Hamming distance from one 5-word affinity query to
every candidate row. Candidates are laid out in the same packed row-major form
the routing table keeps in mesh.Field.fingers, so CPU SIMD and GPU kernels can
consume the same ordering. Returns the best match packed as (131072-dist)<<32 | id.
*/
func AffinityDistances(
	query *[primitive.AffinityWords]uint64,
	candidates [][primitive.AffinityWords]uint64,
) uint64 {
	out := PerCandidateAffinityDistances(query, candidates)
	if len(out) == 0 {
		return 0
	}

	var bestDist uint32 = out[0]
	var bestIdx uint32 = 0
	for i := 1; i < len(out); i++ {
		if out[i] < bestDist {
			bestDist = out[i]
			bestIdx = uint32(i)
		}
	}

	invertedDist := uint32(131072) - bestDist
	return (uint64(invertedDist) << 32) | uint64(bestIdx)
}
