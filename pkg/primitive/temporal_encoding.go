package primitive

import (
	"math/bits"

	"github.com/theapemachine/six/pkg/core"
)

/*
TemporalRotateTokens rotates every word in the token region left by rot (0–63)
to stamp ordinal time into the geometry before the next BindTokenHD.
*/
func (value *Value) TemporalRotateTokens(rot int) {

	if value == nil {
		return
	}

	r := rot & 63
	if r == 0 {
		return
	}

	nWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)
	base := core.Cfg.Value.Region.Tokens.Start

	for offset := 0; offset < nWords; offset++ {
		idx := base + offset
		if idx >= len(*value) {
			break
		}

		(*value)[idx] = bits.RotateLeft64((*value)[idx], r)
	}
}
