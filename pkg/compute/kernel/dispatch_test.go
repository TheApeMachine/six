package kernel

import (
	"bytes"
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
)

func TestNewBuilder(t *testing.T) {
	Convey("Given a builder with a CPU backend", t, func() {
		builder := NewBuilder(WithBackend(&cpu.Backend{}))

		Convey("It should report availability", func() {
			n, err := builder.Available()
			So(err, ShouldBeNil)
			So(n, ShouldBeGreaterThan, 0)
		})

		Convey("It should round-trip bytes through the best backend", func() {
			payload := []byte("hello kernel")

			n, err := builder.Write(payload)
			So(err, ShouldBeNil)
			So(n, ShouldEqual, len(payload))

			out := new(bytes.Buffer)
			nn, readErr := io.Copy(out, builder)
			So(readErr, ShouldBeNil)
			So(nn, ShouldEqual, int64(len(payload)))
			So(bytes.Equal(out.Bytes(), payload), ShouldBeTrue)
		})
	})
}

func TestNewBuilderEmpty(t *testing.T) {
	Convey("Given a builder with no backends", t, func() {
		builder := NewBuilder()

		Convey("It should return EOF on Read", func() {
			buf := make([]byte, 16)
			n, err := builder.Read(buf)
			So(n, ShouldEqual, 0)
			So(err, ShouldEqual, io.EOF)
		})

		Convey("It should return ErrClosedPipe on Write", func() {
			_, err := builder.Write([]byte("x"))
			So(err, ShouldEqual, io.ErrClosedPipe)
		})
	})
}

func BenchmarkBuilderRoundTrip(b *testing.B) {
	builder := NewBuilder(WithBackend(&cpu.Backend{}))
	payload := []byte("benchmark payload of reasonable size for throughput testing")

	var out bytes.Buffer
	out.Grow(len(payload))

	b.ReportAllocs()

	for b.Loop() {
		builder.active = nil
		builder.Write(payload)

		out.Reset()
		io.Copy(&out, builder)
	}
}
