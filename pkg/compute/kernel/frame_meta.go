package kernel

import (
	"strconv"
	"sync/atomic"
	"unsafe"
)

/*
Words 118–119 hold transport metadata (correlation id, residency tag).
They live in the reserved band before the graph-link words Prev/Next
at 120–121 so kernel stamping cannot clobber trie/Value graph fields.
*/
const (
	FrameMetaCorrelationWord = 118
	FrameMetaResidencyWord   = 119
)

/*
FrameProgramOpcode returns the 4-bit truth-table opcode in the program region
(ProgramStartWord), matching cpu/metal/cuda Execute decoders.
*/
func FrameProgramOpcode(frame unsafe.Pointer) uint8 {
	if frame == nil {
		return 0
	}

	frameWords := (*[128]uint64)(frame)

	return uint8(frameWords[ProgramStartWord] & 0xF)
}

/*
FrameCorrelationID returns the uint64 stamped at FrameMetaCorrelationWord.
*/
func FrameCorrelationID(frame unsafe.Pointer) uint64 {
	if frame == nil {
		return 0
	}

	frameWords := (*[128]uint64)(frame)

	return frameWords[FrameMetaCorrelationWord]
}

/*
EnsureFrameCorrelationSeq fills an empty correlation word from a monotonic
Backend-owned counter without touching program or batch metadata words.
*/
func EnsureFrameCorrelationSeq(seq *atomic.Uint64, frame unsafe.Pointer) {
	if frame == nil || seq == nil {
		return
	}

	frameWords := (*[128]uint64)(frame)

	if frameWords[FrameMetaCorrelationWord] != 0 {
		return
	}

	frameWords[FrameMetaCorrelationWord] = seq.Add(1)
}

/*
StampFrameResidency writes 1+substrateIndex into the low byte of the
residency word so the load balancer can compare against candidate slots.
Index must match the stable ordering used by pkg/compute Backend.states.
*/
func StampFrameResidency(frame unsafe.Pointer, substrateIndex int) {
	if frame == nil || substrateIndex < 0 {
		return
	}

	frameWords := (*[128]uint64)(frame)

	frameWords[FrameMetaResidencyWord] = uint64(substrateIndex) + 1
}

/*
ResidencySubstrateIndex returns -1 when unknown; otherwise the substrate
index from the last successful execute stamp.
*/
func ResidencySubstrateIndex(frame unsafe.Pointer) int {
	if frame == nil {
		return -1
	}

	frameWords := (*[128]uint64)(frame)
	tag := frameWords[FrameMetaResidencyWord] & 0xFF

	if tag == 0 {
		return -1
	}

	return int(tag) - 1
}

/*
FormatCorrelationDecimal renders id for structured logs and Elasticsearch
keyword fields (avoids JNA long overflow for unsigned space).
*/
func FormatCorrelationDecimal(id uint64) string {
	if id == 0 {
		return ""
	}

	return strconv.FormatUint(id, 10)
}

/*
CorrelationKeyvals returns key/value pairs to append for Observer.Error
when correlation id is non-zero.
*/
func CorrelationKeyvals(frame unsafe.Pointer) []any {
	id := FrameCorrelationID(frame)

	if id == 0 {
		return nil
	}

	return []any{"correlation_id", FormatCorrelationDecimal(id)}
}
