package primitive

import (
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
)

func setupPrimitiveValueTest(tb testing.TB) {
	tb.Helper()

	original := *core.Cfg
	tb.Cleanup(func() {
		*core.Cfg = original
	})

	core.Cfg.Value.Words = 128
	core.Cfg.Value.Bytes = 1024
}

/*
newValueFromZeroFrame builds a Value from an all-zero payload of length
Value.Bytes — for tests that overwrite token words after construction.
*/
func newValueFromZeroFrame(tb testing.TB) *Value {
	tb.Helper()

	payload := make([]byte, core.Cfg.Value.Bytes)
	value, err := NewValue(payload)

	if err != nil {
		tb.Fatal(err)
	}

	return value
}

func TestWritePreservesFrameIdentity(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("Given a full wire frame, Write preserves ID and layout", t, func() {
		first, err := NewValue([]byte("hello-world"))

		So(err, ShouldBeNil)
		So(first, ShouldNotBeNil)

		defer first.Close()

		expectedID := first.ID()

		frame := make([]byte, core.Cfg.Value.Bytes)
		_, readErr := first.Read(frame)

		So(readErr, ShouldEqual, io.EOF)

		raw := valuePool.Get()
		second := raw.(*Value)
		_, writeErr := second.Write(frame)

		So(writeErr, ShouldBeNil)

		defer second.Close()

		So(second.ID(), ShouldEqual, expectedID)
	})
}

func TestValueFromWireFrame(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("Given a wire frame from Read", t, func() {
		first, err := NewValue([]byte("hello-wire-frame"))
		So(err, ShouldBeNil)
		defer first.Close()

		expectedID := first.ID()
		expectedAff := first.AffinityVector()

		frame := make([]byte, core.Cfg.Value.Bytes)
		_, readErr := first.Read(frame)

		So(readErr, ShouldEqual, io.EOF)

		decoded, decErr := ValueFromWireFrame(frame)

		So(decErr, ShouldBeNil)
		So(decoded, ShouldNotBeNil)
		defer decoded.Close()

		So(decoded.ID(), ShouldEqual, expectedID)
		So(decoded.AffinityVector(), ShouldResemble, expectedAff)
	})
}

func TestNewValue(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("Given raw source bytes", t, func() {
		source := []byte("roy is in the kitchen")

		Convey("NewValue should load them into the literal token head", func() {
			value, err := NewValue(source)
			So(err, ShouldBeNil)
			So(value, ShouldNotBeNil)
			defer value.Close()

			So(value.ID(), ShouldNotEqual, 0)

			buf := make([]byte, core.Cfg.Value.Bytes)
			n, readErr := value.Read(buf)
			So(readErr, ShouldEqual, io.EOF)
			So(n, ShouldEqual, core.Cfg.Value.Bytes)
			So(buf[:len(source)], ShouldResemble, source)
		})

		Convey("String should trim trailing NUL padding in the token slab", func() {
			short := []byte("cat")
			value, err := NewValue(short)
			So(err, ShouldBeNil)
			defer value.Close()

			So(len(value.String()), ShouldEqual, len(short))
			So(value.String(), ShouldEqual, "cat")
		})

		Convey("TokenRegionBytes should match String as UTF-8 payload", func() {
			short := []byte("cat")
			value, err := NewValue(short)
			So(err, ShouldBeNil)
			defer value.Close()

			So(string(value.TokenRegionBytes()), ShouldEqual, value.String())
		})
	})
}

func TestRead(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("Given a populated Value", t, func() {
		source := []byte("roy is in the kitchen")
		value, err := NewValue(source)
		So(err, ShouldBeNil)
		defer value.Close()

		Convey("Read should serialize the full frame without copying semantics into higher layers", func() {
			buffer := make([]byte, core.Cfg.Value.Bytes)
			n, err := value.Read(buffer)

			So(err, ShouldEqual, io.EOF)
			So(n, ShouldEqual, core.Cfg.Value.Bytes)
			So(buffer[:len(source)], ShouldResemble, source)
		})
	})
}

func TestWrite(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("Given a serialized Value frame", t, func() {
		source := []byte("roy is in the kitchen")
		src, err := NewValue(source)
		So(err, ShouldBeNil)
		defer src.Close()

		buffer := make([]byte, core.Cfg.Value.Bytes)
		_, err = src.Read(buffer)
		So(err, ShouldEqual, io.EOF)
	})
}

func TestClose(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("Given a populated Value", t, func() {
		value, err := NewValue([]byte("roy is in the kitchen"))
		So(err, ShouldBeNil)
		So(value, ShouldNotBeNil)

		Convey("Close should wipe the frame before returning it to the pool", func() {
			err := value.Close()

			So(err, ShouldBeNil)
			So(*value, ShouldResemble, Value{})
		})
	})
}

func BenchmarkValue_Read(b *testing.B) {
	setupPrimitiveValueTest(b)

	value, err := NewValue([]byte("roy is in the kitchen"))

	if err != nil {
		b.Fatal(err)
	}

	defer value.Close()

	buffer := make([]byte, core.Cfg.Value.Bytes)
	b.SetBytes(int64(core.Cfg.Value.Bytes))
	b.ResetTimer()

	for b.Loop() {
		n, err := value.Read(buffer)

		if n != core.Cfg.Value.Bytes || err != io.EOF {
			b.Fatalf("Read: n=%d err=%v", n, err)
		}
	}
}

func BenchmarkValue_Write(b *testing.B) {
	setupPrimitiveValueTest(b)

	value, err := NewValue(make([]byte, core.Cfg.Value.Bytes))

	if err != nil {
		b.Fatal(err)
	}

	defer value.Close()

	payload := make([]byte, core.Cfg.Value.Bytes)

	for index := range payload {
		payload[index] = byte(index)
	}

	b.SetBytes(int64(core.Cfg.Value.Bytes))
	b.ResetTimer()

	for b.Loop() {
		if _, err := value.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValueFromWireFrame(b *testing.B) {
	setupPrimitiveValueTest(b)

	source, err := NewValue([]byte("benchmark wire decode"))
	if err != nil {
		b.Fatal(err)
	}

	frame := make([]byte, core.Cfg.Value.Bytes)
	if _, readErr := source.Read(frame); readErr != io.EOF {
		b.Fatalf("Read: %v", readErr)
	}

	if err := source.Close(); err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(core.Cfg.Value.Bytes))
	b.ResetTimer()

	for b.Loop() {
		v, vErr := ValueFromWireFrame(frame)
		if vErr != nil {
			b.Fatal(vErr)
		}

		if err := v.Close(); err != nil {
			b.Fatal(err)
		}
	}
}
