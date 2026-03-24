package primitive

import (
	"errors"
	"fmt"
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestIntegration(t *testing.T) {
	Convey("Given a stream of bytes", t, func() {
		data := make([]*Value, 256)
		for i := range 256 {
			data[i] = NewValueFromByte(byte(i))
		}

		Convey("When building the substrate", func() {
			values := make([]*Value, 0)

			for _, value := range data {
				newValue := NewValue()
				out := NewValue()
				_, err := io.Copy(newValue, value)
				So(err, ShouldBeNil)
				_, err = io.Copy(out, newValue)
				So(err, ShouldBeNil)
				values = append(values, out)
			}

			Convey("It should emit new Values from the seed", func() {
				So(len(values), ShouldEqual, 256)
			})
		})
	})
}

func TestNewValueFromByte(t *testing.T) {
	for i := range 256 {
		Convey(fmt.Sprintf(
			"Given a Value initialized with byte %d", i,
		), t, func() {
			value := NewValueFromByte(byte(i))

			Convey("Every motor orbit position should be set", func() {
				pos := i

				for range 5 {
					So(value[pos/64]&(1<<(pos%64)), ShouldNotEqual, 0)
					pos = (pos*3 + 1) % logicalBits
				}
			})

			Convey("No bits outside the data field should be set", func() {
				for w := DataWords; w < Words; w++ {
					So(value[w], ShouldEqual, 0)
				}
			})
		})
	}
}

func TestRead(t *testing.T) {
	Convey("Given a Value with a known data field", t, func() {
		value := NewValueFromByte(42)
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

		Convey("It should reject a short buffer", func() {
			short := make([]byte, ByteSize-1)
			n, err := value.Read(short)

			So(n, ShouldEqual, 0)
			So(errors.Is(err, io.ErrShortBuffer), ShouldBeTrue)
		})
	})
}

func TestWrite(t *testing.T) {
	Convey("Given a source Value and a destination Value", t, func() {
		src := NewValueFromByte(99)
		dstOccupied := NewValueFromByte(7)

		Convey("It should copy the incoming data field when the destination data field is empty", func() {
			dst := NewValue()
			buf := make([]byte, ByteSize)
			_, _ = src.Read(buf)

			n, err := dst.Write(buf)

			So(n, ShouldEqual, ByteSize)
			So(err, ShouldBeNil)
			for w := range 4 {
				So(dst[w], ShouldEqual, src[w])
			}
			So(dst[4]&1, ShouldEqual, src[4]&1)
		})

		Convey("It should reject a short payload", func() {
			short := make([]byte, ByteSize-1)
			n, err := dstOccupied.Write(short)

			So(n, ShouldEqual, 0)
			So(errors.Is(err, io.ErrShortBuffer), ShouldBeTrue)
		})

		Convey("It should copy incoming data into the operand when destination has data but incoming has no state vector (osmosis)", func() {
			buf := make([]byte, ByteSize)
			_, _ = src.Read(buf)

			var before [DataWords]uint64
			for w := range DataWords {
				before[w] = dstOccupied[w]
			}

			n, err := dstOccupied.Write(buf)

			So(n, ShouldEqual, ByteSize)
			So(err, ShouldBeNil)
			for w := range 4 {
				So(dstOccupied[w], ShouldEqual, before[w])
			}
			So(dstOccupied[4]&0x1F, ShouldEqual, before[4]&0x1F)
			So(dstOccupied[OperandStart>>6], ShouldNotEqual, 0)
		})

		Convey("It should copy incoming state vector into the operand without changing the data field", func() {
			var before [DataWords]uint64
			for w := range DataWords {
				before[w] = dstOccupied[w]
			}

			incoming := NewValue()
			incoming[StateStart>>6] |= 1

			buf := make([]byte, ByteSize)
			valueTo(incoming, buf)

			n, err := dstOccupied.Write(buf)

			So(n, ShouldEqual, ByteSize)
			So(err, ShouldBeNil)
			for w := range DataWords {
				So(dstOccupied[w], ShouldEqual, before[w])
			}
		})
	})
}

func TestClose(t *testing.T) {
	Convey("Given a Value", t, func() {
		value := NewValueFromByte(1)

		Convey("It should return nil", func() {
			So(value.Close(), ShouldBeNil)
		})
	})
}

func BenchmarkNewValueFromByte(b *testing.B) {
	for b.Loop() {
		NewValueFromByte(42)
	}
}

func BenchmarkRead(b *testing.B) {
	value := NewValueFromByte(42)
	buf := make([]byte, ByteSize)

	for b.Loop() {
		value.Read(buf)
	}
}

func BenchmarkWrite(b *testing.B) {
	src := NewValueFromByte(99)
	buf := make([]byte, ByteSize)
	_, _ = src.Read(buf)

	for b.Loop() {
		dst := NewValue()
		_, _ = dst.Write(buf)
	}
}

func BenchmarkClose(b *testing.B) {
	value := NewValueFromByte(1)

	for b.Loop() {
		value.Close()
	}
}
