package cpu

import (
	"fmt"
	"os"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
)

func TestMain(m *testing.M) {
	viper.SetConfigFile("../../../../cmd/cfg/config.yml")
	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "cpu/backend_test: viper.ReadInConfig: %v\n", err)
		os.Exit(1)
	}
	if err := core.LoadValueConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "cpu/backend_test: core.LoadValueConfig: %v\n", err)
		os.Exit(1)
	}
	viper.Set("loglevel", "error")
	viper.Set("logging.trace.path", os.DevNull)
	loggingCfg, err := core.LoadLoggingConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cpu/backend_test: core.LoadLoggingConfig: %v\n", err)
		os.Exit(1)
	}
	errnie.InitLogger(loggingCfg)
	os.Exit(m.Run())
}

func encodeWriteRegInstr(op uint8, sc, dc uint16) uint64 {
	return uint64(uint32(op) | uint32(sc)<<4 | uint32(dc)<<18)
}

// TestUniversalBitwise exercises writeReg-style programs on [128]uint64 frames
// (the same memory layout as primitive.Value; cpu tests cannot import primitive
// without a compute import cycle).
func TestUniversalBitwise(t *testing.T) {
	Convey("UniversalBitwise performs bitwise operations on two Values", t, func() {
		be := NewBackend()
		var b [128]uint64

		Convey("OR merges a nonzero immediate into the destination (writeReg path)", func() {
			var a [128]uint64
			w := core.Cfg.ProgramIndex
			a[core.Cfg.RegPC] = 0
			a[core.Cfg.FW] = 0
			// Opcode 0x7 matches config `or: 0111`. Left must not use 0x3F80==0x3000 or
			// decode routes to execSpan; use a 14-bit immediate and zeroed dst.
			a[w] = encodeWriteRegInstr(0x7, 0x2B2C, uint16(0x3000|core.Cfg.R2))
			a[core.Cfg.R2] = 0
			So(be.UniversalBitwise(unsafe.Pointer(&a), unsafe.Pointer(&b), 1), ShouldBeNil)
			So(a[core.Cfg.R2], ShouldEqual, uint64(0x2B2C))
		})

		Convey("AND masks destination with a 14-bit immediate", func() {
			var a [128]uint64
			w := core.Cfg.ProgramIndex
			a[core.Cfg.RegPC] = 0
			a[core.Cfg.FW] = 0
			a[w] = encodeWriteRegInstr(0x1, 0x00F0, uint16(0x3000|core.Cfg.R2))
			a[core.Cfg.R2] = 0xABCD
			So(be.UniversalBitwise(unsafe.Pointer(&a), unsafe.Pointer(&b), 1), ShouldBeNil)
			So(a[core.Cfg.R2], ShouldEqual, uint64(0x00F0&0xABCD))
		})

		Convey("XOR combines a 14-bit immediate with the destination word", func() {
			var a [128]uint64
			w := core.Cfg.ProgramIndex
			a[core.Cfg.RegPC] = 0
			a[core.Cfg.FW] = 0
			// Opcode 0x6 matches config `xor: 0110`; immediate sc avoids the span decode path.
			a[w] = encodeWriteRegInstr(6, 0x00FF, uint16(0x3000|core.Cfg.R2))
			a[core.Cfg.R2] = 0x0F
			So(be.UniversalBitwise(unsafe.Pointer(&a), unsafe.Pointer(&b), 1), ShouldBeNil)
			So(a[core.Cfg.R2], ShouldEqual, uint64(0xF0))
		})

		Convey("zero immediate AND clears the destination word", func() {
			var a [128]uint64
			w := core.Cfg.ProgramIndex
			a[core.Cfg.RegPC] = 0
			a[core.Cfg.FW] = 0
			a[w] = encodeWriteRegInstr(0x1, 0, uint16(0x3000|core.Cfg.R3))
			a[core.Cfg.R3] = 0xFFFFFFFFFFFFFFFF
			So(be.UniversalBitwise(unsafe.Pointer(&a), unsafe.Pointer(&b), 1), ShouldBeNil)
			So(a[core.Cfg.R3], ShouldEqual, uint64(0))
		})
	})
}

