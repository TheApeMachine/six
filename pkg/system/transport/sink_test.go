package transport

import (
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestSink(t *testing.T) {
	Convey("Given a Sink", t, func() {
		sink := NewSink()
		buf := make([]byte, 16)

		Convey("Read should return EOF", func() {
			n, err := sink.Read(buf)
			So(n, ShouldEqual, 0)
			So(err, ShouldEqual, io.EOF)
		})

		Convey("Write should accept all bytes", func() {
			n, err := sink.Write([]byte("discard"))
			So(err, ShouldBeNil)
			So(n, ShouldEqual, 7)
		})

		Convey("Close should succeed", func() {
			So(sink.Close(), ShouldBeNil)
		})
	})
}

func BenchmarkSinkWrite(b *testing.B) {
	sink := NewSink()
	payload := make([]byte, 256)
	b.ReportAllocs()

	for b.Loop() {
		_, _ = sink.Write(payload)
	}
}
