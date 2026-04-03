package primitive

import (
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
)

func TestProcessMemoryLoadRequests(t *testing.T) {

	Convey("Pending magic triggers resolve and clears state words", t, func() {
		savedIdx := core.Cfg.Value.Region.State.Index
		savedAccum := core.Cfg.Value.Region.State.Accumulator
		savedAff := core.Cfg.Value.Region.Affinity.Start
		defer func() {
			core.Cfg.Value.Region.State.Index = savedIdx
			core.Cfg.Value.Region.State.Accumulator = savedAccum
			core.Cfg.Value.Region.Affinity.Start = savedAff
		}()

		core.Cfg.Value.Region.State.Index = 60
		core.Cfg.Value.Region.State.Accumulator = 62
		core.Cfg.Value.Region.Affinity.Start = 63

		var frame [128]uint64
		frame[72] = 0xfeedbeef
		SetMemoryLoadPending(&frame, 72, 73)

		var neighbor Value
		neighbor[63] = 0x1122334455667788

		ProcessMemoryLoadRequests(
			[]unsafe.Pointer{unsafe.Pointer(&frame)},
			func(query uint64) (Value, bool) {
				if query != 0xfeedbeef {
					return Value{}, false
				}

				return neighbor, true
			},
		)

		So(frame[73], ShouldEqual, neighbor[63])
		So(frame[60], ShouldEqual, uint64(0))
		So(frame[62], ShouldEqual, uint64(0))
	})
}
