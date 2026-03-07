package numeric

import (
	"fmt"
	"unsafe"
)

const (
	ManifoldBytes      = 82248 // 8 (header+pad) + 5 × 257 × 64
	ManifoldWords      = ManifoldBytes / 8
	CubeWords          = 5 * 257 * 8
	GeodesicMatrixSize = 60 * 60
	ScoreScale         = 4_000_000.0
)

func DecodePacked(packed uint64) (int, float64) {
	scoreFixed := uint32(packed >> 40)
	bestIdx := int(packed & 0xFFFFFF)
	bestScore := float64(scoreFixed) / ScoreScale
	return bestIdx, bestScore
}

func PackResult(scoreFixed int32, invertedDist uint16, id int) uint64 {
	if id < 0 {
		id = 0
	}

	return (uint64(uint32(scoreFixed)&0xFFFFFF) << 40) |
		(uint64(invertedDist) << 24) |
		uint64(id&0xFFFFFF)
}

func RebasePackedID(packed uint64, base int) uint64 {
	id := max(int(packed&0xFFFFFF)+base, 0)
	return (packed &^ uint64(0xFFFFFF)) | uint64(id&0xFFFFFF)
}

func PtrToBytes(ptr unsafe.Pointer, n int) ([]byte, error) {
	if n == 0 {
		return nil, nil
	}

	if ptr == nil {
		return nil, fmt.Errorf("nil pointer for %d bytes", n)
	}

	return unsafe.Slice((*byte)(ptr), n), nil
}

func FirstPtr(b []byte) unsafe.Pointer {
	if len(b) == 0 {
		return nil
	}

	return unsafe.Pointer(&b[0])
}

func AbsInt(v int) int {
	if v < 0 {
		return -v
	}

	return v
}
