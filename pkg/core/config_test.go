package core

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
)

func resolveCoreTestConfigPath() string {
	if envPath := strings.TrimSpace(os.Getenv("TEST_CONFIG_PATH")); envPath != "" {
		return filepath.Clean(envPath)
	}

	_, file, _, ok := runtime.Caller(0)

	if ok {
		return filepath.Clean(
			filepath.Join(filepath.Dir(file), "..", "..", "cmd", "cfg", "config.yml"),
		)
	}

	return filepath.Clean(filepath.Join("..", "..", "cmd", "cfg", "config.yml"))
}

func TestMain(m *testing.M) {
	viper.SetConfigFile(resolveCoreTestConfigPath())

	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "core: viper.ReadInConfig: %v\n", err)
		os.Exit(1)
	}

	viper.Set("loglevel", "error")
	NewConfig()

	code := m.Run()

	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "core: viper.ReadInConfig (restore): %v\n", err)
		os.Exit(1)
	}

	NewConfig()
	os.Exit(code)
}

func TestNewConfig(t *testing.T) {
	Convey("NewConfig", t, func() {
		Convey("applies control plane defaults when viper yields zero", func() {
			viper.Set("controlplane.k", 0)
			viper.Set("controlplane.alpha", 0)
			viper.Set("controlplane.affinity.bits", 0)

			cfg := NewConfig()

			So(cfg.Kadabra.BucketSize, ShouldEqual, 20)
			So(cfg.Kadabra.Alpha, ShouldEqual, 3)
			So(cfg.Kadabra.Bits, ShouldEqual, 64)
		})

		Convey("loads telemetry settings from viper", func() {
			viper.Set("telemetry.enabled", true)
			viper.Set("telemetry.udp_endpoint", "127.0.0.1:9191")
			viper.Set("telemetry.universal_bitwise_slots", true)

			cfg := NewConfig()

			So(cfg.TelemetryEnabled, ShouldBeTrue)
			So(cfg.TelemetryEndpoint, ShouldEqual, "127.0.0.1:9191")
			So(cfg.TelemetryUniversalBitwiseSlots, ShouldBeTrue)
		})
	})
}

func TestValueRegionConfigMaxTokenIngestBytes(t *testing.T) {
	Convey("Given a ValueRegionConfig", t, func() {
		Convey("It should compute MaxTokenIngestBytes as half the Morton slab in bytes (minimum 1)", func() {
			region := ValueRegionConfig{
				Tokens: ValueOffsetConfig{Bits: 1024},
			}

			So(region.MaxTokenIngestBytes(), ShouldEqual, 64)

			small := ValueRegionConfig{
				Tokens: ValueOffsetConfig{Bits: 8},
			}

			So(small.MaxTokenIngestBytes(), ShouldEqual, 1)
		})
	})
}

func BenchmarkValueRegionConfigMaxTokenIngestBytes(b *testing.B) {
	large := ValueRegionConfig{
		Tokens: ValueOffsetConfig{Bits: 1024},
	}

	small := ValueRegionConfig{
		Tokens: ValueOffsetConfig{Bits: 8},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_ = large.MaxTokenIngestBytes()
		_ = small.MaxTokenIngestBytes()
	}
}

func BenchmarkNewConfig(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		NewConfig()
	}
}
