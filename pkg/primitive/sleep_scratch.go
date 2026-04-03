package primitive

import (
	"sync/atomic"

	"github.com/theapemachine/six/pkg/core"
)

/*
PrepareSleepScratchFrame strips graph links, mints a fresh ValueID, and seeds
thermodynamic energy so sleep consolidation can mutate clones without aliasing
live routing-table pointers.
*/
func PrepareSleepScratchFrame(value *Value) {

	if value == nil {
		return
	}

	prevWord := core.Cfg.Value.Region.Prev.Start
	nextWord := core.Cfg.Value.Region.Next.Start
	idWord := core.Cfg.Value.Region.ID.Start

	if prevWord >= 0 && prevWord < len(*value) {
		(*value)[prevWord] = 0
	}

	if nextWord >= 0 && nextWord < len(*value) {
		(*value)[nextWord] = 0
	}

	if idWord >= 0 && idWord < len(*value) {
		(*value)[idWord] = atomic.AddUint64(&globalValueIDCounter, 1)
	}

	frame := (*[128]uint64)(value)
	SeedThermodynamicEnergy(frame)
}
