package compute

import (
	"testing"
	"unsafe"

	"github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/stepwise"
)

func TestBackendUniversalBitwiseEmbeddedStepwise(t *testing.T) {
	convey.Convey("Given a frame with a stepwise band header", t, func() {
		backend := NewBackgroundBackend(WithBatchSize(1))
		if backend == nil {
			t.Fatal("NewBackgroundBackend returned nil")
		}
		defer backend.Close()

		convey.Convey("When UniversalBitwise runs embedded AND with operand from B", func() {
			var a, b [stepwise.FrameWords]uint64

			base := stepwise.EmbeddedProgramBase()
			n := stepwise.EmbeddedStepCount()

			a[64] = 0xC
			b[64] = 0xA
			a[base] = stepwise.PackEmbeddedHeader(uint16(n - 1))
			a[base+1] = stepwise.EncodeStepFrames(0x1, 64, 64, 66, false, true)

			for fill := 2; fill < n; fill++ {
				a[base+fill] = stepwise.EncodeStep(0x3, 66, 66, 66)
			}

			err := backend.UniversalBitwise(unsafe.Pointer(&a), unsafe.Pointer(&b))

			convey.So(err, convey.ShouldBeNil)
			convey.So(a[66], convey.ShouldEqual, uint64(0xC&0xA))
		})
	})
}
