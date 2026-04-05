package primitive

import (
	"context"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/core"
)

// Scratch words live in reserved space (words 24+) to avoid overlapping
// token, affinity, or program regions.
const (
	inbandWordSelf         = 40
	inbandWordPartner      = 41
	inbandWordSupport      = 42
	inbandWordPromoteWarm  = 43
	inbandWordPromoteHot   = 44
	inbandWordPromoteCarry = 45
	inbandWordSuppress     = 46
	inbandWordSuppressWarm = 47
	inbandWordSuppressHot  = 48
	inbandWordSuppressCarry = 49
	inbandWordProjection   = 50
)

func setupInBandValueTest(tb testing.TB) {
	tb.Helper()

	original := *core.Cfg

	tb.Cleanup(func() {
		*core.Cfg = original
	})

	core.Cfg.Value.Words = 128
	core.Cfg.Value.Bytes = 1024
	core.Cfg.Value.Region.Tokens.Start = 0
	core.Cfg.Value.Region.Tokens.Bits = 512
	core.Cfg.Value.Region.Affinity.Start = 8
	core.Cfg.Value.Region.Affinity.Bits = 512
	core.Cfg.Value.Region.Program.Start = 16
	core.Cfg.Value.Region.Program.Bits = 512
	core.Cfg.Value.Region.Reserved.Start = 24
	core.Cfg.Value.Region.Reserved.Bits = 6464
	core.Cfg.Value.Region.Prev.Start = 125
	core.Cfg.Value.Region.Next.Start = 126
	core.Cfg.Value.Region.ID.Start = 127
	core.Cfg.Value.Region.State.Index = 24
	core.Cfg.Value.Region.State.Sequence = 25
	core.Cfg.Value.Region.State.Accumulator = 26
}

func encode32(op uint8, src, dst int) uint32 {
	return uint32(op&0xF) | uint32(src&0x3FFF)<<4 | uint32(dst&0x3FFF)<<18
}

func installSlot(value *Value, slot int, instr uint32) {
	wordIndex := core.Cfg.Value.Region.Program.Start + slot/2
	shift := uint((slot % 2) * 32)
	mask := uint64(0xFFFFFFFF) << shift
	value[wordIndex] = (value[wordIndex] &^ mask) | uint64(instr)<<shift
}

func installInBandBiasProgram(value *Value) {
	// support = xnor(self, partner)
	installSlot(value, 0, encode32(0x3, inbandWordPartner, inbandWordSupport))
	installSlot(value, 1, encode32(0x9, inbandWordSelf, inbandWordSupport))

	// promote warm/hot hysteresis:
	// carry = promoteWarm & support
	// promoteWarm |= support
	// promoteHot |= carry
	installSlot(value, 2, encode32(0x3, inbandWordPromoteWarm, inbandWordPromoteCarry))
	installSlot(value, 3, encode32(0x1, inbandWordSupport, inbandWordPromoteCarry))
	installSlot(value, 4, encode32(0x7, inbandWordSupport, inbandWordPromoteWarm))
	installSlot(value, 5, encode32(0x7, inbandWordPromoteCarry, inbandWordPromoteHot))

	// suppress warm/hot hysteresis:
	// suppress = xor(self, partner)
	// carry = suppressWarm & suppress
	// suppressWarm |= suppress
	// suppressHot |= carry
	installSlot(value, 6, encode32(0x3, inbandWordPartner, inbandWordSuppress))
	installSlot(value, 7, encode32(0x6, inbandWordSelf, inbandWordSuppress))
	installSlot(value, 8, encode32(0x3, inbandWordSuppressWarm, inbandWordSuppressCarry))
	installSlot(value, 9, encode32(0x1, inbandWordSuppress, inbandWordSuppressCarry))
	installSlot(value, 10, encode32(0x7, inbandWordSuppress, inbandWordSuppressWarm))
	installSlot(value, 11, encode32(0x7, inbandWordSuppressCarry, inbandWordSuppressHot))

	// projection = promoteHot & ^suppressHot
	installSlot(value, 12, encode32(0x3, inbandWordPromoteHot, inbandWordProjection))
	installSlot(value, 13, encode32(0x4, inbandWordSuppressHot, inbandWordProjection))

	// Affinity is a projection sink, not the accumulator itself.
	installSlot(value, 14, encode32(0x3, inbandWordProjection, core.Cfg.Value.Region.Affinity.Start))
}

