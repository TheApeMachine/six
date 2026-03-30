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
	t.Skip("CPU learn-routing assertions drift from the current in-band primitive execution path; covered by primitive tests and targeted accumulator CPU tests")
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

	if got, want := a[payloadProgramWordStart()], firmwareWord(core.FirmwareTypeBuild, 0); got != want {
		t.Fatalf("build payload word mismatch: got %#x want %#x", got, want)
	}
	if got, want := a[core.Cfg.FW], core.FirmwareRegisterLearn; got != want {
		t.Fatalf("build should sequence to learn: got %d want %d", got, want)
	}
	if got := a[core.Cfg.RegPC]; got != 0 {
		t.Fatalf("build should arm next firmware load at pc=0, got %d", got)
	}
}

func TestBuildPayloadCanBeReplacedByLearn(t *testing.T) {
	be := NewBackend()
	var a, b [128]uint64

	installFirmware(&a, core.FirmwareTypeBootloader)
	a[core.Cfg.RegPC] = 0
	a[core.Cfg.FW] = 0

	if err := be.UniversalBitwise(unsafe.Pointer(&a), unsafe.Pointer(&b), 1); err != nil {
		t.Fatal(err)
	}
	if err := be.UniversalBitwise(unsafe.Pointer(&a), unsafe.Pointer(&b), 1); err != nil {
		t.Fatal(err)
	}
	if got, want := a[payloadProgramWordStart()], firmwareWord(core.FirmwareTypeBuild, 0); got != want {
		t.Fatalf("build payload word mismatch before learn replacement: got %#x want %#x", got, want)
	}

	if err := be.UniversalBitwise(unsafe.Pointer(&a), unsafe.Pointer(&b), 1); err != nil {
		t.Fatal(err)
	}
	if got, want := a[payloadProgramWordStart()], firmwareWord(core.FirmwareTypeLearn, 0); got != want {
		t.Fatalf("learn payload word mismatch after replacement: got %#x want %#x", got, want)
	}
}

func TestBuildUsesAffinityOverlapAsFeatureSignal(t *testing.T) {
	be := NewBackend()

	tests := []struct {
		name       string
		seedTokens [6]uint64
		self       uint64
		partner    uint64
		wantSignal uint64
		wantDelta  uint64
		wantTokens [6]uint64
	}{
		{
			name:       "shared affinity raises feature bit",
			seedTokens: [6]uint64{0x55, 0x11, 0x22, 0x33, 0x44, 0x66},
			self:       0b10110100,
			partner:    0b00110110,
			wantSignal: 0b00110100,
			wantDelta:  0b10000010,
			wantTokens: [6]uint64{0xD7, 0x93, 0xA0, 0xB1, 0xC6, 0xE4},
		},
		{
			name:       "disjoint affinity leaves feature clear",
			seedTokens: [6]uint64{0x33, 0x0F, 0xF0, 0xAA, 0x55, 0x99},
			self:       0b11000000,
			partner:    0b00110000,
			wantSignal: 0,
			wantDelta:  0b11110000,
			wantTokens: [6]uint64{0xC3, 0xFF, 0x00, 0x5A, 0xA5, 0x69},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var a, b [128]uint64
			anchorWords := []int{core.Cfg.TokenIndex, core.Cfg.TokenIndex + 7, core.Cfg.TokenIndex + 14, core.Cfg.TokenIndex + 21, core.Cfg.TokenIndex + 28, core.Cfg.TokenIndex + 35}

			installFirmware(&a, core.FirmwareTypeBuild)
			for i, idx := range anchorWords {
				a[idx] = tc.seedTokens[i]
			}
			a[core.Cfg.AffinityIndex] = tc.self
			b[core.Cfg.AffinityIndex] = tc.partner

			if err := be.UniversalBitwise(unsafe.Pointer(&a), unsafe.Pointer(&b), 1); err != nil {
				t.Fatal(err)
			}

			if got := a[core.Cfg.R6]; got != tc.wantSignal {
				t.Fatalf("feature signal mismatch: got %#x want %#x", got, tc.wantSignal)
			}
			if got := a[core.Cfg.StateAccumulator]; got != tc.wantDelta {
				t.Fatalf("accumulator delta mismatch: got %#x want %#x", got, tc.wantDelta)
			}
			for i, idx := range anchorWords {
				if got := a[idx]; got != tc.wantTokens[i] {
					t.Fatalf("anchor token %d mismatch: got %#x want %#x", i, got, tc.wantTokens[i])
				}
			}
			if got, want := a[core.Cfg.FW], core.FirmwareRegisterLearn; got != want {
				t.Fatalf("build should sequence to learn: got %d want %d", got, want)
			}
			if got := a[core.Cfg.RegPC]; got != 0 {
				t.Fatalf("build should arm next firmware load at pc=0, got %d", got)
			}
		})
	}
}

func TestLearnWeavesSequenceIntoAccumulator(t *testing.T) {
	be := NewBackend()
	var a, b [128]uint64

	installFirmware(&a, core.FirmwareTypeLearn)
	a[core.Cfg.StateSequence] = 1
	a[core.Cfg.StateAccumulator] = 0x82
	a[core.Cfg.R6] = 1

	if err := be.UniversalBitwise(unsafe.Pointer(&a), unsafe.Pointer(&b), 1); err != nil {
		t.Fatal(err)
	}

	if got, want := a[core.Cfg.StateSequence], uint64(0x8000000000000000); got != want {
		t.Fatalf("sequence mismatch: got %#x want %#x", got, want)
	}
	if got, want := a[core.Cfg.StateAccumulator], uint64(0x8000000000000082); got != want {
		t.Fatalf("accumulator mismatch: got %#x want %#x", got, want)
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

	for i, w := 0, uint64(payloadProgramWordStart()); i < len(core.Cfg.Firmware[core.FirmwareTypeViral]) && int(w) < len(a); i, w = i+2, w+1 {
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
		a[core.Cfg.RegPC] = payloadProgramPCStart()
		if err := be.UniversalBitwise(unsafe.Pointer(&a), unsafe.Pointer(&c), 1); err != nil {
			b.Fatal(err)
		}
	}
}
