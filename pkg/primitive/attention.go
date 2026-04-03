package primitive

import "github.com/theapemachine/six/pkg/core"

/*
tokenAttentionRegsConfigured returns the four configured R0–R3 word indices,
or ok false when any index is invalid so the substrate keeps legacy full-word behavior.
*/
func tokenAttentionRegsConfigured() (r0, r1, r2, r3 int, ok bool) {

	reg := core.Cfg.Value.Region.Registers
	r0 = reg.R0
	r1 = reg.R1
	r2 = reg.R2
	r3 = reg.R3

	if r0 < 0 || r1 < 0 || r2 < 0 || r3 < 0 {
		return 0, 0, 0, 0, false
	}

	if r0 >= core.Cfg.Value.Words || r1 >= core.Cfg.Value.Words ||
		r2 >= core.Cfg.Value.Words || r3 >= core.Cfg.Value.Words {
		return 0, 0, 0, 0, false
	}

	return r0, r1, r2, r3, true
}

/*
TokenAttentionMaskForWord returns the 64-bit mask for token wordOffset within
the token region. When R0–R3 are all zero, attention is off and the mask is
all bits set (legacy ScanSignals). Otherwise registers tile Mod-4 across token
words so the LGP can sculpt a repeating 256-bit attention stencil.
*/
func TokenAttentionMaskForWord(frame *Value, wordOffset int) uint64 {

	if frame == nil {
		return ^uint64(0)
	}

	r0, r1, r2, r3, ok := tokenAttentionRegsConfigured()
	if !ok {
		return ^uint64(0)
	}

	if (*frame)[r0]|(*frame)[r1]|(*frame)[r2]|(*frame)[r3] == 0 {
		return ^uint64(0)
	}

	switch wordOffset % 4 {
	case 0:
		return (*frame)[r0]
	case 1:
		return (*frame)[r1]
	case 2:
		return (*frame)[r2]
	default:
		return (*frame)[r3]
	}
}

/*
TokenAttentionActive is true when R0–R3 host any non-zero mask material.
*/
func TokenAttentionActive(a, b *Value) bool {

	if a == nil || b == nil {
		return false
	}

	r0, r1, r2, r3, ok := tokenAttentionRegsConfigured()
	if !ok {
		return false
	}

	az := (*a)[r0] | (*a)[r1] | (*a)[r2] | (*a)[r3]
	bz := (*b)[r0] | (*b)[r1] | (*b)[r2] | (*b)[r3]

	return az != 0 || bz != 0
}
