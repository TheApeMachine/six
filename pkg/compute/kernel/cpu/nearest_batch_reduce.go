//go:build amd64 || arm64

package cpu

import (
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
)

/*
nearestBatchReduce runs batch Hamming distances from the query slab to
contiguous candidates, then writes argmin into the signal words. Matches
the Metal Execute path for OpcodeXOR with NearestAffinityBatchWord > 0.
*/
func nearestBatchReduce(v *[128]uint64, batchCount uint64) {
	if batchCount == 0 {
		return
	}

	const maxInt = int(^uint(0) >> 1)

	if batchCount > uint64(len(v)) {
		batchCount = uint64(len(v))
	}

	if batchCount > uint64(maxInt) {
		batchCount = uint64(maxInt)
	}

	n := int(batchCount)

	query := (*uint64)(unsafe.Pointer(&v[0]))
	cands := (*uint64)(unsafe.Pointer(&v[kernel.NearestAffinityCandidatesStartWord]))
	out := (*[256]uint32)(unsafe.Pointer(&v[kernel.SignalsStartWord]))

	batchAffinityDistances(query, cands, n, &out[0])

	bestIdx := uint64(0)
	bestDist := uint64(out[0])

	for candidateIdx := 1; candidateIdx < n; candidateIdx++ {
		distance := uint64(out[candidateIdx])

		if distance < bestDist {
			bestDist = distance
			bestIdx = uint64(candidateIdx)
		}
	}

	v[kernel.SignalsStartWord+kernel.SignalBestIdxOffset] = bestIdx
	v[kernel.SignalsStartWord+kernel.SignalBestDistOffset] = bestDist
}
