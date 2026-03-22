package transport

import (
	"bytes"
	"errors"
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

/*
feedbackForward is a minimal ReadWriter for Feedback tests.
*/
type feedbackForward struct {
	bytes.Buffer
}

/*
closableBuffer is a bytes.Buffer with io.Closer for Feedback.Close tests.
*/
type closableBuffer struct {
	bytes.Buffer
	closeErr error
}

/*
Close implements io.Closer.
*/
func (buffer *closableBuffer) Close() error {
	return buffer.closeErr
}

/*
writeCloserStub implements io.WriteCloser for backward Feedback wiring.
*/
type writeCloserStub struct {
	closeErr error
}

/*
Write implements io.Writer.
*/
func (stub *writeCloserStub) Write(p []byte) (n int, err error) {
	return len(p), nil
}

/*
Close implements io.Closer.
*/
func (stub *writeCloserStub) Close() error {
	return stub.closeErr
}

/*
brokenWriter always fails on Write.
*/
type brokenWriter struct{}

func (brokenWriter) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}

func (brokenWriter) Write(p []byte) (n int, err error) {
	return 0, io.ErrClosedPipe
}

func TestFeedback(t *testing.T) {
	Convey("Write should hit forward only", t, func() {
		forward := new(feedbackForward)
		backward := new(bytes.Buffer)
		feedback := NewFeedback(forward, backward)

		n, err := feedback.Write([]byte("alpha"))
		So(err, ShouldBeNil)
		So(n, ShouldEqual, 5)
		So(forward.String(), ShouldEqual, "alpha")
		So(backward.Len(), ShouldEqual, 0)
	})

	Convey("Read should tee forward into backward", t, func() {
		forward := new(feedbackForward)
		backward := new(bytes.Buffer)
		feedback := NewFeedback(forward, backward)

		_, err := forward.WriteString("tee-data")
		So(err, ShouldBeNil)

		buf := make([]byte, 32)
		n, err := feedback.Read(buf)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, 8)
		So(string(buf[:n]), ShouldEqual, "tee-data")
		So(backward.String(), ShouldEqual, "tee-data")
	})

	Convey("Write should propagate forward errors", t, func() {
		feedback := NewFeedback(&brokenWriter{}, new(bytes.Buffer))

		n, err := feedback.Write([]byte{1})
		So(n, ShouldEqual, 0)
		So(err, ShouldEqual, io.ErrClosedPipe)
	})

	Convey("Close should join forward and backward close errors", t, func() {
		fErr := errors.New("forward-close")
		bErr := errors.New("backward-close")

		feedback := NewFeedback(
			&closableBuffer{closeErr: fErr},
			&writeCloserStub{closeErr: bErr},
		)

		closeErr := feedback.Close()
		So(closeErr, ShouldNotBeNil)
		So(closeErr.Error(), ShouldContainSubstring, "forward-close")
		So(closeErr.Error(), ShouldContainSubstring, "backward-close")
	})
}

func BenchmarkFeedbackWriteRead(b *testing.B) {
	payload := []byte("benchmark-feedback-payload")
	readBuf := make([]byte, 32)

	b.ReportAllocs()

	for b.Loop() {
		forward := new(feedbackForward)
		backward := new(bytes.Buffer)
		feedback := NewFeedback(forward, backward)

		_, _ = forward.WriteString("read-src")
		_, _ = feedback.Read(readBuf)
		_, _ = feedback.Write(payload)
	}
}
