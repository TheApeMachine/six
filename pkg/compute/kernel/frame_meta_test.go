package kernel

import (
	"testing"
	"unsafe"

	"sync/atomic"

	. "github.com/smartystreets/goconvey/convey"
)

func TestFrameMetaResidencyAndCorrelation(t *testing.T) {
	t.Parallel()

	Convey("EnsureFrameCorrelationSeq fills word 118 once", t, func() {
		var words [128]uint64
		ptr := unsafe.Pointer(&words[0])
		var seq atomic.Uint64

		EnsureFrameCorrelationSeq(&seq, ptr)
		So(FrameCorrelationID(ptr), ShouldEqual, 1)

		EnsureFrameCorrelationSeq(&seq, ptr)
		So(FrameCorrelationID(ptr), ShouldEqual, 1)
	})

	Convey("StampFrameResidency and ResidencySubstrateIndex roundtrip", t, func() {
		var words [128]uint64
		ptr := unsafe.Pointer(&words[0])

		StampFrameResidency(ptr, 2)
		So(ResidencySubstrateIndex(ptr), ShouldEqual, 2)
	})

	Convey("FrameProgramOpcode reads low nibble of program word 16", t, func() {
		var words [128]uint64
		ptr := unsafe.Pointer(&words[0])

		v := (*[128]uint64)(ptr)
		v[ProgramStartWord] = 0xA
		So(FrameProgramOpcode(ptr), ShouldEqual, 0xA)
	})
}
