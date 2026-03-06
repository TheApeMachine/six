package cpu

import (
	"fmt"
	"math/bits"
	"unsafe"

	"github.com/theapemachine/six/numeric"
)

const (
	overlapWeight       int64 = 500
	fillWeight          int64 = 900
	expectationWeight   int64 = 250
	contradictionWeight int64 = 650
	scoreShiftBits            = 10
)

type scoreTerms struct {
	overlap        uint32
	fill           uint32
	expectation    uint32
	missingSupport uint32
	vetoViolation  uint32
}

func (t scoreTerms) contradiction() uint32 {
	return t.missingSupport + t.vetoViolation
}

func scoreFromTerms(t scoreTerms) int32 {
	return int32(
		int64(t.overlap)*overlapWeight+
			int64(t.fill)*fillWeight+
			int64(t.expectation)*expectationWeight-
			int64(t.contradiction())*contradictionWeight,
	) >> scoreShiftBits
}

func accumulateScoreTerms(dictWords, ctxWords, expWords []uint64, cubeBase int) scoreTerms {
	var terms scoreTerms

	for c := 0; c < 4; c++ {
		for b := 0; b < 27; b++ {
			for i := 0; i < 8; i++ {
				offset := (c*27+b)*8 + i
				vetoOffset := (4*27+b)*8 + i

				candidate := dictWords[cubeBase+offset]
				ctx := ctxWords[1+offset]
				exp := expWords[1+offset]
				missing := exp &^ ctx

				vetoCtx := ctxWords[1+vetoOffset]
				candidateVeto := dictWords[cubeBase+vetoOffset]

				terms.overlap += uint32(bits.OnesCount64(candidate & ctx))
				terms.fill += uint32(bits.OnesCount64(candidate & missing))
				terms.expectation += uint32(bits.OnesCount64(candidate & exp))
				terms.missingSupport += uint32(bits.OnesCount64(ctx &^ candidate))
				terms.vetoViolation += uint32(bits.OnesCount64(candidate & vetoCtx))
				terms.vetoViolation += uint32(bits.OnesCount64(candidateVeto & ctx))
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

	return BestFillCPUPackedBytes(dictBytes, numChords, ctxBytes, expBytes, lutBytes)
}

func BestFillCPUPackedBytes(
	dictBytes []byte,
	numChords int,
	ctxBytes []byte,
	expBytes []byte,
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

		cubeBase := base + 1
		terms := accumulateScoreTerms(dictWords, ctxWords, expWords, cubeBase)
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
