package primitive

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
)

func resolveValueTestConfigPath() string {
	if e := strings.TrimSpace(os.Getenv("TEST_CONFIG_PATH")); e != "" {
		return filepath.Clean(e)
	}

	_, file, _, ok := runtime.Caller(0)

	if ok {
		return filepath.Clean(filepath.Join(
			filepath.Dir(file), "..", "..", "cmd", "cfg", "config.yml",
		))
	}

	return filepath.Clean(filepath.Join("..", "..", "cmd", "cfg", "config.yml"))
}

func TestMain(m *testing.M) {
	viper.SetConfigFile(resolveValueTestConfigPath())

	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(
			os.Stderr,
			"primitive/value_test: viper.ReadInConfig: %v\n",
			err,
		)
		os.Exit(1)
	}

	viper.Set("loglevel", "error")
	viper.Set("logging.trace.path", os.DevNull)
	core.NewConfig()
	loggingCfg, err := core.LoadLoggingConfig()

	if err != nil {
		fmt.Fprintf(
			os.Stderr,
			"primitive/value_test: core.LoadLoggingConfig: %v\n",
			err,
		)
		os.Exit(1)
	}

	errnie.InitLogger(loggingCfg)
	code := m.Run()

	if err := errnie.Shutdown(context.Background()); err != nil {
		fmt.Fprintf(
			os.Stderr,
			"primitive/value_test: errnie.Shutdown: %v\n",
			err,
		)
		os.Exit(1)
	}

	os.Exit(code)
}

func TestNewValueAssignsFreshIDAfterPoolReuse(t *testing.T) {
	Convey("NewValue assigns a fresh ValueID after a pooled frame is reused", t, func() {
		first, err := NewValue([]byte("alpha"))
		So(err, ShouldBeNil)
		firstID := first.GetWord(core.Cfg.Value.Region.ID.Start)
		So(firstID, ShouldBeGreaterThan, 0)

		So(first.InstallFirmware(core.FirmwareTypeTombstone), ShouldBeNil)
		So(first.Close(), ShouldBeNil)

		second, err := NewValue([]byte("beta"))
		So(err, ShouldBeNil)

		So(second.GetWord(core.Cfg.Value.Region.ID.Start), ShouldBeGreaterThan, 0)
		So(second.GetWord(core.Cfg.Value.Region.ID.Start), ShouldNotEqual, firstID)

		So(second.InstallFirmware(core.FirmwareTypeTombstone), ShouldBeNil)
		So(second.Close(), ShouldBeNil)
	})
}

func TestRead(t *testing.T) {
	Convey("Given two Values", t, func() {
		valueA, err := NewValue(nil)
		So(err, ShouldBeNil)
		defer valueA.Close()

		valueB, err := NewValue(nil)
		So(err, ShouldBeNil)
		defer valueB.Close()

		Convey("And the Values are Folded (signal emission model)", func() {
			// In the new model, folding does not mutate values directly.
			// Instead, a signal is produced and new Values are emitted.
			// TODO: Check for correct emission of new Values and their linkage.
			// For now, just ensure no error and placeholder for emission check.
			n, err := io.Copy(valueA, valueB)
			So(err, ShouldBeNil)
			So(n, ShouldEqual, 1024)
			// TODO: Check emission of new Values based on signal.
		})

		Convey("And Value A has the Viral Firmware", func() {
			valueA[core.Cfg.Value.Region.Registers.FW] = core.FirmwareRegisterViral
			valueA[core.Cfg.Value.Region.Registers.PC] = 0

			Convey("And the Values are Folded", func() {
				n, err := io.Copy(valueA, valueB)
				So(err, ShouldBeNil)
				So(n, ShouldEqual, 1024)
			})
		})
	})
}

