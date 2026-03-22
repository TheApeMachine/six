package transport

import (
	"bytes"
	"errors"
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

/*
failReadRW fails Read with a non-EOF error for Pipeline Copy coverage.
*/
type failReadRW struct{}

func (failReadRW) Read(p []byte) (n int, err error) {
	return 0, io.ErrUnexpectedEOF
}

func (failReadRW) Write(p []byte) (n int, err error) {
	return len(p), nil
}

/*
failReadBuffer embeds Buffer for Write but Read always errors.
*/
type failReadBuffer struct {
	bytes.Buffer
}

func (failReadBuffer) Read(p []byte) (n int, err error) {
	return 0, io.ErrUnexpectedEOF
}

/*
failWriteFirst fails the first Write in a Pipeline.
*/
type failWriteFirst struct{}

func (failWriteFirst) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}

func (failWriteFirst) Write(p []byte) (n int, err error) {
	return 0, io.ErrClosedPipe
}

/*
failWritePeer fails Write on the second stage during Pipeline Write fan-out.
*/
type failWritePeer struct{}

func (failWritePeer) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}

func (failWritePeer) Write(p []byte) (n int, err error) {
	return 0, io.ErrClosedPipe
}

/*
closerStage is a bytes.Buffer with an optional Close error.
*/
type closerStage struct {
	bytes.Buffer
	closeErr error
}

/*
Close implements io.Closer.
*/
func (stage *closerStage) Close() error {
	return stage.closeErr
}

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

	Convey("Given a two-stage Read where Copy fails", t, func() {
		p := NewPipeline(failReadRW{}, new(bytes.Buffer)).(*Pipeline)
		buf := make([]byte, 8)

		n, err := p.Read(buf)
		So(n, ShouldEqual, 0)
		So(err, ShouldEqual, io.ErrUnexpectedEOF)
	})

	Convey("Given a two-stage Read where the last stage Read errors", t, func() {
		p := NewPipeline(bytes.NewBufferString("hi"), &failReadBuffer{}).(*Pipeline)
		buf := make([]byte, 8)

		n, err := p.Read(buf)
		So(n, ShouldEqual, 0)
		So(err, ShouldEqual, io.ErrUnexpectedEOF)
	})

	Convey("Given a Write where the first stage fails", t, func() {
		p := NewPipeline(failWriteFirst{}, new(bytes.Buffer)).(*Pipeline)

		n, err := p.Write([]byte{1})
		So(n, ShouldEqual, 0)
		So(err, ShouldEqual, io.ErrClosedPipe)
	})

	Convey("Given a Write where the fan-out Copy fails", t, func() {
		p := NewPipeline(bytes.NewBufferString("x"), failWritePeer{}).(*Pipeline)

		n, err := p.Write([]byte{1})
		So(n, ShouldEqual, 1)
		So(err, ShouldEqual, io.ErrClosedPipe)
	})

	Convey("Given Close with multiple failing closers", t, func() {
		first := &closerStage{closeErr: errors.New("first-close")}
		second := &closerStage{closeErr: errors.New("second-close")}
		p := NewPipeline(first, second).(*Pipeline)

		closeErr := p.Close()
		So(closeErr, ShouldNotBeNil)
		So(closeErr.Error(), ShouldContainSubstring, "first-close")
		So(closeErr.Error(), ShouldContainSubstring, "second-close")
	})
}

func BenchmarkPipelineWriteTwoStage(b *testing.B) {
	first := new(bytes.Buffer)
	second := new(bytes.Buffer)
	p := NewPipeline(first, second).(*Pipeline)
	payload := []byte("benchmark-payload-bytes")

	b.ReportAllocs()

	for b.Loop() {
		first.Reset()
		second.Reset()
		_, _ = p.Write(payload)
	}
}

func BenchmarkPipelineCloseClosers(b *testing.B) {
	first := &closerStage{}
	second := &closerStage{}
	p := NewPipeline(first, second).(*Pipeline)

	b.ReportAllocs()

	for b.Loop() {
		_ = p.Close()
	}
}
