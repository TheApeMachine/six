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

func TestNewValue(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("Given raw source bytes", t, func() {
		source := []byte("roy is in the kitchen")

		Convey("NewValue should load them into the literal token head", func() {
			value, err := NewValue(source)
			So(err, ShouldBeNil)
			So(value, ShouldNotBeNil)
			defer value.Close()

			buf := make([]byte, core.Cfg.Value.Bytes)
			n, readErr := value.Read(buf)
			So(readErr, ShouldEqual, io.EOF)
			So(n, ShouldEqual, core.Cfg.Value.Bytes)
			So(buf[:len(source)], ShouldResemble, source)
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

	value, err := NewValue(nil)

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
