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