func installFirmware(frame *[128]uint64, fw core.FirmwareType) {
	start := uint64(core.Cfg.ProgramIndex)
	for i, w := 0, start; i < len(core.Cfg.Firmware[fw]) && int(w) < len(frame); i, w = i+2, w+1 {
		v := uint64(core.Cfg.Firmware[fw][i])
		if i+1 < len(core.Cfg.Firmware[fw]) {
			v |= uint64(core.Cfg.Firmware[fw][i+1]) << 32
		}
		frame[w] = v
	}
}

func TestTombstonePropagation(t *testing.T) {
	be := NewBackend()
	var a, b [128]uint64

	installFirmware(&a, core.FirmwareTypeTombstone)
	a[core.Cfg.R6] = 1234

	if err := be.UniversalBitwise(unsafe.Pointer(&a), unsafe.Pointer(&b), 1); err != nil {
		t.Fatal(err)
	}

	if got := b[core.Cfg.R6]; got != 1234 {
		t.Fatalf("target id not propagated: got %d want 1234", got)
	}
	for i := core.Cfg.ProgramIndex + 4; i < len(a); i++ {
		if b[i] != a[i] {
			t.Fatalf("tombstone program word %d not propagated: got %#x want %#x", i, b[i], a[i])
		}
	}
}

func TestLearnFirmwareFitnessRouting(t *testing.T) {
	be := NewBackend()

	tests := []struct {
		name            string
		accumulatorInit uint64
		wantFW          uint64
		wantAccumulator uint64
	}{
		{
			name:            "novel edge chooses viral",
			accumulatorInit: 0x00,
			wantFW:          uint64(core.FirmwareTypeViral),
			wantAccumulator: 0x0F,
		},
		{
			name:            "stagnant edge chooses tombstone",
			accumulatorInit: 0xFF,
			wantFW:          uint64(core.FirmwareTypeTombstone),
			wantAccumulator: 0xFF,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var a, b [128]uint64

			installFirmware(&a, core.FirmwareTypeLearn)

			const (
				partnerID = uint64(0x0F)
				oldNextID = uint64(0x1234)
			)

			b[core.Cfg.ValueID] = partnerID
			a[core.Cfg.NextID] = oldNextID
			a[core.Cfg.PreviousID] = 0x9999
			a[core.Cfg.StateAccumulator] = tc.accumulatorInit

			if err := be.UniversalBitwise(unsafe.Pointer(&a), unsafe.Pointer(&b), 1); err != nil {
				t.Fatal(err)
			}

			if got := a[core.Cfg.FW]; got != tc.wantFW {
				t.Fatalf("fw mismatch: got %d want %d", got, tc.wantFW)
			}

			if got := a[core.Cfg.PreviousID]; got != oldNextID {
				t.Fatalf("PrevID mismatch: got %#x want %#x", got, oldNextID)
			}

			if got := a[core.Cfg.NextID]; got != partnerID {
				t.Fatalf("NextID mismatch: got %#x want %#x", got, partnerID)
			}

			if got := a[core.Cfg.StateAccumulator]; got != tc.wantAccumulator {
				t.Fatalf("accumulator mismatch: got %#x want %#x", got, tc.wantAccumulator)
			}
		})
	}
}

func firmwareWord(ft core.FirmwareType, wordIdx int) uint64 {
	prog := core.Cfg.Firmware[ft]
	i := wordIdx * 2
	if i >= len(prog) {
		return 0
	}
	v := uint64(prog[i])
	if i+1 < len(prog) {
		v |= uint64(prog[i+1]) << 32
	}
	return v
}

