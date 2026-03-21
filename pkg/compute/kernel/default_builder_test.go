package kernel

import (
	"bytes"
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewBuilder_DefaultDoesNotPanic(t *testing.T) {
	Convey("Given the default kernel builder with no backends", t, func() {
		builder := NewBuilder()

		Convey("It should not panic on io operations", func() {
			buf := make([]byte, 32)
			n, err := builder.Read(buf)
			So(n, ShouldEqual, 0)
			So(err, ShouldEqual, io.EOF)
		})
	})
}

func TestNewBuilder_CPUFallback(t *testing.T) {
	Convey("Given a builder with a CPU backend", t, func() {
		backend := &bytes.Buffer{}
		builder := NewBuilder(WithBackend(&bufBackend{buf: backend}))

		Convey("It should round-trip data", func() {
			payload := []byte("test data")

			n, err := builder.Write(payload)
			So(err, ShouldBeNil)
			So(n, ShouldEqual, len(payload))

			out := make([]byte, len(payload))
			n, err = builder.Read(out)
			So(err, ShouldBeNil)
			So(n, ShouldEqual, len(payload))
			So(bytes.Equal(out, payload), ShouldBeTrue)
		})
	})
}

type bufBackend struct {
	buf *bytes.Buffer
}

func (b *bufBackend) Available() (int, error)     { return 1, nil }
func (b *bufBackend) Read(p []byte) (int, error)  { return b.buf.Read(p) }
func (b *bufBackend) Write(p []byte) (int, error) { return b.buf.Write(p) }
func (b *bufBackend) Close() error                { b.buf.Reset(); return nil }

func BenchmarkBuilder_RoundTrip(b *testing.B) {
	backend := &bytes.Buffer{}
	bufBackend := &bufBackend{buf: backend}
	builder := NewBuilder(WithBackend(bufBackend))
	payload := []byte("benchmark payload for builder write/read path")
	out := make([]byte, len(payload))

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		backend.Reset()
		builder.Reset()
		builder.Write(payload)
		builder.Read(out)
	}
}
