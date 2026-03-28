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
