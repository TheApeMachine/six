package primitive

import (
	"math/bits"
	"sync"

	"github.com/theapemachine/six/pkg/core"
)

/*
MacroGraph buffers hold XOR-accumulated token strips keyed by a parent ValueID
(simple resonator-style context for extended RESONATOR_UNBIND).
*/
var macroGraph sync.Map // uint64 -> []uint64

/*
MacroGraphAccumulateFromChild XORs a child token region into the macro buffer
for parentValueID (typically canonical parent A after signal emission).
*/
func MacroGraphAccumulateFromChild(parentValueID uint64, child *Value) {

	if parentValueID == 0 || child == nil {
		return
	}

	tokenBits := int(core.Cfg.Value.Region.Tokens.Bits)
	tokenWords := int((tokenBits + 63) / 64)
	base := core.Cfg.Value.Region.Tokens.Start

	buf := make([]uint64, tokenWords)

	raw, _ := macroGraph.Load(parentValueID)
	if existing, ok := raw.([]uint64); ok && len(existing) == tokenWords {
		copy(buf, existing)
	}

	for offset := 0; offset < tokenWords; offset++ {
		idx := base + offset
		if idx >= len(*child) {
			break
		}

		buf[offset] ^= child[idx]
	}

	macroGraph.Store(parentValueID, buf)
}

/*
MacroGraphSnapshot returns a copy of the macro buffer for valueID if present.
*/
func MacroGraphSnapshot(valueID uint64) ([]uint64, bool) {

	if valueID == 0 {
		return nil, false
	}

	raw, ok := macroGraph.Load(valueID)
	if !ok {
		return nil, false
	}

	buf, ok := raw.([]uint64)
	if !ok || len(buf) == 0 {
		return nil, false
	}

	out := make([]uint64, len(buf))
	copy(out, buf)

	return out, true
}

/*
MacroGraphDiscard removes a macro buffer (e.g. when a Value evaporates).
*/
func MacroGraphDiscard(valueID uint64) {

	macroGraph.Delete(valueID)
}

/*
ApplyResonatorUnbindToTokens XORs a rotated macro snapshot into the frame token
region. pivotID selects which buffer (usually frame id or Prev).
*/
func ApplyResonatorUnbindToTokens(
	frame *[128]uint64,
	pivotID uint64,
	rotationBits int,
) {

	if frame == nil {
		return
	}

	macro, ok := MacroGraphSnapshot(pivotID)
	if !ok {
		return
	}

	tokenBits := int(core.Cfg.Value.Region.Tokens.Bits)
	tokenWords := int((tokenBits + 63) / 64)
	base := core.Cfg.Value.Region.Tokens.Start

	rot := rotationBits & 63

	for offset := 0; offset < tokenWords && offset < len(macro); offset++ {
		wordIdx := base + offset
		if wordIdx >= len(frame) {
			break
		}

		word := macro[offset]
		if rot != 0 {
			word = bits.RotateLeft64(word, rot)
		}

		frame[wordIdx] ^= word
	}
}
