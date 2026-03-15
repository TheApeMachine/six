package lsm

import (
	"math/bits"

	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

/*
extractStatePhase recovers the GF(257) state encoded in a stored state chord.
ResidualCarry is treated as the authoritative snapshot when present because the
stored value is now allowed to be lexical-free while query observables still
carry the transient five-bit seed. When that snapshot is absent, we fall back to
ignoring the lexical seed bits and reading whatever native state bit remains.
*/
func extractStatePhase(chord data.Chord, symbol byte) (numeric.Phase, bool) {
	if carry := chord.ResidualCarry(); carry > 0 {
		phase := numeric.Phase(carry % uint64(numeric.FermatPrime))
		if phase > 0 {
			return phase, true
		}
	}

	base := data.BaseChord(symbol)

	for blockIdx := 0; blockIdx < 5; blockIdx++ {
		block := chord.Block(blockIdx)
		if blockIdx == 4 {
			block &= 1
		}
		block &^= base.Block(blockIdx)
		if block == 0 {
			continue
		}

		bitIdx := bits.TrailingZeros64(block)
		primeIdx := blockIdx*64 + bitIdx
		phase := numeric.Phase(primeIdx)
		if phase >= 1 && uint32(phase) < numeric.FermatPrime {
			return phase, true
		}
	}

	return 0, false
}

func statePhaseMatches(chord data.Chord, symbol byte, expected numeric.Phase) bool {
	phase, ok := extractStatePhase(chord, symbol)
	return ok && phase == expected
}

func firstMetaForKeyUnsafe(idx *SpatialIndexServer, key uint64) data.Chord {
	meta := data.MustNewChord()
	if metas := idx.metaEntries[key]; len(metas) > 0 {
		meta.CopyFrom(metas[0])
	}
	return meta
}

func lexicalDistance(left, right byte) int {
	return bits.OnesCount8(left ^ right)
}
