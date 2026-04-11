//go:build amd64 || arm64

package cpu

import (
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
)

/*
nearestBatchReduce runs batch Hamming distances from the query slab to
contiguous candidates, then writes argmin into the signal words. Matches
the Metal Execute path for opcode 0x6 with NearestAffinityBatchWord > 0.
*/
func nearestBatchReduce(v *[128]uint64, batchCount uint64) {
	n := int(batchCount)

	if n <= 0 {
		return
	}

	query := (*uint64)(unsafe.Pointer(&v[0]))
	cands := (*uint64)(unsafe.Pointer(&v[kernel.NearestAffinityCandidatesStartWord]))
	out := (*[256]uint32)(unsafe.Pointer(&v[kernel.SignalsStartWord]))

	batchAffinityDistances(query, cands, n, &out[0])

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
