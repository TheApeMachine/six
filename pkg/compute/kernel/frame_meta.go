package kernel

import (
	"strconv"
	"sync/atomic"
	"unsafe"
)

const (
	FrameMetaCorrelationWord = 118
	FrameMetaResidencyWord   = 119
)

func FrameCorrelationID(frame unsafe.Pointer) uint64 {
	if frame == nil {
		return 0
	}

	frameWords := (*[128]uint64)(frame)

	return frameWords[FrameMetaCorrelationWord]
}

/*
FrameProgramRawOpcode returns the low byte of the first program word.
*/
func FrameProgramRawOpcode(frame unsafe.Pointer) uint8 {
	if frame == nil {
		return 0
	}

	frameWords := (*[128]uint64)(frame)

	return uint8(frameWords[ProgramStartWord] & 0xFF)
}

/*
EnsureFrameCorrelationSeq fills an empty correlation word from a monotonic counter.
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

func FormatCorrelationDecimal(id uint64) string {
	if id == 0 {
		return ""
	}

	return strconv.FormatUint(id, 10)
}

func CorrelationKeyvals(frame unsafe.Pointer) []any {
	id := FrameCorrelationID(frame)

	if id == 0 {
		return nil
	}

	return []any{"correlation_id", FormatCorrelationDecimal(id)}
}

/*
ResidencySubstrateIndex returns -1 when unknown; otherwise the substrate index.
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
