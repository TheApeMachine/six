package primitive

import (
	"math/bits"

	"github.com/theapemachine/six/pkg/core"
)

/*
RefreshSemanticAffinityKey recomputes SimHash from the token region, then XORs
a fold of execution state (accumulator, r0, sequence) into the Affinity word.
This yields a routing key that moves as in-band programs mutate registers.
*/
func (value *Value) RefreshSemanticAffinityKey() {

	if value == nil {
		return
	}

	value.ComputeAffinityLSH()

	acc := value.GetWord(core.Cfg.Value.Region.State.Accumulator)
	seq := value.GetWord(core.Cfg.Value.Region.State.Sequence)
	r0 := value.GetWord(core.Cfg.Value.Region.Registers.R0)
	mix := bits.RotateLeft64(acc^r0^(seq*0x9E3779B97F4A7C15), 23)

	affWord := core.Cfg.Value.Region.Affinity.Start
	if affWord < 0 || affWord >= len(*value) {
		return
	}

	(*value)[affWord] ^= mix
}
