package primitive

import (
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
	"github.com/theapemachine/six/pkg/core"
)

func init() {
	viper.SetConfigFile("../../cmd/cfg/config.yml")
	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "primitive/value_test: viper.ReadInConfig: %v\n", err)
		os.Exit(1)
	}
	if err := core.LoadValueConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "primitive/value_test: core.LoadValueConfig: %v\n", err)
		os.Exit(1)
	}
}

func TestRead(t *testing.T) {
	Convey("Given a Value with a known data field", t, func() {
		value := NewValue()
		value[0] = 42
		value[core.Cfg.StateIndex] = 1 // Mark as not empty
		buf := make([]byte, ByteSize)

		Convey("It should serialize into a full-size buffer", func() {
			original := *value
			n, err := value.Read(buf)

			So(n, ShouldEqual, ByteSize)
			So(errors.Is(err, io.EOF), ShouldBeTrue)

			var roundtrip Value
			valueFrom(buf, &roundtrip)

			for w := range Words {
				So(roundtrip[w], ShouldEqual, original[w])
			}

			So(value[core.Cfg.StateIndex], ShouldEqual, original[core.Cfg.StateIndex])
		})
	})
}

func TestWrite(t *testing.T) {
	Convey("Given a Value", t, func() {
		Convey("It should accept a short payload and report bytes consumed accurately", func() {
			value := NewValue()
			want := []byte("hello")
			rest := want
			for len(rest) > 0 {
				n, err := value.Write(rest)
				So(err, ShouldBeNil)
				So(n, ShouldBeGreaterThan, 0)
				So(n, ShouldBeLessThanOrEqualTo, len(rest))
				rest = rest[n:]
			}

			for i, b := range want {
				tok := value.TokenID(i)
				So(byte(tok>>32), ShouldEqual, b)
			}

			value[core.Cfg.StateIndex] = 1
			readBack := make([]byte, ByteSize)
			_, rerr := value.Read(readBack)
			So(errors.Is(rerr, io.EOF), ShouldBeTrue)
			var decoded Value
			valueFrom(readBack, &decoded)
			for i, b := range want {
				tok := decoded.TokenID(i)
				So(byte(tok>>32), ShouldEqual, b)
			}
		})
	})
}

func TestRegion0Layout(t *testing.T) {
	Convey("Region0 layout invariants from config", t, func() {
		tokenWords := int((core.Cfg.TokenBits + 63) / 64)
		So(core.Cfg.TokenBits, ShouldEqual, tokenWords*64)
		So(core.Cfg.TokenBits%64, ShouldEqual, 0)
		So(core.Cfg.ValueID, ShouldEqual, core.Cfg.TokenIndex+tokenWords)
		So(core.Cfg.AffinityIndex, ShouldBeGreaterThan, core.Cfg.NextID)
		So(core.Cfg.AffinityIndex%64, ShouldEqual, 0)
	})
}

func TestRegion0RoundTrip(t *testing.T) {
	Convey("Given a Value with Region0 token and link words populated", t, func() {
		value := NewValue()

		for i := 0; i < 57; i++ {
			So(value.SetTokenID(i, Tokenize(byte('a'+i%26), uint64(i))), ShouldBeTrue)
		}
		value[core.Cfg.StateIndex] = 57 // Mark as not empty

		value.SetValueID(0xAABBCCDD)
		value.SetPrevValueID(0x11223344)
		value.SetNextValueID(0x55667788)

		buf := make([]byte, ByteSize)

		Convey("It should preserve the full Region0 payload across serialization", func() {
			original := *value
			n, err := value.Read(buf)
			So(n, ShouldEqual, ByteSize)
			So(errors.Is(err, io.EOF), ShouldBeTrue)

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

func TestClose(t *testing.T) {
	Convey("Given a Value", t, func() {
		value := NewValue()
		value[0] = 1

		Convey("It should return nil", func() {
			So(value.Close(), ShouldBeNil)
		})
	})
}

func BenchmarkNewValue(b *testing.B) {
	for b.Loop() {
		NewValue()
	}
}

func BenchmarkRead(b *testing.B) {
	value := NewValue()
	value[0] = 42
	buf := make([]byte, ByteSize)

	for b.Loop() {
		value.Read(buf)
	}
}

func BenchmarkWrite(b *testing.B) {
	src := NewValue()
	src[0] = 99
	buf := make([]byte, ByteSize)
	_, _ = src.Read(buf)

	for b.Loop() {
		dst := NewValue()
		_, _ = dst.Write(buf)
	}
}

func BenchmarkClose(b *testing.B) {
	value := NewValue()
	value[0] = 1

	for b.Loop() {
		value.Close()
	}
}
