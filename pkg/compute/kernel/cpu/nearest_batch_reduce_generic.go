//go:build !amd64 && !arm64

package cpu

import (
	"math/bits"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
)

/*
nearestBatchReduce is the portable batch nearest-affinity path when SIMD
batchAffinityDistances is unavailable. Query and each candidate occupy eight
uint64 lanes (64-byte chunks), matching the assembly kernels.
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

	query := (*[8]uint64)(unsafe.Pointer(&v[0]))
	out := (*[256]uint32)(unsafe.Pointer(&v[kernel.SignalsStartWord]))

	for candidateIdx := 0; candidateIdx < n; candidateIdx++ {
		base := kernel.NearestAffinityCandidatesStartWord + candidateIdx*8
		cand := (*[8]uint64)(unsafe.Pointer(&v[base]))
		var dist uint32

		for laneIdx := range query {
			dist += uint32(bits.OnesCount64(query[laneIdx] ^ cand[laneIdx]))
		}

		out[candidateIdx] = dist
	}

	bestIdx := uint64(0)
	bestDist := uint64(out[0])

	for candidateIdx := 1; candidateIdx < n; candidateIdx++ {
		currDist := uint64(out[candidateIdx])

		if currDist < bestDist {
			bestDist = currDist
			bestIdx = uint64(candidateIdx)
		}
	}

	v[kernel.SignalsStartWord+kernel.SignalBestIdxOffset] = bestIdx
	v[kernel.SignalsStartWord+kernel.SignalBestDistOffset] = bestDist
}
