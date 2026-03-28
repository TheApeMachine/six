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

func TestUniversalBitwise(t *testing.T) {
	Convey("UniversalBitwise performs bitwise operations on two Values", t, func() {

	})
}

func installFirmware(frame *[128]uint64, fw core.FirmwareType) {
	start := uint64(core.Cfg.ProgramIndex)
	for i, w := 0, start; i < len(core.Cfg.Firmware[fw]) && int(w) < len(frame); i, w = i+2, w+1 {
		frame[w] = firmwareWordAt(core.Cfg.Firmware[fw], i)
	}
}

func TestTombstonePropagationAndUnlink(t *testing.T) {
	be := NewBackend()
	var a, b [128]uint64

	installFirmware(&a, core.FirmwareTypeTombstone)
	a[core.Cfg.R6] = 1234
	a[core.Cfg.PreviousID] = 1234
	a[core.Cfg.NextID] = 9999
	b[core.Cfg.PreviousID] = 7
	b[core.Cfg.NextID] = 1234

	if err := be.UniversalBitwise(unsafe.Pointer(&a), unsafe.Pointer(&b)); err != nil {
		t.Fatal(err)
	}

	if got := a[core.Cfg.PreviousID]; got != 0 {
		t.Fatalf("prev was not cleared: got %d want 0", got)
	}
	if got := a[core.Cfg.NextID]; got != 9999 {
		t.Fatalf("next changed unexpectedly: got %d want 9999", got)
	}
	if got := b[core.Cfg.R6]; got != 1234 {
		t.Fatalf("target id not propagated: got %d want 1234", got)
	}
	if got, want := b[core.Cfg.ProgramIndex:], a[core.Cfg.ProgramIndex:]; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("tombstone program not propagated")
	}

	installFirmware(&b, core.FirmwareTypeTombstone)
	if err := be.UniversalBitwise(unsafe.Pointer(&b), unsafe.Pointer(&a)); err != nil {
		t.Fatal(err)
	}
	if got := b[core.Cfg.NextID]; got != 0 {
		t.Fatalf("infected next was not cleared: got %d want 0", got)
	}
}

func BenchmarkUniversalBitwise(b *testing.B) {
	be := NewBackend()
	var a, c [128]uint64

	aWord := uint64(core.Cfg.ProgramIndex)
	for i, w := 0, aWord+4; i < len(core.Cfg.Firmware[core.FirmwareTypeViral]) && int(w) < len(a); i, w = i+2, w+1 {
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
		a[core.Cfg.RegPC] = 4
		if err := be.UniversalBitwise(unsafe.Pointer(&a), unsafe.Pointer(&c)); err != nil {
			b.Fatal(err)
		}
	}
}
