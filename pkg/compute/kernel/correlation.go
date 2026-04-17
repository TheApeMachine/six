package kernel

import "unsafe"

// Frame words used for diagnostics when a kernel backend reports an error.
// Layout matches pkg/core/config.go defaults (scheduling next word, asset metadata).
const (
	SchedulingNextProgramWord = 117
	FrameMetaLowWord          = 118
	FrameMetaResidencyWord    = 119
	ValueIDWord               = 122
)

// CorrelationKeyvalsFlat returns alternating key/value pairs for observer logging
// (value id, scheduler word, frame metadata) extracted from a Value frame pointer.
func CorrelationKeyvalsFlat(ptr unsafe.Pointer) []any {
	if ptr == nil {
		return nil
	}

	f := (*[128]uint64)(ptr)

	return []any{
		"value_id", f[ValueIDWord],
		"sched_next", f[SchedulingNextProgramWord],
		"frame_meta_lo", f[FrameMetaLowWord],
		"frame_residency", f[FrameMetaResidencyWord],
	}
}
