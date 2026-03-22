package primitive

import (
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestValueError(t *testing.T) {
	Convey("Given a ValueError", t, func() {
		Convey("It should return its string representation", func() {
			So(ErrShortValue.Error(), ShouldEqual, "value: buffer shorter than 1024 bytes")
		})
	})
}

func TestValue(t *testing.T) {
	Convey("Given a fresh Value", t, func() {
		value := NewValue()

		Convey("It should start with all bits zero", func() {
			So(value.PopCount(), ShouldEqual, 0)
			So(value.IsZero(), ShouldBeTrue)
		})

		Convey("It should set and query individual bits", func() {
			value.Set(0)
			value.Set(42)
			value.Set(8190)

			So(value.Has(0), ShouldBeTrue)
			So(value.Has(42), ShouldBeTrue)
			So(value.Has(8190), ShouldBeTrue)
			So(value.Has(1), ShouldBeFalse)
			So(value.PopCount(), ShouldEqual, 3)
		})

		Convey("It should round-trip through Read and Write", func() {
			value.Set(7)
			value.Set(100)
			value.Set(4000)

			buf := make([]byte, ByteSize)
			n, err := value.Read(buf)
			So(n, ShouldEqual, ByteSize)
			So(err, ShouldEqual, io.EOF)

			other := NewValue()
			n, err = other.Write(buf)
			So(n, ShouldEqual, ByteSize)
			So(err, ShouldBeNil)

			So(value.Equal(other), ShouldBeTrue)
		})

		Convey("It should satisfy io.ReadWriteCloser exactly", func() {
			var readWriteCloser io.ReadWriteCloser = value

			So(readWriteCloser, ShouldNotBeNil)
			So(readWriteCloser.Close(), ShouldBeNil)
		})

		Convey("Read should return ErrShortValue on undersized buffer", func() {
			n, err := value.Read(make([]byte, ByteSize-1))
			So(n, ShouldEqual, 0)
			So(err, ShouldEqual, ErrShortValue)
		})

		Convey("Write should return ErrShortValue on undersized buffer", func() {
			n, err := value.Write(make([]byte, ByteSize-1))
			So(n, ShouldEqual, 0)
			So(err, ShouldEqual, ErrShortValue)
		})

		Convey("Write should clamp the unused bit above CoreBits", func() {
			buf := make([]byte, ByteSize)
			buf[ByteSize-1] = 0xFF

			v := NewValue()
			v.Write(buf)
			So(v[Words-1]&^uint64(LastMask), ShouldEqual, 0)
		})

		Convey("Clamp should zero only the bit above CoreBits", func() {
			value[Words-1] = ^uint64(0)
			value.Clamp()
			So(value[Words-1]&^uint64(LastMask), ShouldEqual, 0)
			So(value[Words-1]&LastMask, ShouldEqual, uint64(LastMask))
		})

		Convey("IsZero should return false for a non-zero middle word", func() {
			value[64] = 1
			So(value.IsZero(), ShouldBeFalse)
		})

		Convey("Equal should return false for different Values", func() {
			other := NewValue()
			other.Set(42)
			So(value.Equal(other), ShouldBeFalse)
		})
	})
}

func TestValuePortableRoundTrip(t *testing.T) {
	Convey("Given the portable (big-endian) serialization path", t, func() {
		origTo, origFrom := valueTo, valueFrom
		valueTo, valueFrom = valueToPortable, valueFromPortable

		Reset(func() {
			valueTo, valueFrom = origTo, origFrom
		})

		Convey("It should round-trip a Value with scattered bits", func() {
			value := NewValue()
			value.Set(0)
			value.Set(63)
			value.Set(64)
			value.Set(511)
			value.Set(4096)
			value.Set(CoreBits - 1)

			buf := make([]byte, ByteSize)
			n, err := value.Read(buf)
			So(n, ShouldEqual, ByteSize)
			So(err, ShouldEqual, io.EOF)

			other := NewValue()
			n, err = other.Write(buf)
			So(n, ShouldEqual, ByteSize)
			So(err, ShouldBeNil)

			So(value.Equal(other), ShouldBeTrue)
		})

		Convey("It should round-trip a fully saturated Value", func() {
			value := NewValue()

			for i := range CoreBits {
				value.Set(i)
			}

			buf := make([]byte, ByteSize)
			value.Read(buf)

			other := NewValue()
			other.Write(buf)

			So(value.Equal(other), ShouldBeTrue)
		})

		Convey("It should produce the same bytes as the native path", func() {
			value := NewValue()

			for i := range 200 {
				value.Set(i * 41 % CoreBits)
			}

			portBuf := make([]byte, ByteSize)
			valueToPortable(value, portBuf)

			nativeBuf := make([]byte, ByteSize)
			origTo(value, nativeBuf)

			So(portBuf, ShouldResemble, nativeBuf)
		})

		Convey("It should reconstruct the same Value as the native path", func() {
			src := NewValue()

			for i := range 200 {
				src.Set(i * 41 % CoreBits)
			}

			buf := make([]byte, ByteSize)
			origTo(src, buf)

			portVal := NewValue()
			valueFromPortable(buf, portVal)

			nativeVal := NewValue()
			origFrom(buf, nativeVal)

			So(portVal.Equal(nativeVal), ShouldBeTrue)
			So(portVal.Equal(src), ShouldBeTrue)
		})
	})
}

func BenchmarkPortableRead(b *testing.B) {
	origTo := valueTo
	valueTo = valueToPortable

	defer func() { valueTo = origTo }()

	value := NewValue()

	for i := range 50 {
		value.Set(i * 37 % CoreBits)
	}

	buf := make([]byte, ByteSize)
	b.ReportAllocs()

	for b.Loop() {
		value.Read(buf)
	}
}

func BenchmarkPortableWrite(b *testing.B) {
	origTo, origFrom := valueTo, valueFrom
	valueTo, valueFrom = valueToPortable, valueFromPortable

	defer func() { valueTo, valueFrom = origTo, origFrom }()

	src := NewValue()

	for i := range 50 {
		src.Set(i * 37 % CoreBits)
	}

	buf := make([]byte, ByteSize)
	src.Read(buf)
	dst := NewValue()
	b.ReportAllocs()

	for b.Loop() {
		dst.Write(buf)
	}
}

func BenchmarkRead(b *testing.B) {
	value := NewValue()

	for i := range 50 {
		value.Set(i * 37 % CoreBits)
	}

	buf := make([]byte, ByteSize)
	b.ReportAllocs()

	for b.Loop() {
		value.Read(buf)
	}
}

func BenchmarkWrite(b *testing.B) {
	src := NewValue()

	for i := range 50 {
		src.Set(i * 37 % CoreBits)
	}

	buf := make([]byte, ByteSize)
	src.Read(buf)
	dst := NewValue()
	b.ReportAllocs()

	for b.Loop() {
		dst.Write(buf)
	}
}

func BenchmarkSet(b *testing.B) {
	value := NewValue()
	b.ReportAllocs()

	for b.Loop() {
		value.Set(4321)
	}
}

func BenchmarkHas(b *testing.B) {
	value := NewValue()
	value.Set(4321)
	b.ReportAllocs()

	for b.Loop() {
		value.Has(4321)
	}
}

func BenchmarkClamp(b *testing.B) {
	value := NewValue()
	value[Words-1] = ^uint64(0)
	b.ReportAllocs()

	for b.Loop() {
		value.Clamp()
	}
}

func BenchmarkPopCount(b *testing.B) {
	value := NewValue()

	for i := range 500 {
		value.Set(i * 17 % CoreBits)
	}

	b.ReportAllocs()

	for b.Loop() {
		value.PopCount()
	}
}

func BenchmarkIsZero(b *testing.B) {
	value := NewValue()
	b.ReportAllocs()

	for b.Loop() {
		value.IsZero()
	}
}

func BenchmarkEqual(b *testing.B) {
	a := NewValue()
	c := NewValue()

	for i := range 50 {
		a.Set(i * 37 % CoreBits)
		c.Set(i * 37 % CoreBits)
	}

	b.ReportAllocs()

	for b.Loop() {
		a.Equal(c)
	}
}
