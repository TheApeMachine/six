package primitive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
)

func setupValueTests() {
	viper.SetConfigFile("../../cmd/cfg/config.yml")
	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "primitive/value_test: viper.ReadInConfig: %v\n", err)
		os.Exit(1)
	}
	if err := core.LoadValueConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "primitive/value_test: core.LoadValueConfig: %v\n", err)
		os.Exit(1)
	}
	loggingCfg, err := core.LoadLoggingConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "primitive/value_test: core.LoadLoggingConfig: %v\n", err)
		os.Exit(1)
	}
	errnie.InitLogger(loggingCfg)
}

func TestMain(m *testing.M) {
	setupValueTests()
	code := m.Run()
	_ = errnie.Shutdown(context.Background())
	os.Exit(code)
}

func TestNewValue(t *testing.T) {
	Convey("Given NewValue", t, func() {
		Convey("It should return a pooled Value with no tokens when p is nil", func() {
			v, err := NewValue(nil)
			So(err, ShouldBeNil)
			So(v, ShouldNotBeNil)
			defer v.Close()
		})

		Convey("It should write each byte into the token region when indices are valid", func() {
			payload := []byte("xy")
			v, err := NewValue(payload)
			So(err, ShouldBeNil)
			defer v.Close()
			So(v.TokenID(0), ShouldEqual, Tokenize('x', 0))
			So(v.TokenID(1), ShouldEqual, Tokenize('y', 1))
		})
	})
}

func TestValue_Read(t *testing.T) {
	Convey("Given (*Value).Read", t, func() {
		Convey("It should return io.ErrShortBuffer when len(p) < ByteSize", func() {
			value, err := NewValue(nil)
			So(err, ShouldBeNil)
			defer value.Close()
			value[core.Cfg.StateIndex] = 1

			short := make([]byte, ByteSize-1)
			n, rerr := value.Read(short)
			So(n, ShouldEqual, 0)
			So(errors.Is(rerr, io.ErrShortBuffer), ShouldBeTrue)
		})

		Convey("It should return io.EOF when StateIndex marks empty", func() {
			value, err := NewValue(nil)
			So(err, ShouldBeNil)
			defer value.Close()

			buf := make([]byte, ByteSize)
			n, rerr := value.Read(buf)
			So(n, ShouldEqual, 0)
			So(errors.Is(rerr, io.EOF), ShouldBeTrue)
		})

		Convey("It should serialize the full in-memory Value into p and end with io.EOF", func() {
			value, err := NewValue(nil)
			So(err, ShouldBeNil)
			defer value.Close()
			value[0] = 42
			value[core.Cfg.StateIndex] = 1
			buf := make([]byte, ByteSize)

			original := *value
			n, rerr := value.Read(buf)

			So(n, ShouldEqual, ByteSize)
			So(errors.Is(rerr, io.EOF), ShouldBeTrue)

			var roundtrip Value
			valueFrom(buf, &roundtrip)
			for w := range Words {
				So(roundtrip[w], ShouldEqual, original[w])
			}
			So(value[core.Cfg.StateIndex], ShouldEqual, original[core.Cfg.StateIndex])
		})
	})
}

func TestValue_Write(t *testing.T) {
	Convey("Given (*Value).Write", t, func() {
		Convey("It should accept an empty p as a no-op", func() {
			value, err := NewValue(nil)
			So(err, ShouldBeNil)
			defer value.Close()

			n, werr := value.Write(nil)
			So(werr, ShouldBeNil)
			So(n, ShouldEqual, 0)
		})

		Convey("It should consume a full wire frame of len ByteSize", func() {
			value, err := NewValue(nil)
			So(err, ShouldBeNil)
			defer value.Close()

			payload := make([]byte, ByteSize)
			payload[0] = 0xab
			n, werr := value.Write(payload)
			So(werr, ShouldBeNil)
			So(n, ShouldEqual, ByteSize)
		})
	})
}

func TestValue_Close(t *testing.T) {
	Convey("Given (*Value).Close", t, func() {
		Convey("It should return nil for a normal discard", func() {
			value, err := NewValue(nil)
			So(err, ShouldBeNil)
			value[0] = 1
			So(value.Close(), ShouldBeNil)
		})
	})
}

func TestBytesToValue(t *testing.T) {
	Convey("Given BytesToValue", t, func() {
		Convey("It should load a wire image independent of the pool", func() {
			src, err := NewValue(nil)
			So(err, ShouldBeNil)
			src[0] = 0xcd
			defer src.Close()

			buf := make([]byte, ByteSize)
			So(ValueToBytes(src, buf), ShouldBeNil)

			vp := BytesToValue(buf)
			So(vp, ShouldNotBeNil)
			So(vp[0], ShouldEqual, 0xcd)
		})
	})
}

