package metal

import (
	"context"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/primitive"
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

func installSlot(frame *primitive.Value, slot int, instr uint32) {
	wordIdx := core.Cfg.Value.Region.Program.Start + slot/2
	shift := uint((slot % 2) * 32)
	mask := uint64(0xFFFFFFFF) << shift
	frame[wordIdx] = (frame[wordIdx] &^ mask) | (uint64(instr) << shift)
}

func setupTestConfig() {
	core.Cfg.Value.Region.Program.Start = 16
	core.Cfg.Value.Region.Program.Bits = 512
	core.Cfg.Value.Region.Signals.Start = 24
	core.Cfg.Value.Region.Signals.Bits = 512
}

func TestAvailable(t *testing.T) {
	if Available() == 0 {
		t.Skip("Metal backend unavailable in this environment")
	}

	Convey("Given the Metal backend", t, func() {
		So(Available(), ShouldBeGreaterThan, 0)
	})
}

func TestUniversalBitwiseUsesSelfOnly32BitProgram(t *testing.T) {
	core.PreserveGlobalConfig(t)

	Convey("Given a Metal backend and a single in-band slot program", t, func() {
		setupTestConfig()

		if Available() == 0 {
			t.Skip("Metal backend unavailable")
		}

		backend := NewBackend(0, BackendWithObserver(nil))

		frame := primitive.AllocValue()
		So(frame, ShouldNotBeNil)

		defer primitive.FreeValue(frame)

		frame[0] = 0xAAAAAAAAAAAAAAAA
		frame[1] = 0xCCCCCCCCCCCCCCCC

		frame[reservedProbeWordIndexA] = 11
		frame[reservedProbeWordIndexB] = 42

		installSlot(frame, 0, encode32(0x6, 0, 1))

		idx, ok := primitive.ArenaIndex(frame)
		So(ok, ShouldBeTrue)

		err := backend.Execute([]uint32{idx})

		So(err, ShouldBeNil)
		So(frame[1], ShouldEqual, uint64(0xCCCCCCCCCCCCCCCC))
		So(frame[reservedProbeWordIndexA], ShouldEqual, uint64(11))
		So(frame[reservedProbeWordIndexB], ShouldEqual, uint64(42))
	})
}

func TestBackendExecuteGeometric(t *testing.T) {
	Convey("Given a Metal backend and a geometric opcode", t, func() {
		if Available() == 0 {
			t.Skip("Metal backend unavailable")
		}

		backend := NewBackend(0, BackendWithObserver(nil))

		frame := primitive.AllocValue()
		So(frame, ShouldNotBeNil)

		defer primitive.FreeValue(frame)

		left := geometry.Multivector{1, 2, 3, 4, 5, 6, 7, 8}
		right := geometry.Multivector{2, -1, 4, 0, 1, 3, -2, 5}
		expected := left.GeometricProduct(right)

		frame[kernel.ProgramStartWord] = kernel.OpcodeGeometricCompose
		writeMetalTestMultivector(frame, kernel.ContextStartWord, left)
		writeMetalTestMultivector(frame, kernel.GradientStartWord, right)

		idx, ok := primitive.ArenaIndex(frame)
		So(ok, ShouldBeTrue)

		err := backend.Execute([]uint32{idx})

		So(err, ShouldBeNil)
		So(readMetalTestMultivector(frame, kernel.SignalsStartWord), ShouldResemble, expected)
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

func BenchmarkBackendExecuteGeometric(b *testing.B) {
	if Available() == 0 {
		b.Skip("Metal backend unavailable")
	}

	backend := NewBackend(0, BackendWithObserver(nil))
	frame := primitive.AllocValue()

	writeMetalTestMultivector(
		frame,
		kernel.ContextStartWord,
		geometry.Multivector{1, 2, 3, 4, 5, 6, 7, 8},
	)
	writeMetalTestMultivector(
		frame,
		kernel.GradientStartWord,
		geometry.Multivector{2, -1, 4, 0, 1, 3, -2, 5},
	)

	frame[kernel.ProgramStartWord] = kernel.OpcodeGeometricCompose
	idx, ok := primitive.ArenaIndex(frame)
	if !ok {
		b.Fatal("benchmark frame not in arena")
	}

	b.ReportAllocs()

	for b.Loop() {
		_ = backend.Execute([]uint32{idx})
	}

	primitive.FreeValue(frame)
}

func writeMetalTestMultivector(frame *primitive.Value, start int, mv geometry.Multivector) {
	*(*geometry.Multivector)(unsafe.Pointer(&(*frame)[start])) = mv
}

func readMetalTestMultivector(frame *primitive.Value, start int) geometry.Multivector {
	return *(*geometry.Multivector)(unsafe.Pointer(&(*frame)[start]))
}
