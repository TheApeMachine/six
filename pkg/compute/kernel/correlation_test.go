package kernel

import (
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
)

func TestCorrelationKeyvalsFlat(t *testing.T) {
	t.Parallel()

	Convey("Given a nil frame pointer", t, func() {
		So(CorrelationKeyvalsFlat(nil), ShouldBeNil)
	})

	Convey("Given a populated Value frame", t, func() {
		var frame [128]uint64

		frame[ValueIDWord] = 42
		frame[SchedulingNextProgramWord] = 7
		frame[FrameMetaLowWord] = 8
		frame[FrameMetaResidencyWord] = 9

		kv := CorrelationKeyvalsFlat(unsafe.Pointer(&frame[0]))

		So(kv, ShouldResemble, []any{
			"value_id", uint64(42),
			"sched_next", uint64(7),
			"frame_meta_lo", uint64(8),
			"frame_residency", uint64(9),
		})
	})
}

func BenchmarkCorrelationKeyvalsFlat(b *testing.B) {
	var frame [128]uint64

	frame[ValueIDWord] = 1
	ptr := unsafe.Pointer(&frame[0])

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_ = CorrelationKeyvalsFlat(ptr)
	}
}
