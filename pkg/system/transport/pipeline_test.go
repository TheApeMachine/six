package transport

import (
	"bytes"
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestPipeline(t *testing.T) {
	Convey("Given an empty pipeline", t, func() {
		p := NewPipeline().(*Pipeline)

		Convey("Read should return EOF", func() {
			buf := make([]byte, 8)
			n, err := p.Read(buf)
			So(n, ShouldEqual, 0)
			So(err, ShouldEqual, io.EOF)
		})

		Convey("Write should succeed without components", func() {
			n, err := p.Write([]byte("x"))
			So(err, ShouldBeNil)
			So(n, ShouldEqual, 1)
		})

		Convey("Close should succeed", func() {
			So(p.Close(), ShouldBeNil)
		})
	})

	Convey("Given a single-component pipeline", t, func() {
		stage := bytes.NewBufferString("data")
		p := NewPipeline(stage).(*Pipeline)
		out := make([]byte, 16)

		Convey("Read should return staged bytes then EOF", func() {
			n, err := p.Read(out)
			So(err, ShouldBeNil)
			So(n, ShouldEqual, 4)
			So(string(out[:n]), ShouldEqual, "data")

			n, err = p.Read(out)
			So(n, ShouldEqual, 0)
			So(err, ShouldEqual, io.EOF)
		})
	})

	Convey("Given a two-stage pipeline", t, func() {
		first := new(bytes.Buffer)
		second := new(bytes.Buffer)
		p := NewPipeline(first, second).(*Pipeline)

		Convey("Write should flow first to second", func() {
			n, err := p.Write([]byte("ab"))
			So(err, ShouldBeNil)
			So(n, ShouldEqual, 2)
			So(second.String(), ShouldEqual, "ab")
		})
	})
}

func BenchmarkPipelineWriteTwoStage(b *testing.B) {
	first := new(bytes.Buffer)
	second := new(bytes.Buffer)
	p := NewPipeline(first, second).(*Pipeline)
	payload := []byte("benchmark-payload-bytes")

	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		first.Reset()
		second.Reset()
		_, _ = p.Write(payload)
	}
}
