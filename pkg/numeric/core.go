package numeric

import (
	"math"
	"unsafe"
)

/*
Manifold and scoring layout constants for GPU and packed-result encoding.
ManifoldBytes = header(8) + 5×257×64; ScoreScale maps fixed-point scores.
*/
const (
	ManifoldBytes      = 82248 // 8 (header+pad) + 5 × 257 × 64
	ManifoldWords      = ManifoldBytes / 8
	CubeWords          = 5 * 257 * 8
	GeodesicMatrixSize = 60 * 60
	ScoreScale         = 4_000_000.0
	packedScoreBias    = 1 << 23
	packedScoreMax     = packedScoreBias - 1
	packedScoreMin     = -packedScoreBias
)

/*
DecodePacked extracts id (bits 0–23), invertedDist (bits 24–39), and score (bits 40–63, bias-shifted)
from a packed uint64. Score is decoded as (raw - packedScoreBias) / ScoreScale.
*/
func DecodePacked(packed uint64) (int, float64) {
	scoreFixed := int32((packed>>40)&0xFFFFFF) - int32(packedScoreBias)
	bestIdx := int(packed & 0xFFFFFF)
	bestScore := float64(scoreFixed) / ScoreScale
	return bestIdx, bestScore
}

/*
PackResult encodes scoreFixed, invertedDist, and id into a single uint64.
Layout: score(bits 40–63), invertedDist(bits 24–39), id(bits 0–23).
Clamps scoreFixed to packedScoreMin..packedScoreMax.
*/
func PackResult(scoreFixed int32, invertedDist uint16, id int) uint64 {
	if id < 0 {
		id = 0
	}

	if scoreFixed < packedScoreMin {
		scoreFixed = packedScoreMin
	}

	if scoreFixed > packedScoreMax {
		scoreFixed = packedScoreMax
	}

	scoreBits := uint64(uint32(int64(scoreFixed) + int64(packedScoreBias)))

	return (scoreBits << 40) |
		(uint64(invertedDist) << 24) |
		uint64(id&0xFFFFFF)
}

/*
RebasePackedID adds base to the packed id field (low 24b), preserving score and invertedDist.
*/
func RebasePackedID(packed uint64, base int) uint64 {
	id := max(int(packed&0xFFFFFF)+base, 0)
	const max24 = 0xFFFFFF
	if id > max24 {
		id = max24
	}
	return (packed &^ uint64(max24)) | uint64(id&max24)
}

/*
PtrToBytes returns a byte slice over the memory at ptr of length `length`.
Returns nil, nil for length==0, and an error for ptr==nil when length>0.
*/
func PtrToBytes(ptr unsafe.Pointer, length int) ([]byte, error) {
	if length == 0 {
		return nil, nil
	}

	if ptr == nil {
		return nil, NumericNilPointerError
	}

	return unsafe.Slice((*byte)(ptr), length), nil
}

/*
FirstPtr returns a pointer to the first byte of buf, or nil if buf is empty.
*/
func FirstPtr(buf []byte) unsafe.Pointer {
	if len(buf) == 0 {
		return nil
	}

	return unsafe.Pointer(&buf[0])
}

/*
AbsInt returns the absolute value of value.
*/
func AbsInt(value int) int {
	if value == math.MinInt {
		return math.MaxInt
	}
	if value < 0 {
		return -value
	}

	return value
}

type NumericError string

const (
	NumericNilPointerError NumericError = "nil pointer"
)

func (err NumericError) Error() string {
	return string(err)
}
