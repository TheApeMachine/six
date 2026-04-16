package primitive

import (
	"sync"

	"github.com/theapemachine/six/pkg/compute/kernel"
)

var valueByID sync.Map

/*
RegisterValue maps an ID word to the live Value pointer so execute-time
hydration can resolve peer IDs without Go-side token staging.
*/
func RegisterValue(value *Value) {
	if value == nil {
		return
	}

	id := value.ID()

	if id == 0 {
		return
	}

	valueByID.Store(id, value)
}

/*
UnregisterValue drops a Value from the active ID map before the frame is cleared.
*/
func UnregisterValue(value *Value) {
	if value == nil {
		return
	}

	id := value.ID()

	if id == 0 {
		return
	}

	valueByID.Delete(id)
}

/*
LookupValue returns the registered Value for an ID, or nil.
*/
func LookupValue(id uint64) *Value {
	if id == 0 {
		return nil
	}

	raw, ok := valueByID.Load(id)
	if !ok {
		return nil
	}

	value, ok := raw.(*Value)
	if !ok {
		return nil
	}

	return value
}

/*
HydrateLearnerPeers copies peer token regions into the learner Asset staging
when asset[0]==PrevID and asset[16]==NextID with no other asset words set,
matching the unsupervised_learn contract without Go-side token duplication.
*/
func HydrateLearnerPeers(value *Value) {
	if value == nil {
		return
	}

	prev := (*value)[kernel.PrevStartWord]
	next := (*value)[kernel.NextStartWord]

	if prev == 0 || next == 0 {
		return
	}

	ast := kernel.AssetStartWord

	if (*value)[ast] != prev || (*value)[ast+16] != next {
		return
	}

	for offset := 1; offset < 16; offset++ {
		if (*value)[ast+offset] != 0 {
			return
		}

		if (*value)[ast+16+offset] != 0 {
			return
		}
	}

	peerA := LookupValue(prev)
	peerB := LookupValue(next)

	if peerA == nil || peerB == nil {
		return
	}

	tokenStart, tokenWords := TokenRegion.WordExtent()

	for offset := 0; offset < tokenWords && offset < 16; offset++ {
		(*value)[ast+offset] = (*peerA)[tokenStart+offset]
		(*value)[ast+16+offset] = (*peerB)[tokenStart+offset]
	}
}