func TestValueToBytes(t *testing.T) {
	Convey("Given ValueToBytes", t, func() {
		Convey("It should fill p with the Value wire layout", func() {
			v, err := NewValue(nil)
			So(err, ShouldBeNil)
			defer v.Close()
			v[0] = 0xef

			p := make([]byte, ByteSize)
			So(ValueToBytes(v, p), ShouldBeNil)
			var check Value
			valueFrom(p, &check)
			So(check[0], ShouldEqual, 0xef)
		})
	})
}

func TestTokenize(t *testing.T) {
	Convey("Given Tokenize", t, func() {
		Convey("It should pack the byte into the high 32 bits and index into the low 32 bits", func() {
			tok := Tokenize('Z', 7)
			So(byte(tok>>32), ShouldEqual, 'Z')
			So(uint32(tok), ShouldEqual, 7)
		})
	})
}

func TestValue_ApplyWireFrame(t *testing.T) {
	Convey("Given (*Value).ApplyWireFrame", t, func() {
		Convey("It should reject p when len(p) != ByteSize", func() {
			v, err := NewValue(nil)
			So(err, ShouldBeNil)
			defer v.Close()

			So(v.ApplyWireFrame(make([]byte, ByteSize-1)), ShouldNotBeNil)
		})

		Convey("It should copy a full frame into the Value", func() {
			src, err := NewValue(nil)
			So(err, ShouldBeNil)
			src[0] = 0x11
			buf := make([]byte, ByteSize)
			So(ValueToBytes(src, buf), ShouldBeNil)
			So(src.Close(), ShouldBeNil)

			dst, err := NewValue(nil)
			So(err, ShouldBeNil)
			defer dst.Close()
			So(dst.ApplyWireFrame(buf), ShouldBeNil)
			So(dst[0], ShouldEqual, 0x11)
		})
	})
}

func TestValue_Region0Layout(t *testing.T) {
	Convey("Given core.Cfg (LoadValueConfig) Region0 layout invariants", t, func() {
		tokenWords := int((core.Cfg.TokenBits + 63) / 64)
		So(core.Cfg.TokenBits, ShouldEqual, tokenWords*64)
		So(core.Cfg.TokenBits%64, ShouldEqual, 0)
		So(core.Cfg.ValueID, ShouldEqual, core.Cfg.TokenIndex+tokenWords)
		So(core.Cfg.AffinityIndex, ShouldBeGreaterThan, core.Cfg.NextID)
		So(core.Cfg.AffinityIndex%64, ShouldEqual, 0)
	})
}

func TestValue_Region0RoundTrip(t *testing.T) {
	Convey("Given a Value with Region0 tokens and link words set", t, func() {
		value, err := NewValue(nil)
		So(err, ShouldBeNil)
		defer value.Close()

		for i := 0; i < 57; i++ {
			So(value.SetTokenID(i, Tokenize(byte('a'+i%26), uint64(i))), ShouldBeTrue)
		}
		value[core.Cfg.StateIndex] = 57
		value.SetValueID(0xAABBCCDD)
		value.SetPrevValueID(0x11223344)
		value.SetNextValueID(0x55667788)

		buf := make([]byte, ByteSize)

		Convey("It should preserve Region0 across Read and BytesToValue", func() {
			original := *value
			n, rerr := value.Read(buf)
			So(n, ShouldEqual, ByteSize)
			So(errors.Is(rerr, io.EOF), ShouldBeTrue)

			roundtrip := BytesToValue(buf)
			for i := 0; i < 57; i++ {
				So(roundtrip.TokenID(i), ShouldEqual, original.TokenID(i))
			}
			So(roundtrip.ValueID(), ShouldEqual, original.ValueID())
			So(roundtrip.PrevValueID(), ShouldEqual, original.PrevValueID())
			So(roundtrip.NextValueID(), ShouldEqual, original.NextValueID())
		})
	})
}

func BenchmarkNewValue(b *testing.B) {
	for i := 0; i < b.N; i++ {
		v, _ := NewValue(nil)
		_ = v.Close()
	}
}

func BenchmarkValue_Read(b *testing.B) {
	value, _ := NewValue(nil)
	defer value.Close()
	value[0] = 42
	value[core.Cfg.StateIndex] = 1
	buf := make([]byte, ByteSize)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = value.Read(buf)
	}
}

func BenchmarkValue_Write(b *testing.B) {
	src, _ := NewValue(nil)
	defer src.Close()
	src[0] = 99
	buf := make([]byte, ByteSize)
	_, _ = src.Read(buf)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst, _ := NewValue(nil)
		_, _ = dst.Write(buf)
		_ = dst.Close()
	}
}

func BenchmarkValue_Close(b *testing.B) {
	for i := 0; i < b.N; i++ {
		v, _ := NewValue(nil)
		v[0] = 1
		_ = v.Close()
	}
}

func BenchmarkBytesToValue(b *testing.B) {
	buf := make([]byte, ByteSize)
	buf[0] = 0x77

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = BytesToValue(buf)
	}
}

func BenchmarkValueToBytes(b *testing.B) {
	v, _ := NewValue(nil)
	defer v.Close()
	v[0] = 0x55
	p := make([]byte, ByteSize)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValueToBytes(v, p)
	}
}
