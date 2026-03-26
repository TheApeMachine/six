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
		value[StateSlotIndex] = 1 // Mark as not empty
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

			So(value[StateSlotIndex], ShouldEqual, original[StateSlotIndex])
		})

		Convey("Consume resets the value after serialization", func() {
			v := NewValue()
			v[0] = 42
			v[StateSlotIndex] = 1
			n, err := v.Consume(buf)
			So(n, ShouldEqual, ByteSize)
			So(errors.Is(err, io.EOF), ShouldBeTrue)
			So(v[StateSlotIndex], ShouldEqual, 0)
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
	Convey("Region0 reserves 57 tokens and 3 IDs", t, func() {
		So(DataWords, ShouldEqual, 60)
		So(DataBits, ShouldEqual, 60*64)
		So(RegionAffinityStart, ShouldEqual, 4096)
	})
}

func TestRegion0RoundTrip(t *testing.T) {
	Convey("Given a Value with Region0 token and link words populated", t, func() {
		value := NewValue()

		for i := 0; i < 57; i++ {
			So(value.SetTokenID(i, Tokenize(byte('a'+i%26), uint64(i))), ShouldBeTrue)
		}
		value[StateSlotIndex] = 57 // Mark as not empty

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
