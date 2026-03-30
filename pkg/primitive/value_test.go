package primitive

import (
	"bytes"
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
		return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "cmd", "cfg", "config.yml"))
	}
	return filepath.Clean(filepath.Join("..", "..", "cmd", "cfg", "config.yml"))
}

func TestMain(m *testing.M) {
	viper.SetConfigFile(resolveValueTestConfigPath())

	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "primitive/value_test: viper.ReadInConfig: %v\n", err)
		os.Exit(1)
	}

	if err := core.LoadValueConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "primitive/value_test: core.LoadValueConfig: %v\n", err)
		os.Exit(1)
	}
	viper.Set("loglevel", "error")
	viper.Set("logging.trace.path", os.DevNull)
	loggingCfg, err := core.LoadLoggingConfig()

	if err != nil {
		fmt.Fprintf(os.Stderr, "primitive/value_test: core.LoadLoggingConfig: %v\n", err)
		os.Exit(1)
	}

	errnie.InitLogger(loggingCfg)
	code := m.Run()
	_ = errnie.Shutdown(context.Background())
	os.Exit(code)
}

func TestFold(t *testing.T) {
	Convey("Given two Values", t, func() {
		valueA, err := NewValue(nil)
		So(err, ShouldBeNil)
		defer valueA.Close()

		valueB, err := NewValue(nil)
		So(err, ShouldBeNil)
		defer valueB.Close()

		Convey("And Value A has the Viral Firmware", func() {
			valueA[core.Cfg.FW] = core.FirmwareRegisterViral
			valueA[core.Cfg.RegPC] = 0

			Convey("When Fold is used directly", func() {
				So(valueA.Fold(valueB), ShouldBeNil)

				Convey("Then the partner program space should be rewritten in place", func() {
					So(valueB.HasProgram(), ShouldBeTrue)
				})
			})
		})
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

		Convey("And the Values are Folded", func() {
			n, err := io.Copy(valueA, valueB)
			So(err, ShouldBeNil)
			So(n, ShouldEqual, 1024)

			Convey("Then a new Value is emitted", func() {
				buf := bytes.NewBuffer(make([]byte, 0, 1024))
				n, err := io.Copy(buf, valueA)
				So(err, ShouldBeNil)
				So(n, ShouldEqual, 1024)

				valueC, err := NewValue(nil)
				So(err, ShouldBeNil)
				defer valueC.Close()

				valueFrom(buf.Bytes(), valueC)
			})
		})

		Convey("And Value A has the Viral Firmware", func() {
			valueA[core.Cfg.FW] = core.FirmwareRegisterViral
			valueA[core.Cfg.RegPC] = 0

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
			valueA[core.Cfg.FW] = core.FirmwareRegisterViral
			valueA[core.Cfg.RegPC] = 0

			Convey("And ValueB is Folded into ValueA", func() {
				wire := make([]byte, ByteSize)
				So(ValueToBytes(valueB, wire), ShouldBeNil)
				n, err := valueA.Write(wire)
				So(err, ShouldBeNil)
				So(n, ShouldEqual, 1024)

				observed := BytesToValue(wire)
				defer observed.Close()

				Convey("ValueB should have the Viral Firmware", func() {
					assertViralPartnerState(observed)
				})
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

		So(valueA.Fold(valueB), ShouldBeNil)
		affinity := valueA[core.Cfg.AffinityIndex] & 0x0000FFFFFFFFFFFF
		So(affinity, ShouldNotEqual, uint64(0))
		So(byte(affinity>>40), ShouldEqual, text[0])
		So(valueA[core.Cfg.StateSequence], ShouldNotEqual, uint64(0))
		So(valueA[core.Cfg.StateAccumulator], ShouldNotEqual, uint64(0))
	})
}

func assertViralPartnerState(partner *Value) {
	So(partner[core.Cfg.FW], ShouldEqual, core.FirmwareRegisterLearn)
	So(partner[core.Cfg.RegPC], ShouldEqual, uint64(0))
}

func BenchmarkValue_Read(b *testing.B) {
	v, err := NewValue(nil)
	if err != nil {
		b.Fatal(err)
	}
	defer v.Close()

	buf := make([]byte, ByteSize)
	b.SetBytes(int64(ByteSize))
	b.ResetTimer()
	for b.Loop() {
		n, err := v.Read(buf)
		if n != ByteSize || err != io.EOF {
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

	payload := make([]byte, ByteSize)

	for i := range payload {
		payload[i] = byte(i)
	}

	if _, err := dst.Write(payload); err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(ByteSize))
	b.ResetTimer()

	for b.Loop() {
		if _, err := dst.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
}
