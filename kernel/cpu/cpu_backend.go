package cpu

import (
	"fmt"
	"math/bits"
	"unsafe"

	"github.com/theapemachine/six/numeric"
)

const (
	cubeFaces           int   = 257 // must match geometry.CubeFaces
	overlapWeight       int64 = 500
	fillWeight          int64 = 900
	expectationWeight   int64 = 250
	contradictionWeight int64 = 650
	precisionUnity      int64 = 1024
	scoreShiftBits            = 10
	precisionBytes            = 5 * cubeFaces * 2
)

type scoreTerms struct {
	overlap              uint32
	fill                 uint32
	expectationScaled    uint64
	missingSupportScaled uint64
	vetoViolationScaled  uint64
}

func precisionFor(precisionWords []uint16, cube, block int) uint16 {
	if len(precisionWords) < 5*cubeFaces {
		return uint16(precisionUnity)
	}

	return precisionWords[cube*cubeFaces+block]
}

func scoreFromTerms(t scoreTerms) int32 {
	if precisionUnity == 0 {
		return 0
	}

	expectation := int64(t.expectationScaled) / precisionUnity
	missingSupport := int64(t.missingSupportScaled) / precisionUnity
	vetoViolation := int64(t.vetoViolationScaled) / precisionUnity
	contradiction := missingSupport + vetoViolation

	return int32(
		int64(t.overlap)*overlapWeight+
			int64(t.fill)*fillWeight+
			expectation*expectationWeight-
			contradiction*contradictionWeight,
	) >> scoreShiftBits
}

func accumulateScoreTerms(dictWords, ctxWords, expWords []uint64, precisionWords []uint16, cubeBase int) scoreTerms {
	var terms scoreTerms

	for c := 0; c < 4; c++ {
		for b := 0; b < cubeFaces; b++ {
			supportPrecision := uint64(precisionFor(precisionWords, c, b))
			vetoPrecision := uint64(precisionFor(precisionWords, 4, b))

			for i := 0; i < 8; i++ {
				offset := (c*cubeFaces+b)*8 + i
				vetoOffset := (4*cubeFaces+b)*8 + i

				candidate := dictWords[cubeBase+offset]
				ctx := ctxWords[1+offset]
				exp := expWords[1+offset]
				missing := exp &^ ctx

				vetoCtx := ctxWords[1+vetoOffset]
				candidateVeto := dictWords[cubeBase+vetoOffset]

				terms.overlap += uint32(bits.OnesCount64(candidate & ctx))
				terms.fill += uint32(bits.OnesCount64(candidate & missing))

				expectationCount := uint64(bits.OnesCount64(candidate & exp))
				missingCount := uint64(bits.OnesCount64(ctx &^ candidate))
				vetoCount := uint64(bits.OnesCount64(candidate & vetoCtx))
				vetoCount += uint64(bits.OnesCount64(candidateVeto & ctx))

				terms.expectationScaled += expectationCount * supportPrecision
				terms.missingSupportScaled += missingCount * supportPrecision
				terms.vetoViolationScaled += vetoCount * vetoPrecision
			}
		}
	}

	return terms
}

func BestFillCPUPacked(
	dictionary unsafe.Pointer,
	numChords int,
	context unsafe.Pointer,
	expectedReality unsafe.Pointer,
	expectedPrecision unsafe.Pointer,
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

	var precisionData []byte
	if expectedPrecision != nil {
		precisionData, err = numeric.PtrToBytes(expectedPrecision, precisionBytes)
		if err != nil {
			return 0, err
		}
	}

	return BestFillCPUPackedBytes(dictBytes, numChords, ctxBytes, expBytes, precisionData, lutBytes)
}

func BestFillCPUPackedBytes(
	dictBytes []byte,
	numChords int,
	ctxBytes []byte,
	expBytes []byte,
	precisionData []byte,
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

	var precisionWords []uint16
	if len(precisionData) > 0 {
		if len(precisionData) < precisionBytes {
			return 0, fmt.Errorf("precision buffer too small: have=%d want=%d", len(precisionData), precisionBytes)
		}
		precisionWords = unsafe.Slice((*uint16)(unsafe.Pointer(&precisionData[0])), len(precisionData)/2)
	}

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

		cubeBase := base + 1
		terms := accumulateScoreTerms(dictWords, ctxWords, expWords, precisionWords, cubeBase)
		scoreFixed := scoreFromTerms(terms)

		rotCandidate := int((header >> 9) & 0x3F)
		geodDist := uint16(255)
		if len(lutBytes) >= numeric.GeodesicMatrixSize && ctxRot < 60 && rotCandidate < 60 {
			geodDist = uint16(lutBytes[ctxRot*60+rotCandidate])
		}

		invertedDist := uint16(65535 - geodDist)

		packed := numeric.PackResult(scoreFixed, invertedDist, id)
		if packed > bestPacked {
			bestPacked = packed
		}
	}

	return bestPacked, nil
}
