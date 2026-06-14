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
TestRecruitCommunityPredicate verifies that recruitment remains an in-band
gossip sweep: linked peers are stamped by ALU writes, while the sample head
self-stamps the header key used by prompt readout.
*/
func TestRecruitCommunityPredicate(t *testing.T) {
	Convey("Given the compiled recruit_community firmware", t, func() {
		entry := Cfg.Programs[RECRUIT_COMMUNITY]
		So(len(entry.Words), ShouldBeGreaterThan, 0)

		const (
			wordOpcodeWrite     = 0x3 // copies register A into dest (COPYA / write paths)
			wordOpcodeOR        = 0x7
			affinityLaneStart   = 123
			affinityLaneSpan    = 5
			headerSourceStart   = 122       // header scratch uses id word lane
			chainReferenceSlot  = 67        // properties.reference points at the sample head
			headerCommunitySlot = 64        // route header community field
			headerTargetSlot    = 65        // route header target field
			headerRoleSlot      = 66        // route header role field
			predicateBit        = uint64(1) // InstrPredBitShift
			peerTarget          = uint64(1)
		)

		Convey("It encodes the peer stamp and community header key", func() {
			foundPredicate := false
			foundAffinityWrite := false
			foundHeaderCommunityWrite := false
			foundHeaderTargetWrite := false
			foundHeaderRoleWrite := false
			foundPeerCommunityWrite := false

			for _, word := range entry.Words {
				if word == 0 {
					continue
				}

				opcode := word & 0xF
				predicate := (word >> 57) & 1
				aStart := (word >> 4) & 0x7F
				aSpan := ((word >> 11) & 0x7F) + 1
				bStart := (word >> 18) & 0x7F
				dstStart := (word >> 32) & 0x7F
				dstSpan := ((word >> 39) & 0x7F) + 1
				target := (word >> 53) & 0x3
				srcAFromB := (word >> 61) & 1

				if predicate == predicateBit && srcAFromB == 1 && aStart == chainReferenceSlot && bStart == headerSourceStart && aSpan == 1 {
					foundPredicate = true
				}

				if opcode == wordOpcodeOR && aStart == affinityLaneStart && aSpan == affinityLaneSpan && dstStart == affinityLaneStart && dstSpan == affinityLaneSpan {
					foundAffinityWrite = true
				}

				if opcode == wordOpcodeWrite && target == peerTarget && aStart == headerSourceStart && dstStart == headerCommunitySlot && dstSpan == 1 {
					foundPeerCommunityWrite = true
				}

				if opcode == wordOpcodeWrite && target == 0 && aStart == headerSourceStart && aSpan == 1 && dstStart == headerCommunitySlot && dstSpan == 1 {
					foundHeaderCommunityWrite = true
				}

				if opcode == wordOpcodeWrite && target == 0 && aStart == headerSourceStart && aSpan == 1 && dstStart == headerTargetSlot && dstSpan == 1 {
					foundHeaderTargetWrite = true
				}

				if opcode == wordOpcodeWrite && target == 0 && dstStart == headerRoleSlot && dstSpan == 1 {
					foundHeaderRoleWrite = true
				}
			}

			So(foundPredicate, ShouldBeTrue)
			So(foundAffinityWrite, ShouldBeTrue)
			So(foundPeerCommunityWrite, ShouldBeTrue)
			So(foundHeaderCommunityWrite, ShouldBeTrue)
			So(foundHeaderTargetWrite, ShouldBeTrue)
			So(foundHeaderRoleWrite, ShouldBeTrue)
		})
	})
}

