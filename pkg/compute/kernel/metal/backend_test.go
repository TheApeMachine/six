package metal

import (
	"context"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
)

/*
reservedProbeWordIndexA/B are arbitrary frame indices in the untouched
region of the test layout; UniversalBitwise must not clobber them when
only slots inside the programmed region are active.
*/
const (
	reservedProbeWordIndexA = 50
	reservedProbeWordIndexB = 60
)

func encode32(op uint8, src, dst int) uint32 {
	return uint32(op&0xF) | uint32(src&0x3FFF)<<4 | uint32(dst&0x3FFF)<<18
}

func installSlot(frame *[128]uint64, slot int, instr uint32) {
	wordIdx := core.Cfg.Value.Region.Program.Start + slot/2
	shift := uint((slot % 2) * 32)
	mask := uint64(0xFFFFFFFF) << shift
	frame[wordIdx] = (frame[wordIdx] &^ mask) | (uint64(instr) << shift)
}

func setupTestConfig() {
	core.Cfg.Value.Region.Program.Start = 8
	core.Cfg.Value.Region.Program.Bits = 512
	core.Cfg.Value.Region.Signals.Start = 16
	core.Cfg.Value.Region.Signals.Bits = 512
}

func TestAvailable(t *testing.T) {
	Convey("Given the Metal backend", t, func() {
		count := Available()
		So(count, ShouldBeGreaterThan, 0)
	})
}

func TestUniversalBitwiseUsesSelfOnly32BitProgram(t *testing.T) {
	Convey("Given a Metal backend and a single in-band slot program", t, func() {
		setupTestConfig()

		if Available() == 0 {
			t.Skip("Metal backend unavailable")
		}

		backend := NewBackend(0, BackendWithObserver(nil))

		var frame [128]uint64
		frame[0] = 0xAAAAAAAAAAAAAAAA
		frame[1] = 0xCCCCCCCCCCCCCCCC

		frame[reservedProbeWordIndexA] = 11
		frame[reservedProbeWordIndexB] = 42

		installSlot(&frame, 0, encode32(0x6, 0, 1))

		err := backend.Execute([]unsafe.Pointer{unsafe.Pointer(&frame)})

		So(err, ShouldBeNil)
		So(frame[1], ShouldEqual, uint64(0xCCCCCCCCCCCCCCCC))
		So(frame[reservedProbeWordIndexA], ShouldEqual, uint64(11))
		So(frame[reservedProbeWordIndexB], ShouldEqual, uint64(42))
	})
}

func TestSchedule(t *testing.T) {
	Convey("Schedule executes the supplied job", t, func() {
		backend := NewBackend(0)
		called := false

		err := backend.Schedule(func(ctx context.Context) error {
			called = true
			return nil
		})

		So(err, ShouldBeNil)
		So(called, ShouldBeTrue)
	})
}
