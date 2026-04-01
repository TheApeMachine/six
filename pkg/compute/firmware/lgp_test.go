package firmware

import (
	"fmt"
	"math/rand"
	"os"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
)

func TestMain(m *testing.M) {
	viper.SetConfigFile("../../../cmd/cfg/config.yml")
	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "firmware_test: viper.ReadInConfig: %v\n", err)
		os.Exit(1)
	}
	viper.Set("loglevel", "error")
	viper.Set("logging.trace.path", os.DevNull)
	core.NewConfig()
	loggingCfg, _ := core.LoadLoggingConfig()
	errnie.InitLogger(loggingCfg)
	os.Exit(m.Run())
}

func TestIsIntron(t *testing.T) {
	Convey("Intron detection", t, func() {
		Convey("Zero instruction is an intron", func() {
			So(IsIntron(0), ShouldBeTrue)
		})

		Convey("Identity A with same src and dst is an intron", func() {
			instr := MakeIntron(uint16(core.Cfg.Value.Region.Registers.R0))
			So(IsIntron(instr), ShouldBeTrue)
		})

		Convey("XOR gate is not an intron", func() {
			instr := uint32(0x6) | (uint32(core.Cfg.Value.Region.Registers.R0) << 4) | (uint32(core.Cfg.Value.Region.Registers.R1) << 18)
			So(IsIntron(instr), ShouldBeFalse)
		})
	})
}

func TestInsertIntrons(t *testing.T) {
	Convey("Given a fresh frame", t, func() {
		var c [128]uint64

		Convey("InsertIntrons places introns at regular intervals", func() {
			InsertIntrons(&c, 4)

			start, last := PayloadLGPSpan()
			intronCount := 0

			for slot := 0; slot < start; slot++ {
				So(InstructionSlot(&c, slot), ShouldEqual, 0)
			}

			for slot := start; slot < last; slot++ {
				instr := InstructionSlot(&c, slot)
				if (slot-start)%5 == 4 {
					So(IsIntron(instr), ShouldBeTrue)
					intronCount++
				}
			}
			So(intronCount, ShouldBeGreaterThan, 0)
		})
	})
}

func TestTraceEffective(t *testing.T) {
	Convey("Given a program that writes to r6", t, func() {
		var c [128]uint64
		r0 := uint16(core.Cfg.Value.Region.Registers.R0)
		r6 := uint16(core.Cfg.Value.Region.Registers.R6)
		start := ProgramPayloadFirst32BitSlot()

		instr := uint32(0x6) | (uint32(r0) << 4) | (uint32(r6) << 18)
		SetInstructionSlot(&c, start, instr)

		Convey("TraceEffective marks that slot as effective", func() {
			mask := TraceEffective(&c)
			So(traceMaskHas(mask, 0), ShouldBeTrue)
		})
	})

	Convey("Given a program that only writes to r0 (not r6)", t, func() {
		var c [128]uint64
		r0 := uint16(core.Cfg.Value.Region.Registers.R0)
		r1 := uint16(core.Cfg.Value.Region.Registers.R1)
		start := ProgramPayloadFirst32BitSlot()

		instr := uint32(0x1) | (uint32(r1) << 4) | (uint32(r0) << 18)
		SetInstructionSlot(&c, start, instr)

		Convey("TraceEffective returns 0 (no r6 influence)", func() {
			mask := TraceEffective(&c)
			So(traceMaskHas(mask, 0), ShouldBeFalse)
		})
	})

	Convey("Given a write that reuses the existing r6 value as an input", t, func() {
		var c [128]uint64
		r0 := uint16(core.Cfg.Value.Region.Registers.R0)
		r1 := uint16(core.Cfg.Value.Region.Registers.R1)
		r6 := uint16(core.Cfg.Value.Region.Registers.R6)
		start := ProgramPayloadFirst32BitSlot()

		SetInstructionSlot(&c, start, uint32(0x6)|(uint32(r0)<<4)|(uint32(r6)<<18))
		SetInstructionSlot(&c, start+1, uint32(0x7)|(uint32(r1)<<4)|(uint32(r6)<<18))

		Convey("TraceEffective keeps both the original write and the update", func() {
			mask := TraceEffective(&c)
			So(traceMaskHas(mask, 0), ShouldBeTrue)
			So(traceMaskHas(mask, 1), ShouldBeTrue)
		})
	})
}

func TestHomologousCrossover(t *testing.T) {
	Convey("Given a donor with effective instructions and a blank recipient", t, func() {
		var donor, recipient [128]uint64
		r0 := uint16(core.Cfg.Value.Region.Registers.R0)
		r6 := uint16(core.Cfg.Value.Region.Registers.R6)
		start := ProgramPayloadFirst32BitSlot()

		donorInstr := uint32(0x6) | (uint32(r0) << 4) | (uint32(r6) << 18)
		SetInstructionSlot(&donor, start, donorInstr)

		rng := rand.New(rand.NewSource(42))
		HomologousCrossover(&recipient, &donor, rng)

		Convey("The recipient should receive the effective instruction", func() {
			got := InstructionSlot(&recipient, start)
			So(got, ShouldEqual, donorInstr)
		})
	})
}

func BenchmarkTraceEffective(b *testing.B) {
	var c [128]uint64
	start := ProgramPayloadFirst32BitSlot()
	r0 := uint16(core.Cfg.Value.Region.Registers.R0)
	r6 := uint16(core.Cfg.Value.Region.Registers.R6)

	for slot := 0; slot < 32; slot++ {
		op := uint32(0x6 + (slot % 2))
		SetInstructionSlot(&c, start+slot, op|(uint32(r0)<<4)|(uint32(r6)<<18))
	}

	b.ReportAllocs()

	for b.Loop() {
		_ = TraceEffective(&c)
	}
}
