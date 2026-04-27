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
		Convey("When NewConfig merges viper after TestMain loaded cmd/cfg/config.yml", func() {
			cfg := NewConfig()

			Convey("It should lower the link DSL into 64-bit instruction words", func() {
				src := cfg.Programs[LINK].Source
				rules := cfg.Programs[LINK].Compiled()

				So(len(src), ShouldBeGreaterThan, 0)
				So(len(rules), ShouldBeGreaterThan, 0)
				So(rules[0], ShouldNotEqual, uint64(0))
			})

			Convey("It should include the LINK program with Name \"link\" and non-empty Compiled", func() {
				So(len(cfg.Programs), ShouldBeGreaterThan, 0)
				So(cfg.Programs[LINK].Name, ShouldEqual, "link")
				So(len(cfg.Programs[LINK].Compiled()), ShouldBeGreaterThan, 0)
			})
		})

		Convey("loads telemetry settings from viper", func() {
			viper.Set("telemetry.enabled", true)
			viper.Set("telemetry.ws_url", "ws://127.0.0.1:9191/ws")

			cfg := NewConfig()

			So(cfg.TelemetryEnabled, ShouldBeTrue)
			So(cfg.TelemetryWebSocketURL, ShouldEqual, "ws://127.0.0.1:9191/ws")
		})

		Convey("It should preserve explicit zero values", func() {
			Reset(func() {
				viper.Set("system.maxMembersPerField", 256)
				NewConfig()
			})

			viper.Set("system.maxMembersPerField", 0)

			cfg := NewConfig()

			So(cfg.System.MaxMembersPerField, ShouldEqual, 0)
		})
	})
}

func TestValueRegionConfigMaxTokenIngestBytes(t *testing.T) {
	Convey("Given a ValueRegionConfig", t, func() {
		Convey("It should compute MaxTokenIngestBytes as four Morton slots per token word", func() {
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

func TestValueOffsetConfig_WordExtent(t *testing.T) {
	Convey("Given a ValueOffsetConfig", t, func() {
		Convey("It should return start and ceil(Bits/64) words", func() {
			cfg := ValueOffsetConfig{Start: 16, Bits: 512}

			start, words := cfg.WordExtent()

			So(start, ShouldEqual, 16)
			So(words, ShouldEqual, 8)
		})

		Convey("It should round partial words up", func() {
			cfg := ValueOffsetConfig{Start: 0, Bits: 257}

			_, words := cfg.WordExtent()

			So(words, ShouldEqual, 5)
		})
	})
}

func BenchmarkValueOffsetConfig_WordExtent(b *testing.B) {
	cfg := ValueOffsetConfig{Start: 16, Bits: 512}

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_, _ = cfg.WordExtent()
	}
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

/*
TestRecruitCommunityGuardPredicate verifies that the canonical
recruit_community block lowers its Shannon-limit guard to a real inline
predicate instruction. The guard reads `(A.affinity >= 121)` (the 47%
ceiling for a 257-bit affinity window), so the lowered instruction must
carry the GE condition code with 121 staged into the constants table.
*/
func TestRecruitCommunityGuardPredicate(t *testing.T) {
	Convey("Given the compiled recruit_community firmware", t, func() {
		entry := Cfg.Programs[RECRUIT_COMMUNITY]
		So(len(entry.Words), ShouldBeGreaterThan, 0)

		Convey("It encodes at least one GE-predicate instruction", func() {
			foundGE := false
			for _, word := range entry.Words {
				if word == 0 {
					continue
				}

				if (word>>57)&1 != 1 {
					continue
				}

				if (word>>58)&7 == 3 { // PredGE
					foundGE = true
					break
				}
			}

			So(foundGE, ShouldBeTrue)
		})

		Convey("And the constants table stages 121 for the Shannon threshold", func() {
			has121 := false
			for _, init := range entry.Constants {
				if init.Value == 121 {
					has121 = true
					break
				}
			}

			So(has121, ShouldBeTrue)
		})
	})
}

func BenchmarkNewConfig(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		NewConfig()
	}
}
