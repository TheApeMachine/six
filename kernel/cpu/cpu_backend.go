package cpu

import (
	"fmt"
	"math/bits"
	"unsafe"

	"github.com/theapemachine/six/numeric"
)

func BestFillCPUPacked(
	dictionary unsafe.Pointer,
	numChords int,
	context unsafe.Pointer,
	expectedReality unsafe.Pointer,
	targetIdx int,
	geodesicLUT unsafe.Pointer,
) (uint64, error) {
	if numChords == 0 {
		return 0, nil
	}

	dictBytes, err := numeric.PtrToBytes(dictionary, numChords*numeric.ManifoldBytes)
	if err != nil {
		return 0, err
	}
	ctxBytes, err := numeric.PtrToBytes(context, numeric.ManifoldBytes)
	if err != nil {
		return 0, err
	}
	if expectedReality == nil {
		expectedReality = context
	}
	expBytes, err := numeric.PtrToBytes(expectedReality, numeric.ManifoldBytes)
	if err != nil {
		return 0, err
	}

	var lutBytes []byte
	if geodesicLUT != nil {
		lutBytes, err = numeric.PtrToBytes(geodesicLUT, numeric.GeodesicMatrixSize)
		if err != nil {
			return 0, err
		}
	}

	return BestFillCPUPackedBytes(dictBytes, numChords, ctxBytes, expBytes, targetIdx, lutBytes)
}

func BestFillCPUPackedBytes(
	dictBytes []byte,
	numChords int,
	ctxBytes []byte,
	expBytes []byte,
	targetIdx int,
	lutBytes []byte,
) (uint64, error) {
	if numChords < 0 {
		return 0, fmt.Errorf("invalid chord count: %d", numChords)
	}
	if len(dictBytes) < numChords*numeric.ManifoldBytes {
		return 0, fmt.Errorf("dictionary buffer too small: have=%d want=%d", len(dictBytes), numChords*numeric.ManifoldBytes)
	}
	if len(ctxBytes) < numeric.ManifoldBytes {
		return 0, fmt.Errorf("context buffer too small: have=%d want=%d", len(ctxBytes), numeric.ManifoldBytes)
	}
	if len(expBytes) < numeric.ManifoldBytes {
		return 0, fmt.Errorf("expected buffer too small: have=%d want=%d", len(expBytes), numeric.ManifoldBytes)
	}

	dictWords := unsafe.Slice((*uint64)(unsafe.Pointer(&dictBytes[0])), len(dictBytes)/8)
	ctxWords := unsafe.Slice((*uint64)(unsafe.Pointer(&ctxBytes[0])), len(ctxBytes)/8)
	expWords := unsafe.Slice((*uint64)(unsafe.Pointer(&expBytes[0])), len(expBytes)/8)

	ctxHeader := uint16(ctxWords[0] & 0xFFFF)
	ctxWinding := uint8((ctxHeader >> 5) & 0xF)
	ctxState := uint8((ctxHeader >> 15) & 0x1)
	ctxRot := int((ctxHeader >> 9) & 0x3F)

	var bestPacked uint64

	for id := range numChords {
		base := id * numeric.ManifoldWords
		header := uint16(dictWords[base] & 0xFFFF)

		if uint8((header>>5)&0xF) != ctxWinding {
			continue
		}
		if uint8((header>>15)&0x1) != ctxState {
			continue
		}

		var hamming uint32
		var disorder uint32
		var expectation uint32

		cubeBase := base + 1
		for i := 0; i < numeric.CubeWords; i++ {
			candidate := dictWords[cubeBase+i]
			ctx := ctxWords[1+i]
			exp := expWords[1+i]

			hamming += uint32(bits.OnesCount64(candidate & ctx))
			disorder += uint32(bits.OnesCount64(candidate &^ ctx))
			expectation += uint32(bits.OnesCount64(candidate & exp))
		}

		scoreFixed := int32((int64(hamming)*500 + int64(expectation)*1000 - int64(disorder)*300) >> 10)

		rotCandidate := int((header >> 9) & 0x3F)
		geodDist := uint16(255)
		if len(lutBytes) >= numeric.GeodesicMatrixSize && ctxRot < 60 && rotCandidate < 60 {
			geodDist = uint16(lutBytes[ctxRot*60+rotCandidate])
		}

		indexDist := numeric.AbsInt(id - targetIdx)
		combined := min(65535, indexDist+int(geodDist))
		invertedDist := uint16(65535 - combined)

		packed := numeric.PackResult(scoreFixed, invertedDist, id)
		if packed > bestPacked {
			bestPacked = packed
		}
	}

	return bestPacked, nil
}
