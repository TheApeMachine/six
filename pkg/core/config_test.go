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

			Convey("It should lower the query DSL into 64-bit instruction words", func() {
				src := cfg.Programs[QUERY].Source
				rules := cfg.Programs[QUERY].Compiled()

				So(len(src), ShouldBeGreaterThan, 0)
				So(len(rules), ShouldBeGreaterThan, 0)
				So(rules[0], ShouldNotEqual, uint64(0))
			})

			Convey("It should include the query program with Name \"query\" and non-empty Compiled", func() {
				So(len(cfg.Programs), ShouldBeGreaterThan, 0)
				So(cfg.Programs[QUERY].Name, ShouldEqual, "query")
				So(len(cfg.Programs[QUERY].Compiled()), ShouldBeGreaterThan, 0)
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

func TestPrecompile(t *testing.T) {
	Convey("Given a malformed firmware block", t, func() {
		originalPrograms := viper.Get("programs")

		Reset(func() {
			viper.Set("programs", originalPrograms)
			NewConfig()
		})

		viper.Set("programs", map[string]any{
			"broken": "not valid firmware",
		})

		Convey("It should fail at config construction instead of creating a missing program", func() {
			So(func() {
				NewConfig()
			}, ShouldPanic)
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
TestRecruitCommunityPredicate verifies that recruitment remains guarded in
firmware: the Hamming-distance budget must compile to a scratch xor followed by
an in-band popcount predicate.
*/
func TestRecruitCommunityPredicate(t *testing.T) {
	Convey("Given the compiled recruit_community firmware", t, func() {
		entry := Cfg.Programs[RECRUIT_COMMUNITY]
		So(len(entry.Words), ShouldBeGreaterThan, 0)

		Convey("It encodes the Hamming-distance scratch xor and LT predicate", func() {
			foundXor := false
			foundPredicate := false
			foundUnionOr := false
			foundUnionPredicate := false

			for _, word := range entry.Words {
				if word == 0 {
					continue
				}

				opcode := word & 0xF
				predicate := (word >> 57) & 1
				cond := (word >> 58) & 7
				aStart := (word >> 4) & 0x7F
				aSpan := ((word >> 11) & 0x7F) + 1
				dstStart := (word >> 32) & 0x7F
				dstSpan := ((word >> 39) & 0x7F) + 1

				if opcode == 0x6 && aStart == 40 && aSpan == 5 && dstStart >= 73 && dstSpan == 5 {
					foundXor = true
				}

				if predicate == 1 && cond == 0 && aStart >= 73 && aSpan == 1 {
					foundPredicate = true
				}

				if opcode == 0x7 && aStart == 123 && aSpan == 5 && dstStart >= 73 && dstSpan == 5 {
					foundUnionOr = true
				}

				if predicate == 1 && cond == 1 && aStart >= 73 && aSpan == 1 {
					foundUnionPredicate = true
				}
			}

			So(foundXor, ShouldBeTrue)
			So(foundPredicate, ShouldBeTrue)
			So(foundUnionOr, ShouldBeTrue)
			So(foundUnionPredicate, ShouldBeTrue)
		})

		Convey("And the constants table stages the Hamming and Shannon budgets", func() {
			hasHammingBudget := false
			hasShannonBudget := false

			for _, init := range entry.Constants {
				if init.Value == 64 {
					hasHammingBudget = true
				}

				if init.Value == 121 {
					hasShannonBudget = true
				}
			}

			So(hasHammingBudget, ShouldBeTrue)
			So(hasShannonBudget, ShouldBeTrue)
		})
	})
}

func TestClassifyReadoutReducers(t *testing.T) {
	Convey("Given the compiled classify_readout firmware", t, func() {
		entry := Cfg.Programs[CLASSIFY_READOUT]
		So(len(entry.Words), ShouldBeGreaterThan, 0)

		Convey("It uses generic categorical lane reducers", func() {
			foundArgMin := false
			foundMode := false

			for _, word := range entry.Words {
				if word == 0 {
					continue
				}

				opcode := word & 0xF
				predicate := (word >> 57) & 1

				if predicate == 1 && opcode == 0x1 {
					foundArgMin = true
				}

				if predicate == 1 && opcode == 0x2 {
					foundMode = true
				}
			}

			So(foundArgMin, ShouldBeTrue)
			So(foundMode, ShouldBeTrue)
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