func TestClassifyReadoutReducers(t *testing.T) {
	Convey("Given the compiled classify_readout firmware", t, func() {
		entry := Cfg.Programs[CLASSIFY_READOUT]
		So(len(entry.Words), ShouldBeGreaterThan, 0)

		Convey("It selects labels through prompt affinity and community readout state", func() {
			foundPromptAffinityFold := false
			foundRootPredicate := false
			foundAffinityXor := false
			foundDeltaPopcnt := false
			foundNearestCompare := false
			foundSurprisalCopy := false
			foundCommunityCopy := false
			foundCommunityPredicate := false
			foundLabelCopy := false
			foundResolved := false
			foundReducerOpcode := false
			constants := make(map[uint64]uint64)

			for _, init := range entry.Constants {
				constants[init.Offset] = init.Value
			}

			for _, word := range entry.Words {
				if word == 0 {
					continue
				}

				opcode := word & 0xF
				predicate := (word >> 57) & 1
				aStart := (word >> 4) & 0x7F
				aSpan := ((word >> 11) & 0x7F) + 1
				bStart := (word >> 18) & 0x7F
				bSpan := ((word >> 25) & 0x7F) + 1
				dstStart := (word >> 32) & 0x7F
				dstSpan := ((word >> 39) & 0x7F) + 1
				predCond := (word >> 58) & 0x7
				target := (word >> 53) & 0x3
				srcAFromB := (word >> 61) & 1

				if opcode == 0x7 && target == 0 &&
					aStart == 123 && aSpan == 5 &&
					bStart == 123 && bSpan == 5 &&
					dstStart == 123 && dstSpan == 5 {
					foundPromptAffinityFold = true
				}

				if predicate == 1 && predCond == 4 && srcAFromB == 1 && aStart == 66 && constants[bStart] == 3 {
					foundRootPredicate = true
				}

				if opcode == 0x6 && target == 1 &&
					aStart == 123 && aSpan == 5 &&
					bStart == 123 && bSpan == 5 &&
					dstStart == 48 && dstSpan == 5 {
					foundAffinityXor = true
				}

				if predicate == 1 && predCond == 6 && target == 1 && srcAFromB == 1 && aStart == 48 && aSpan == 5 && dstStart == 70 {
					foundDeltaPopcnt = true
				}

				if predicate == 1 && predCond == 0 && srcAFromB == 1 && aStart == 70 && bStart == 68 {
					foundNearestCompare = true
				}

				if opcode == 0x5 && target == 0 && bStart == 70 && dstStart == 68 {
					foundSurprisalCopy = true
				}

				if opcode == 0x5 && target == 0 && bStart == 64 && dstStart == 64 {
					foundCommunityCopy = true
				}

				if predicate == 1 && predCond == 4 && srcAFromB == 1 && aStart == 64 && bStart == 64 {
					foundCommunityPredicate = true
				}

				if opcode == 0x5 && target == 0 && bStart == 56 && dstStart == 56 {
					foundLabelCopy = true
				}

				if opcode == 0x3 && predicate == 0 && dstStart == 61 && constants[aStart] == 6 {
					foundResolved = true
				}

				if predicate == 1 && (opcode == 0x1 || opcode == 0x2 || opcode == 0x4 || opcode == 0x5 || opcode == 0x8) {
					foundReducerOpcode = true
				}
			}

			So(foundPromptAffinityFold, ShouldBeTrue)
			So(foundRootPredicate, ShouldBeTrue)
			So(foundAffinityXor, ShouldBeTrue)
			So(foundDeltaPopcnt, ShouldBeTrue)
			So(foundNearestCompare, ShouldBeTrue)
			So(foundSurprisalCopy, ShouldBeTrue)
			So(foundCommunityCopy, ShouldBeTrue)
			So(foundCommunityPredicate, ShouldBeTrue)
			So(foundLabelCopy, ShouldBeTrue)
			So(foundResolved, ShouldBeTrue)
			So(foundReducerOpcode, ShouldBeFalse)
		})
	})
}

func TestClassPrototypeFirmware(t *testing.T) {
	Convey("Given the compiled class_prototype firmware", t, func() {
		entry := Cfg.Programs[CLASS_PROTOTYPE]
		So(len(entry.Words), ShouldBeGreaterThan, 0)

		Convey("It settles without constructing class-prototype helpers", func() {
			foundDone := false
			foundContinuationClear := false
			foundContextOr := false
			foundReadoutRole := false
			constants := make(map[uint64]uint64)

			for _, init := range entry.Constants {
				constants[init.Offset] = init.Value
			}

			for _, word := range entry.Words {
				if word == 0 {
					continue
				}

				opcode := word & 0xF
				predicate := (word >> 57) & 1
				aStart := (word >> 4) & 0x7F
				aSpan := ((word >> 11) & 0x7F) + 1
				bStart := (word >> 18) & 0x7F
				bSpan := ((word >> 25) & 0x7F) + 1
				dstStart := (word >> 32) & 0x7F
				dstSpan := ((word >> 39) & 0x7F) + 1
				target := (word >> 53) & 0x3

				if opcode == 0x3 && predicate == 0 && target == 0 && dstStart == 61 && constants[aStart] == 5 {
					foundDone = true
				}

				if opcode == 0x7 && target == 0 &&
					aStart == 40 && aSpan == 8 &&
					bStart == 40 && bSpan == 8 &&
					dstStart == 40 && dstSpan == 8 {
					foundContextOr = true
				}

				if opcode == 0x0 && target == 0 && dstStart == 71 && dstSpan == 1 {
					foundContinuationClear = true
				}

				if opcode == 0x3 && target == 0 && dstStart == 66 {
					foundReadoutRole = true
				}
			}

			So(foundDone, ShouldBeTrue)
			So(foundContinuationClear, ShouldBeTrue)
			So(foundContextOr, ShouldBeFalse)
			So(foundReadoutRole, ShouldBeFalse)
		})
	})
}