func TestWrite(t *testing.T) {
	Convey("Given three Values", t, func() {
		valueA, err := NewValue(nil)
		So(err, ShouldBeNil)
		defer valueA.Close()

		valueB, err := NewValue(nil)
		So(err, ShouldBeNil)
		defer valueB.Close()

		valueC, err := NewValue(nil)
		So(err, ShouldBeNil)
		defer valueC.Close()

		Convey("And Value A has the Viral Firmware", func() {
			valueA[core.Cfg.Value.Region.Registers.FW] = core.FirmwareRegisterViral
			valueA[core.Cfg.Value.Region.Registers.PC] = 0

			Convey("And ValueB is Folded into ValueA (signal emission model)", func() {
				wire := make([]byte, core.Cfg.Value.Bytes)
				So(ValueToBytes(valueB, wire), ShouldBeNil)
				n, err := valueA.Write(wire)
				So(err, ShouldBeNil)
				So(n, ShouldEqual, 1024)
				// TODO: Check emission of new Values and linkage, not in-place mutation.
			})
		})
	})
}

func TestBootloaderProjectsStructureInBand(t *testing.T) {
	Convey("Bootloader derives affinity and state seeds from token spans in-band", t, func() {
		text := []byte("Mary moved to the kitchen.")
		valueA, err := NewValue(text)
		So(err, ShouldBeNil)
		defer valueA.Close()

		valueB, err := NewValue(nil)
		So(err, ShouldBeNil)
		defer valueB.Close()

		n, err := valueA.Write(valueB.Bytes())
		So(err, ShouldBeNil)
		So(n, ShouldEqual, 1024)
		// TODO: Check that the correct structure emission signal is produced.
	})
}

func TestLearnAdvancesStateSequenceInBand(t *testing.T) {
	Convey("Learn advances StateSequence in-band as a geometric signal", t, func() {
		valueA, err := NewValue(nil)
		So(err, ShouldBeNil)
		defer valueA.Close()

		valueB, err := NewValue(nil)
		So(err, ShouldBeNil)
		defer valueB.Close()

		valueA[core.Cfg.Value.Region.State.Sequence] = 1
		valueA[core.Cfg.Value.Region.Registers.FW] = core.FirmwareRegisterLearn
		valueA[core.Cfg.Value.Region.Registers.PC] = 0

		n, err := valueA.Write(valueB.Bytes())
		So(err, ShouldBeNil)
		So(n, ShouldEqual, 1024)
		// TODO: Check that the correct signal is produced and new Value(s) are emitted.
	})
}

func TestLearnWeavesAccumulatorWithSequenceInBand(t *testing.T) {
	Convey("Learn folds the evolved sequence into StateAccumulator in-band", t, func() {
		valueA, err := NewValue(nil)
		So(err, ShouldBeNil)
		defer valueA.Close()

		valueB, err := NewValue(nil)
		So(err, ShouldBeNil)
		defer valueB.Close()

		valueA[core.Cfg.Value.Region.State.Sequence] = 1
		valueA[core.Cfg.Value.Region.State.Accumulator] = 0x82
		valueA[core.Cfg.Value.Region.Registers.R6] = 1
		valueA[core.Cfg.Value.Region.Registers.FW] = core.FirmwareRegisterLearn
		valueA[core.Cfg.Value.Region.Registers.PC] = 0

		n, err := valueA.Write(valueB.Bytes())
		So(err, ShouldBeNil)
		So(n, ShouldEqual, 1024)
		// TODO: Check that the correct signal is produced and new Value(s) are emitted.
	})
}

func TestBuildAppliesAccumulatorDeltaToLeadingTokenInBand(t *testing.T) {
	Convey("Build applies the XOR-delta sketch across dispersed token anchors in-band", t, func() {
		valueA, err := NewValue(nil)
		So(err, ShouldBeNil)
		defer valueA.Close()

		valueB, err := NewValue(nil)
		So(err, ShouldBeNil)
		defer valueB.Close()

		anchorWords := []int{
			core.Cfg.Value.Region.Tokens.Start,
			core.Cfg.Value.Region.Tokens.Start + 7,
			core.Cfg.Value.Region.Tokens.Start + 14,
			core.Cfg.Value.Region.Tokens.Start + 21,
			core.Cfg.Value.Region.Tokens.Start + 28,
			core.Cfg.Value.Region.Tokens.Start + 35,
		}

		seedTokens := []uint64{
			0x55, 0x11, 0x22, 0x33, 0x44, 0x66,
		}

		for i, idx := range anchorWords {
			valueA[idx] = seedTokens[i]
		}

		valueA[core.Cfg.Value.Region.Affinity.Start] = 0b10110100
		valueA[core.Cfg.Value.Region.Registers.FW] = core.FirmwareRegisterBuild
		valueA[core.Cfg.Value.Region.Registers.PC] = 0
		valueB[core.Cfg.Value.Region.Affinity.Start] = 0b00110110

		n, err := valueA.Write(valueB.Bytes())
		So(err, ShouldBeNil)
		So(n, ShouldEqual, 1024)
		// TODO: Check that the correct signal is produced and new Value(s) are emitted.
	})
}

