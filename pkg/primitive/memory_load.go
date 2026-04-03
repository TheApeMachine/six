package primitive

import (
	"unsafe"

	"github.com/theapemachine/six/pkg/core"
)

/*
MemoryLoadPendingMagic is written to State.Index by LGPXMemoryLoadMark so the
host can run spatial lookup between UniversalBitwise passes.

Query affinity is read from the frame word at queryWordIdx; results are
written to destWordIdx (typically a general register holding fetched data).
*/
const MemoryLoadPendingMagic uint64 = 0x4D4D4C44

/*
SetMemoryLoadPending records a pending memory load in-band. argA and argB are
clamped to valid word indices by the caller.
*/
func SetMemoryLoadPending(frame *[128]uint64, queryWordIdx, destWordIdx int) {

	if frame == nil {
		return
	}

	idx := core.Cfg.Value.Region.State.Index
	accum := core.Cfg.Value.Region.State.Accumulator

	if idx < 0 || idx >= len(frame) || accum < 0 || accum >= len(frame) {
		return
	}

	if queryWordIdx < 0 {
		queryWordIdx = 0
	}

	if queryWordIdx > 127 {
		queryWordIdx = 127
	}

	if destWordIdx < 0 {
		destWordIdx = 0
	}

	if destWordIdx > 127 {
		destWordIdx = 127
	}

	frame[idx] = MemoryLoadPendingMagic
	frame[accum] = uint64(queryWordIdx&0x7F) | (uint64(destWordIdx&0x7F) << 8)
}

/*
ProcessMemoryLoadRequests drains pending load markers. resolve receives the
query uint64 taken from the frame and returns a neighbor Value (only
Affinity and caller-chosen words matter). The hook returns ok false when no
neighbor is available.
*/
func ProcessMemoryLoadRequests(
	frames []unsafe.Pointer,
	resolve func(query uint64) (neighbor Value, ok bool),
) {

	if len(frames) == 0 || resolve == nil {
		return
	}

	idxWord := core.Cfg.Value.Region.State.Index
	accumWord := core.Cfg.Value.Region.State.Accumulator
	affinityWord := core.Cfg.Value.Region.Affinity.Start

	if idxWord < 0 || accumWord < 0 || affinityWord < 0 {
		return
	}

	for _, ptr := range frames {
		if ptr == nil {
			continue
		}

		frame := (*[128]uint64)(ptr)
		if idxWord >= len(frame) || accumWord >= len(frame) {
			continue
		}

		if frame[idxWord] != MemoryLoadPendingMagic {
			continue
		}

		pack := frame[accumWord]
		queryIdx := int(pack & 0x7F)
		destIdx := int((pack >> 8) & 0x7F)

		if queryIdx >= len(frame) || destIdx >= len(frame) {
			frame[idxWord] = 0
			frame[accumWord] = 0

			continue
		}

		query := frame[queryIdx]
		neighbor, ok := resolve(query)
		if !ok {
			frame[idxWord] = 0
			frame[accumWord] = 0

			continue
		}

		if affinityWord < len(neighbor) {
			frame[destIdx] = neighbor[affinityWord]
		}

		frame[idxWord] = 0
		frame[accumWord] = 0
	}
}