func TestStructuralSignalFirmware(t *testing.T) {
	Convey("Given the compiled structural_associate firmware", t, func() {
		entry := Cfg.Programs[STRUCTURAL_ASSOCIATE]
		So(len(entry.Words), ShouldBeGreaterThan, 0)

		Convey("It produces explicit signal witnesses and linked peer state", func() {
			foundTokenXor := false
			foundConfidencePopcnt := false
			foundOwnerNext := false
			foundPeerPrev := false
			foundPeerNext := false

			for _, word := range entry.Words {
				if word == 0 {
					continue
				}

				opcode := word & 0xF
				predicate := (word >> 57) & 1
				aStart := (word >> 4) & 0x7F
				aSpan := ((word >> 11) & 0x7F) + 1
				bStart := (word >> 18) & 0x7F
				bSpan := ((word >> 25) & 0x7F) + 1
				dstStart := (word >> 32) & 0x7F
				dstSpan := ((word >> 39) & 0x7F) + 1
				target := (word >> 53) & 0x3
				predCond := (word >> 58) & 0x7

				if opcode == 0x6 && predicate == 0 &&
					aStart == 0 && aSpan == 8 &&
					bStart == 0 && bSpan == 8 &&
					dstStart == 32 && dstSpan == 8 {
					foundTokenXor = true
				}

				if predicate == 1 && predCond == 6 && aStart == 32 && aSpan == 8 && dstStart == 57 {
					foundConfidencePopcnt = true
				}

				if opcode == 0x5 && target == 0 && bStart == 122 && dstStart == 121 {
					foundOwnerNext = true
				}

				if opcode == 0x3 && target == 1 && aStart == 122 && dstStart == 120 {
					foundPeerPrev = true
				}

				if opcode == 0x3 && target == 1 && aStart == 122 && dstStart == 121 {
					foundPeerNext = true
				}
			}

			So(foundTokenXor, ShouldBeTrue)
			So(foundConfidencePopcnt, ShouldBeTrue)
			So(foundOwnerNext, ShouldBeTrue)
			So(foundPeerPrev, ShouldBeTrue)
			So(foundPeerNext, ShouldBeTrue)
		})
	})

	Convey("Given the compiled structural_readout firmware", t, func() {
		entry := Cfg.Programs[STRUCTURAL_READOUT]
		So(len(entry.Words), ShouldBeGreaterThan, 0)

		Convey("It folds association tokens and resolves the prompt head", func() {
			foundTokenOr := false
			foundSignalXor := false
			foundConfidencePopcnt := false
			foundResolved := false
			foundDone := false
			constants := make(map[uint64]uint64)

			for _, init := range entry.Constants {
				constants[init.Offset] = init.Value
			}

			for _, word := range entry.Words {
				if word == 0 {
					continue
				}

				opcode := word & 0xF
				predicate := (word >> 57) & 1
				aStart := (word >> 4) & 0x7F
				aSpan := ((word >> 11) & 0x7F) + 1
				bStart := (word >> 18) & 0x7F
				bSpan := ((word >> 25) & 0x7F) + 1
				dstStart := (word >> 32) & 0x7F
				predCond := (word >> 58) & 0x7

				if opcode == 0x7 && predicate == 0 &&
					aStart == 0 && aSpan == 16 &&
					bStart == 0 && bSpan == 16 &&
					dstStart == 0 {
					foundTokenOr = true
				}

				if opcode == 0x6 && predicate == 0 && aStart == 0 && aSpan == 8 && bStart == 0 && bSpan == 8 && dstStart == 32 {
					foundSignalXor = true
				}

				if predicate == 1 && predCond == 6 && aStart == 32 && aSpan == 8 && dstStart == 57 {
					foundConfidencePopcnt = true
				}

				if opcode == 0x3 && predicate == 0 && dstStart == 61 && constants[aStart] == 6 {
					foundResolved = true
				}

				if opcode == 0x3 && predicate == 0 && dstStart == 61 && constants[aStart] == 5 {
					foundDone = true
				}
			}

			So(foundTokenOr, ShouldBeTrue)
			So(foundSignalXor, ShouldBeTrue)
			So(foundConfidencePopcnt, ShouldBeTrue)
			So(foundResolved, ShouldBeTrue)
			So(foundDone, ShouldBeTrue)
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
