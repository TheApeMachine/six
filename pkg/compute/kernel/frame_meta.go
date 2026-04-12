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

func FrameID(frame unsafe.Pointer) uint64 {
	if frame == nil {
		return 0
	}

	frameWords := (*[128]uint64)(frame)

	return frameWords[IDStartWord]
}

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

/*
CorrelationKV is one key/value pair for observer and logging hooks.
*/
type CorrelationKV struct {
	Key   string
	Value string
}

/*
CorrelationKeyvals returns correlation fields when the frame carries a
non-zero correlation word; otherwise nil.
*/
func CorrelationKeyvals(frame unsafe.Pointer) []CorrelationKV {
	id := FrameCorrelationID(frame)

	if id == 0 {
		return nil
	}

	return []CorrelationKV{{
		Key:   "correlation_id",
		Value: FormatCorrelationDecimal(id),
	}}
}

/*
CorrelationKeyvalsFlat expands CorrelationKeyvals into alternating key/value
any slices for variadic observer APIs.
*/
func CorrelationKeyvalsFlat(frame unsafe.Pointer) []any {
	pairs := CorrelationKeyvals(frame)

	if len(pairs) == 0 {
		return nil
	}

	out := make([]any, 0, len(pairs)*2)

	for _, pair := range pairs {
		out = append(out, pair.Key, pair.Value)
	}

	return out
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