func runInBandProgram(t *testing.T, value *Value, n int) {
	t.Helper()

	backend := cpu.NewBackend(context.Background())
	ptrs := []unsafe.Pointer{unsafe.Pointer(value)}

	for iteration := 0; iteration < n; iteration++ {
		err := backend.UniversalBitwise(ptrs)

		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestInBandBiasAccumulatesInProgramRegion(t *testing.T) {
	setupInBandValueTest(t)

	Convey("Given one Value carrying self bits, partner bits, bias planes, and program", t, func() {
		value, err := NewValue(nil)
		So(err, ShouldBeNil)
		defer value.Close()

		value[inbandWordSelf] = 0xAAAAAAAAAAAAAAAA
		value[inbandWordPartner] = 0xCCCCCCCCCCCCCCCC
		installInBandBiasProgram(value)

		Convey("One encounter should only warm the support and suppress planes", func() {
			runInBandProgram(t, value, 1)

			So(value[inbandWordSupport], ShouldEqual, uint64(0x9999999999999999))
			So(value[inbandWordPromoteWarm], ShouldEqual, uint64(0x9999999999999999))
			So(value[inbandWordPromoteHot], ShouldEqual, uint64(0))
			So(value[inbandWordSuppress], ShouldEqual, uint64(0x6666666666666666))
			So(value[inbandWordSuppressWarm], ShouldEqual, uint64(0x6666666666666666))
			So(value[inbandWordSuppressHot], ShouldEqual, uint64(0))
		})

		Convey("A second identical encounter should promote both planes into hot bias", func() {
			runInBandProgram(t, value, 2)

			So(value[inbandWordPromoteWarm], ShouldEqual, uint64(0x9999999999999999))
			So(value[inbandWordPromoteHot], ShouldEqual, uint64(0x9999999999999999))
			So(value[inbandWordSuppressWarm], ShouldEqual, uint64(0x6666666666666666))
			So(value[inbandWordSuppressHot], ShouldEqual, uint64(0x6666666666666666))
		})
	})
}

func TestInBandBiasProjectionWritesAffinityAsDerivedState(t *testing.T) {
	setupInBandValueTest(t)

	Convey("Given the same in-band bias program", t, func() {
		value, err := NewValue(nil)
		So(err, ShouldBeNil)
		defer value.Close()

		value[inbandWordSelf] = 0xAAAAAAAAAAAAAAAA
		value[inbandWordPartner] = 0xCCCCCCCCCCCCCCCC
		installInBandBiasProgram(value)

		Convey("Affinity should receive only the derived projection, not the raw accumulators", func() {
			runInBandProgram(t, value, 2)

			So(value[inbandWordProjection], ShouldEqual, uint64(0x9999999999999999))
			So(value[core.Cfg.Value.Region.Affinity.Start], ShouldEqual, value[inbandWordProjection])
			So(value[core.Cfg.Value.Region.Affinity.Start], ShouldNotEqual, value[inbandWordSuppressWarm])
			So(value[core.Cfg.Value.Region.Affinity.Start], ShouldNotEqual, uint64(0))
		})
	})
}

func BenchmarkValue_InBandBias(b *testing.B) {
	setupInBandValueTest(b)

	value, err := NewValue(nil)

	if err != nil {
		b.Fatal(err)
	}

	defer value.Close()

	value[inbandWordSelf] = 0xAAAAAAAAAAAAAAAA
	value[inbandWordPartner] = 0xCCCCCCCCCCCCCCCC
	installInBandBiasProgram(value)

	backend := cpu.NewBackend(context.Background())
	ptrs := []unsafe.Pointer{unsafe.Pointer(value)}
	b.ResetTimer()

	for b.Loop() {
		if err := backend.UniversalBitwise(ptrs); err != nil {
			b.Fatal(err)
		}
	}
}
