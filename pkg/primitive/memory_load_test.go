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
			nil,
			nil,
		)

		So(frame[73], ShouldEqual, neighbor[63])
		So(frame[60], ShouldEqual, uint64(0))
		So(frame[62], ShouldEqual, uint64(0))
	})
}

func TestProcessMemoryLoadActiveFetch(t *testing.T) {

	Convey("MemoryLoadEnqueueMagic clones neighbors onto PRIORITY sink", t, func() {
		savedIdx := core.Cfg.Value.Region.State.Index
		savedAccum := core.Cfg.Value.Region.State.Accumulator
		savedID := core.Cfg.Value.Region.ID.Start
		defer func() {
			core.Cfg.Value.Region.State.Index = savedIdx
			core.Cfg.Value.Region.State.Accumulator = savedAccum
			core.Cfg.Value.Region.ID.Start = savedID
		}()

		core.Cfg.Value.Region.State.Index = 60
		core.Cfg.Value.Region.State.Accumulator = 62
		core.Cfg.Value.Region.ID.Start = 5

		var frame [128]uint64
		frame[72] = 0xcafe
		frame[5] = 0x100

		SetMemoryLoadActiveFetchPending(&frame, 72)

		var enqueued int

		ProcessMemoryLoadRequests(
			[]unsafe.Pointer{unsafe.Pointer(&frame)},
			nil,
			func(query uint64) []Value {
				if query != 0xcafe {
					return nil
				}

				var n Value
				n[5] = 0x200

				return []Value{n}
			},
			func(p unsafe.Pointer) {
				enqueued++
				fr := (*Value)(p)
				So((*fr)[5], ShouldEqual, uint64(0x200))
				valuePool.Put(fr)
			},
		)

		So(frame[60], ShouldEqual, uint64(0))
		So(frame[62], ShouldEqual, uint64(0))
		So(enqueued, ShouldEqual, 1)
	})
}
