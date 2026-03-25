package primitive

import (
	"errors"
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestRead(t *testing.T) {
	Convey("Given a Value with a known data field", t, func() {
		value := NewValue()
		value[0] = 42
		buf := make([]byte, ByteSize)

		Convey("It should serialize into a full-size buffer", func() {
			n, err := value.Read(buf)

			So(n, ShouldEqual, ByteSize)
			So(errors.Is(err, io.EOF), ShouldBeTrue)

			var roundtrip Value
			valueFrom(buf, &roundtrip)

			for w := range Words {
				So(roundtrip[w], ShouldEqual, value[w])
			}
		})

		Convey("It should stream a short buffer over multiple reads", func() {
			short := make([]byte, 100)
			n1, err1 := value.Read(short)
			So(n1, ShouldEqual, 100)
			So(err1, ShouldBeNil)

			rest := make([]byte, ByteSize)
			n2, err2 := value.Read(rest)
			So(n2, ShouldEqual, ByteSize-100)
			So(errors.Is(err2, io.EOF), ShouldBeTrue)

			full := make([]byte, ByteSize)
			copy(full, short[:n1])
			copy(full[n1:], rest[:n2])

			var roundtrip Value
			valueFrom(full, &roundtrip)
			for w := range Words {
				So(roundtrip[w], ShouldEqual, value[w])
			}
		})
	})
}

func TestWrite(t *testing.T) {
	Convey("Given a Value", t, func() {
		Convey("It should reject a short payload", func() {
			value := NewValue()
			short := make([]byte, ByteSize-1)
			n, err := value.Write(short)

			So(n, ShouldEqual, 0)
			So(errors.Is(err, io.ErrShortBuffer), ShouldBeTrue)
		})
	})
}

func TestRegion0Layout(t *testing.T) {
	Convey("Region0 reserves 57 tokens and 3 IDs", t, func() {
		So(DataWords, ShouldEqual, 60)
		So(DataBits, ShouldEqual, 60*64)
		So(InstrStart, ShouldEqual, DataBits)
	})
}

func TestRegion0RoundTrip(t *testing.T) {
	Convey("Given a Value with Region0 token and link words populated", t, func() {
		value := NewValue()

		for i := 0; i < 57; i++ {
			So(value.SetTokenID(i, Tokenize(byte('a'+i%26), uint64(i))), ShouldBeTrue)
		}

		value.SetValueID(0xAABBCCDD)
		value.SetPrevValueID(0x11223344)
		value.SetNextValueID(0x55667788)

		buf := make([]byte, ByteSize)

		Convey("It should preserve the full Region0 payload across serialization", func() {
			n, err := value.Read(buf)
			So(n, ShouldEqual, ByteSize)
			So(errors.Is(err, io.EOF), ShouldBeTrue)

			roundtrip := BytesToValue(buf)
			for i := 0; i < 57; i++ {
				So(roundtrip.TokenID(i), ShouldEqual, value.TokenID(i))
			}
			So(roundtrip.ValueID(), ShouldEqual, value.ValueID())
			So(roundtrip.PrevValueID(), ShouldEqual, value.PrevValueID())
			So(roundtrip.NextValueID(), ShouldEqual, value.NextValueID())
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

func BenchmarkNewValueFromByte(b *testing.B) {
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
