package transport

import (
	"bytes"
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

type brokenFlipRW struct{}

func (brokenFlipRW) Read(p []byte) (n int, err error) {
	return 0, io.ErrUnexpectedEOF
}

func (brokenFlipRW) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func TestNewFlipFlop(t *testing.T) {
	Convey("Given two buffers", t, func() {
		from := bytes.NewBufferString("ping")
		to := new(bytes.Buffer)

		Convey("It should move bytes to the peer and copy the response back", func() {
			err := NewFlipFlop(from, to)
			So(err, ShouldBeNil)
			So(to.Len(), ShouldEqual, 0)

			back, rerr := io.ReadAll(from)
			So(rerr, ShouldBeNil)
			So(string(back), ShouldEqual, "ping")
		})
	})

	Convey("Given a failing reader", t, func() {
		Convey("It should return a wrapped error", func() {
			err := NewFlipFlop(brokenFlipRW{}, new(bytes.Buffer))
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "flipflop")
		})
	})

	Convey("Given a failing second Copy leg", t, func() {
		Convey("It should wrap the to->from error", func() {
			from := bytes.NewBufferString("ping")
			to := flipFlopFailRead{}

			err := NewFlipFlop(from, &to)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "to->from")
		})
	})
}

/*
flipFlopFailRead accepts writes into an internal buffer but Read always fails so the
second io.Copy leg errors. Buffer is not embedded: embedding would promote
bytes.Buffer.Read and shadow this type's Read in interface dispatch.
*/
type flipFlopFailRead struct {
	buf bytes.Buffer
}

func (reader *flipFlopFailRead) Write(p []byte) (n int, err error) {
	return reader.buf.Write(p)
}

func (reader *flipFlopFailRead) Read(p []byte) (n int, err error) {
	return 0, io.ErrUnexpectedEOF
}

func BenchmarkNewFlipFlop(b *testing.B) {
	from := bytes.NewBuffer(make([]byte, 128))
	to := new(bytes.Buffer)
	payload := make([]byte, 128)

	b.ReportAllocs()

	for b.Loop() {
		from.Reset()
		to.Reset()
		_, _ = from.Write(payload)
		_ = NewFlipFlop(from, to)
	}
}
