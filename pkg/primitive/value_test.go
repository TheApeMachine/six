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
			valueA.installFirmware(core.FirmwareTypeViral)

			Convey("When Fold is used directly", func() {
				So(valueA.Fold(valueB), ShouldBeNil)

				Convey("Then the partner program space should be rewritten in place", func() {
					So(valueB[core.Cfg.ProgramIndex:], ShouldResemble, valueA[core.Cfg.ProgramIndex:])
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
			valueA.installFirmware(core.FirmwareTypeViral)

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
			valueA.installFirmware(core.FirmwareTypeViral)

			Convey("And ValueB is Folded into ValueA", func() {
				n, err := io.Copy(valueA, valueB)
				So(err, ShouldBeNil)
				So(n, ShouldEqual, 1024)

				Convey("ValueB should have the Viral Firmware", func() {
					buf := bytes.NewBuffer(make([]byte, 0, 1024))
					_, err = io.Copy(buf, valueA)
					So(err, ShouldBeNil)
					valueFrom(buf.Bytes(), valueB)
					So(valueB[core.Cfg.ProgramIndex:], ShouldResemble, valueA[core.Cfg.ProgramIndex:])
				})

				Convey("And ValueC is Folded into ValueB", func() {
					n, err := io.Copy(valueB, valueC)
					So(err, ShouldBeNil)
					So(n, ShouldEqual, 1024)

					Convey("ValueC should have the Viral Firmware", func() {
						buf := bytes.NewBuffer(make([]byte, 0, 1024))
						_, err = io.Copy(buf, valueB)
						So(err, ShouldBeNil)
						valueFrom(buf.Bytes(), valueC)
						So(valueC[core.Cfg.ProgramIndex:], ShouldResemble, valueB[core.Cfg.ProgramIndex:])
					})
				})
			})
		})
	})
}

func TestInstallPayloadProgram(t *testing.T) {
	Convey("Installing a payload program should arm the payload entry", t, func() {
		value, err := NewValue(nil)
		So(err, ShouldBeNil)
		defer value.Close()

		payload := []uint32{core.EncodeWriteRegisterImmediate(7, core.Cfg.R8)}
		value.InstallPayloadProgram(payload)

		So(value[core.Cfg.RegPC], ShouldEqual, core.PayloadProgramPCStart())
		So(value.ProgramOp(int(core.PayloadProgramPCStart())), ShouldEqual, uint8(payload[0]&0xF))
	})
}

func TestInstallProgramClearsTrailingInstructions(t *testing.T) {
	Convey("Installing a short program should clear trailing instruction slots", t, func() {
		value, err := NewValue(nil)
		So(err, ShouldBeNil)
		defer value.Close()

		program := []uint32{core.EncodeWriteRegisterImmediate(3, core.Cfg.R6)}
		value.InstallProgram(program)

		So(uint32(value[core.Cfg.ProgramIndex]), ShouldEqual, program[0])
		So(uint32(value[core.Cfg.ProgramIndex]>>32), ShouldEqual, uint32(0))

		for word := core.Cfg.ProgramIndex + 1; word < primitiveProgramWordLimit(); word++ {
			So(value[word], ShouldEqual, uint64(0))
		}
	})
}

func primitiveProgramWordLimit() int {
	return min(core.Cfg.ProgramIndex+int((core.Cfg.ProgramBits+63)/64), Words)
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