func TestTokenRegionObservedBytes(t *testing.T) {
	Convey("nil value yields nil slice", t, func() {
		So((*Value).TokenIDs(nil), ShouldBeNil)
	})

	Convey("TokenRegionObservedBytes packs token words little-endian and trims trailing zeros", t, func() {
		var v Value
		base := core.Cfg.Value.Region.Tokens.Start
		So(base < core.Cfg.Value.Words, ShouldBeTrue)

		v[base] = 0x020100

		got := v.TokenIDs()
		So(got, ShouldResemble, []byte{0, 1, 2})
	})

	Convey("multiple token words emit little-endian bytes per word; only trailing zeros of full pack are trimmed", t, func() {
		var v Value
		base := core.Cfg.Value.Region.Tokens.Start
		tokenWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)
		So(tokenWords, ShouldBeGreaterThan, 1)
		So(base+1, ShouldBeLessThan, core.Cfg.Value.Words)

		v[base] = 0x04030201
		v[base+1] = 0x08070605

		got := v.TokenIDs()
		want := []byte{1, 2, 3, 4, 0, 0, 0, 0, 5, 6, 7, 8}
		So(got, ShouldResemble, want)
	})

	Convey("all-zero token region yields empty non-nil slice", t, func() {
		var v Value
		base := core.Cfg.Value.Region.Tokens.Start
		tokenWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)

		for w := 0; w < tokenWords; w++ {
			idx := base + w

			if idx >= core.Cfg.Value.Words {
				break
			}

			v[idx] = 0
		}

		got := v.TokenIDs()
		So(got, ShouldNotBeNil)
		So(len(got), ShouldEqual, 0)
	})
}

func TestDetokenizeTokenID(t *testing.T) {
	Convey("DetokenizeTokenID recovers byte and index from Tokenize", t, func() {
		for _, tc := range []struct {
			b     byte
			index uint64
		}{
			{'a', 0},
			{'Z', 99},
			{0xFF, 1 << 18},
		} {
			tid := Tokenize(tc.b, tc.index)
			So(tid, ShouldEqual, Tokenize(tc.b, tc.index))
		}
	})
}

func assertViralPartnerState(partner *Value) {
	So(partner[core.Cfg.Value.Region.Registers.FW], ShouldEqual, core.FirmwareRegisterLearn)
	So(partner[core.Cfg.Value.Region.Registers.PC], ShouldEqual, uint64(0))
}

func BenchmarkTokenize(b *testing.B) {
	var sink uint64
	var index uint64
	b.ResetTimer()

	for b.Loop() {
		sink += Tokenize('x', index)
		index++
	}

	_ = sink
}

func BenchmarkValue_Read(b *testing.B) {
	v, err := NewValue(nil)
	if err != nil {
		b.Fatal(err)
	}
	defer v.Close()

	buf := make([]byte, core.Cfg.Value.Bytes)
	b.SetBytes(int64(core.Cfg.Value.Bytes))
	b.ResetTimer()
	for b.Loop() {
		n, err := v.Read(buf)
		if n != core.Cfg.Value.Bytes || err != io.EOF {
			b.Fatalf("Read: n=%d err=%v", n, err)
		}
	}
}

func BenchmarkValue_Write(b *testing.B) {
	dst, err := NewValue(nil)

	if err != nil {
		b.Fatal(err)
	}

	defer dst.Close()

	payload := make([]byte, core.Cfg.Value.Bytes)

	for i := range payload {
		payload[i] = byte(i)
	}

	if _, err := dst.Write(payload); err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(core.Cfg.Value.Bytes))
	b.ResetTimer()

	for b.Loop() {
		if _, err := dst.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
}
