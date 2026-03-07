package cpu

import (
	"math/bits"
	"testing"
	"unsafe"

	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/numeric"
)

func TestScoreFromTerms_MatchesLegacyFormula(t *testing.T) {
	terms := scoreTerms{
		overlap:              219,
		fill:                 143,
		expectationScaled:    301 * 1024,
		missingSupportScaled: 67 * 1024,
		vetoViolationScaled:  29 * 1024,
	}

	legacyRaw := int64(terms.overlap)*500 +
		int64(terms.fill)*900 +
		int64(terms.expectationScaled/1024)*250 -
		int64((terms.missingSupportScaled/1024)+(terms.vetoViolationScaled/1024))*650
	legacy := int32(legacyRaw >> 10)

	if got := scoreFromTerms(terms); got != legacy {
		t.Fatalf("score mismatch: got=%d want=%d", got, legacy)
	}
}

func TestBestFillCPUPackedBytes_ParityWithLegacyLoop(t *testing.T) {
	var ctx geometry.IcosahedralManifold
	ctx.Header.SetState(1)
	ctx.Header.SetRotState(7)
	ctx.Cubes[0][0].Set(1)
	ctx.Cubes[0][0].Set(2)
	ctx.Cubes[0][0].Set(10)
	ctx.Cubes[4][0].Set(50)

	var exp geometry.IcosahedralManifold
	exp.Header = ctx.Header
	exp.Cubes[0][0].Set(1)
	exp.Cubes[0][0].Set(2)
	exp.Cubes[0][0].Set(3)
	exp.Cubes[0][0].Set(10)
	exp.Cubes[0][0].Set(11)
	exp.Cubes[4][0].Set(50)

	dict := make([]geometry.IcosahedralManifold, 2)

	dict[0].Header = ctx.Header
	dict[0].Header.SetRotState(3)
	dict[0].Cubes[0][0].Set(1)
	dict[0].Cubes[0][0].Set(3)
	dict[0].Cubes[0][0].Set(10)
	dict[0].Cubes[0][0].Set(50)
	dict[0].Cubes[4][0].Set(2)

	dict[1].Header = ctx.Header
	dict[1].Header.SetRotState(8)
	dict[1].Cubes[0][0].Set(1)
	dict[1].Cubes[0][0].Set(2)
	dict[1].Cubes[0][0].Set(11)

	dictBytes := unsafe.Slice((*byte)(unsafe.Pointer(&dict[0])), len(dict)*numeric.ManifoldBytes)
	ctxBytes := unsafe.Slice((*byte)(unsafe.Pointer(&ctx)), numeric.ManifoldBytes)
	expBytes := unsafe.Slice((*byte)(unsafe.Pointer(&exp)), numeric.ManifoldBytes)

	got, err := BestFillCPUPackedBytes(dictBytes, len(dict), ctxBytes, expBytes, nil, nil)
	if err != nil {
		t.Fatalf("BestFillCPUPackedBytes returned error: %v", err)
	}

	want, err := legacyBestFillCPUPackedBytes(dictBytes, len(dict), ctxBytes, expBytes)
	if err != nil {
		t.Fatalf("legacyBestFillCPUPackedBytes returned error: %v", err)
	}

	if got != want {
		t.Fatalf("packed mismatch: got=%d want=%d", got, want)
	}
}

func legacyBestFillCPUPackedBytes(
	dictBytes []byte,
	numChords int,
	ctxBytes []byte,
	expBytes []byte,
) (uint64, error) {
	dictWords := unsafe.Slice((*uint64)(unsafe.Pointer(&dictBytes[0])), len(dictBytes)/8)
	ctxWords := unsafe.Slice((*uint64)(unsafe.Pointer(&ctxBytes[0])), len(ctxBytes)/8)
	expWords := unsafe.Slice((*uint64)(unsafe.Pointer(&expBytes[0])), len(expBytes)/8)

	ctxHeader := uint16(ctxWords[0] & 0xFFFF)
	ctxWinding := uint8((ctxHeader >> 5) & 0xF)
	ctxState := uint8((ctxHeader >> 15) & 0x1)

	var bestPacked uint64

	for id := 0; id < numChords; id++ {
		base := id * numeric.ManifoldWords
		header := uint16(dictWords[base] & 0xFFFF)

		if uint8((header>>5)&0xF) != ctxWinding {
			continue
		}
		if uint8((header>>15)&0x1) != ctxState {
			continue
		}

		var overlap uint32
		var fill uint32
		var contradiction uint32
		var expectation uint32

		cubeBase := base + 1
		for c := 0; c < 4; c++ {
			for b := 0; b < cubeFaces; b++ {
				for i := 0; i < 8; i++ {
					offset := (c*cubeFaces+b)*8 + i
					vetoOffset := (4*cubeFaces+b)*8 + i

					candidate := dictWords[cubeBase+offset]
					ctx := ctxWords[1+offset]
					exp := expWords[1+offset]
					missing := exp &^ ctx

					vetoCtx := ctxWords[1+vetoOffset]
					candidateVeto := dictWords[cubeBase+vetoOffset]

					overlap += uint32(bits.OnesCount64(candidate & ctx))
					fill += uint32(bits.OnesCount64(candidate & missing))
					expectation += uint32(bits.OnesCount64(candidate & exp))
					contradiction += uint32(bits.OnesCount64(ctx &^ candidate))
					contradiction += uint32(bits.OnesCount64(candidate & vetoCtx))
					contradiction += uint32(bits.OnesCount64(candidateVeto & ctx))
				}
			}
		}

		scoreFixed := int32((int64(overlap)*500 + int64(fill)*900 + int64(expectation)*250 - int64(contradiction)*650) >> 10)
		packed := numeric.PackResult(scoreFixed, 65535-255, id)
		if packed > bestPacked {
			bestPacked = packed
		}
	}

	return bestPacked, nil
}
