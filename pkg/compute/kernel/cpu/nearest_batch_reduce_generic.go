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
	n := int(batchCount)

	if n <= 0 {
		return
	}

	query := (*[8]uint64)(unsafe.Pointer(&v[0]))
	out := (*[256]uint32)(unsafe.Pointer(&v[kernel.SignalsStartWord]))

	for idx := 0; idx < n; idx++ {
		base := kernel.NearestAffinityCandidatesStartWord + idx*8
		cand := (*[8]uint64)(unsafe.Pointer(&v[base]))
		var dist uint32

		for wordIdx := 0; wordIdx < 8; wordIdx++ {
			dist += uint32(bits.OnesCount64(query[wordIdx] ^ cand[wordIdx]))
		}

		out[idx] = dist
	}

	bestIdx := uint64(0)
	bestDist := uint64(out[0])

	for idx := 1; idx < n; idx++ {
		d := uint64(out[idx])

		if d < bestDist {
			bestDist = d
			bestIdx = uint64(idx)
		}
	}

	v[kernel.SignalsStartWord+kernel.SignalBestIdxOffset] = bestIdx
	v[kernel.SignalsStartWord+kernel.SignalBestDistOffset] = bestDist
}
