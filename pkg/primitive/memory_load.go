package primitive

import (
	"unsafe"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
)

/*
MemoryLoadPendingMagic is written to State.Index by LGPXMemoryLoadMark so the
host can run spatial lookup between UniversalBitwise passes.

Query affinity is read from the frame word at queryWordIdx; results are
written to destWordIdx (typically a general register holding fetched data).
*/
const MemoryLoadPendingMagic uint64 = 0x4D4D4C44

/*
MemoryLoadEnqueueMagic selects active LSM fetch: neighbors are cloned and sent
to the PRIORITY queue instead of inlining a single affinity word.
*/
const MemoryLoadEnqueueMagic uint64 = 0x4D4D4C45

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
		errnie.Warn(
			"primitive.SetMemoryLoadPending: clamped queryWordIdx",
			"from", queryWordIdx,
			"to", 0,
		)
		queryWordIdx = 0
	}

	if queryWordIdx > 127 {
		errnie.Warn(
			"primitive.SetMemoryLoadPending: clamped queryWordIdx",
			"from", queryWordIdx,
			"to", 127,
		)
		queryWordIdx = 127
	}

	if destWordIdx < 0 {
		errnie.Warn(
			"primitive.SetMemoryLoadPending: clamped destWordIdx",
			"from", destWordIdx,
			"to", 0,
		)
		destWordIdx = 0
	}

	if destWordIdx > 127 {
		errnie.Warn(
			"primitive.SetMemoryLoadPending: clamped destWordIdx",
			"from", destWordIdx,
			"to", 127,
		)
		destWordIdx = 127
	}

	frame[idx] = MemoryLoadPendingMagic
	frame[accum] = uint64(queryWordIdx&0x7F) | (uint64(destWordIdx&0x7F) << 8)
}

/*
SetMemoryLoadActiveFetchPending marks the frame for active fetch: the host
reads queryWordIdx, resolves many neighbors, and enqueues each clone on PRIORITY.
*/
func SetMemoryLoadActiveFetchPending(frame *[128]uint64, queryWordIdx int) {

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

	frame[idx] = MemoryLoadEnqueueMagic
	frame[accum] = uint64(queryWordIdx & 0x7F)
}

/*
ProcessMemoryLoadRequests drains pending load markers. When inline is non-nil,
MemoryLoadPendingMagic performs the single-word copy. When enqueue and
priorityEnqueue are non-nil, MemoryLoadEnqueueMagic clones each neighbor
(omitting the requester's ValueID) and schedules PRIORITY execution.
*/
func ProcessMemoryLoadRequests(
	frames []unsafe.Pointer,
	inline func(query uint64) (neighbor Value, ok bool),
	enqueue func(query uint64) []Value,
	priorityEnqueue func(unsafe.Pointer),
) {

	if len(frames) == 0 {
		return
	}

	idxWord := core.Cfg.Value.Region.State.Index
	accumWord := core.Cfg.Value.Region.State.Accumulator
	affinityWord := core.Cfg.Value.Region.Affinity.Start
	idWord := core.Cfg.Value.Region.ID.Start

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

		magic := frame[idxWord]

		if magic != MemoryLoadPendingMagic && magic != MemoryLoadEnqueueMagic {
			continue
		}

		if magic == MemoryLoadEnqueueMagic {
			if enqueue == nil || priorityEnqueue == nil {
				frame[idxWord] = 0
				frame[accumWord] = 0

				continue
			}

			queryIdx := int(frame[accumWord] & 0x7F)

			if queryIdx >= len(frame) {
				frame[idxWord] = 0
				frame[accumWord] = 0

				continue
			}

			query := frame[queryIdx]
			reqID := uint64(0)

			if idWord >= 0 && idWord < len(frame) {
				reqID = frame[idWord]
			}

			frame[idxWord] = 0
			frame[accumWord] = 0

			neighbors := enqueue(query)
			if len(neighbors) == 0 {
				continue
			}

			for index := range neighbors {
				nv := &neighbors[index]

				if idWord >= 0 && idWord < len(*nv) && reqID != 0 &&
					(*nv)[idWord] == reqID {
					continue
				}

				cloned := ClonePooledFrame(nv)
				if cloned == nil {
					continue
				}

				priorityEnqueue(unsafe.Pointer(cloned))
			}

			continue
		}

		if inline == nil {
			frame[idxWord] = 0
			frame[accumWord] = 0

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
		neighbor, ok := inline(query)
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