func TestBootloaderSequencesToBuild(t *testing.T) {
	be := NewBackend()
	var a, b [128]uint64

	installFirmware(&a, core.FirmwareTypeBootloader)
	a[core.Cfg.RegPC] = 0
	a[core.Cfg.FW] = 0

	if err := be.UniversalBitwise(unsafe.Pointer(&a), unsafe.Pointer(&b), 1); err != nil {
		t.Fatal(err)
	}

	if got, want := a[core.Cfg.FW], core.FirmwareRegisterBuild; got != want {
		t.Fatalf("bootloader fw mismatch: got %d want %d", got, want)
	}
	if got := a[core.Cfg.RegPC]; got != 0 {
		t.Fatalf("bootloader should arm next firmware load at pc=0, got %d", got)
	}

	if err := be.UniversalBitwise(unsafe.Pointer(&a), unsafe.Pointer(&b), 1); err != nil {
		t.Fatal(err)
	}

	if got, want := a[core.Cfg.ProgramIndex+int(core.UserProgramPCStart)], firmwareWord(core.FirmwareTypeBuild, 0); got != want {
		t.Fatalf("build payload word mismatch: got %#x want %#x", got, want)
	}
	if got, want := a[core.Cfg.FW], core.FirmwareRegisterLearn; got != want {
		t.Fatalf("build should sequence to learn: got %d want %d", got, want)
	}
	if got := a[core.Cfg.RegPC]; got != 0 {
		t.Fatalf("build should arm next firmware load at pc=0, got %d", got)
	}
}

func TestViralArmsSelfAndPartnerForLearn(t *testing.T) {
	be := NewBackend()
	var a, b [128]uint64

	installFirmware(&a, core.FirmwareTypeViral)
	a[core.Cfg.RegPC] = 0
	a[core.Cfg.FW] = 0

	if err := be.UniversalBitwise(unsafe.Pointer(&a), unsafe.Pointer(&b), 1); err != nil {
		t.Fatal(err)
	}

	if got, want := a[core.Cfg.FW], core.FirmwareRegisterLearn; got != want {
		t.Fatalf("self fw mismatch: got %d want %d", got, want)
	}
	if got := a[core.Cfg.RegPC]; got != 0 {
		t.Fatalf("self should be armed for next learn load at pc=0, got %d", got)
	}
	if got, want := b[core.Cfg.FW], core.FirmwareRegisterLearn; got != want {
		t.Fatalf("partner fw mismatch: got %d want %d", got, want)
	}
	if got := b[core.Cfg.RegPC]; got != 0 {
		t.Fatalf("partner should be armed for next learn load at pc=0, got %d", got)
	}
}

func BenchmarkUniversalBitwise(b *testing.B) {
	be := NewBackend()
	var a, c [128]uint64

	aWord := uint64(core.Cfg.ProgramIndex)
	for i, w := 0, aWord+core.UserProgramPCStart; i < len(core.Cfg.Firmware[core.FirmwareTypeViral]) && int(w) < len(a); i, w = i+2, w+1 {
		v := uint64(core.Cfg.Firmware[core.FirmwareTypeViral][i])
		if i+1 < len(core.Cfg.Firmware[core.FirmwareTypeViral]) {
			v |= uint64(core.Cfg.Firmware[core.FirmwareTypeViral][i+1]) << 32
		}
		a[w] = v
	}
	a[core.Cfg.R0] = 0
	a[core.Cfg.R1] = 5120
	a[core.Cfg.R2] = 8192
	a[core.Cfg.R3] = 1
	a[core.Cfg.R4] = 5120
	a[core.Cfg.R5] = 8192

	b.SetBytes(1024)
	b.ResetTimer()
	for b.Loop() {
		a[core.Cfg.RegPC] = core.UserProgramPCStart
		if err := be.UniversalBitwise(unsafe.Pointer(&a), unsafe.Pointer(&c), 1); err != nil {
			b.Fatal(err)
		}
	}
}
