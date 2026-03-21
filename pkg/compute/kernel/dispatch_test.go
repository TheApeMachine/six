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
		builder.Reset()
		builder.Write(payload)

		out.Reset()
		io.Copy(&out, builder)
	}
}

type countingBackend struct {
	buf            bytes.Buffer
	availableCalls int
}

func (backend *countingBackend) Available() (int, error) {
	backend.availableCalls++

	return 1, nil
}

func (backend *countingBackend) Read(p []byte) (int, error) {
	return backend.buf.Read(p)
}

func (backend *countingBackend) Write(p []byte) (int, error) {
	return backend.buf.Write(p)
}

func (backend *countingBackend) Close() error {
	backend.buf.Reset()

	return nil
}

func TestBuilderProbesOnEveryCall(t *testing.T) {
	Convey("Given a builder with a counting backend", t, func() {
		backend := &countingBackend{}
		builder := NewBuilder(WithBackend(backend))

		payload := []byte("x")

		Convey("It should probe Available on every io call", func() {
			n, err := builder.Write(payload)
			So(err, ShouldBeNil)
			So(n, ShouldEqual, len(payload))
			So(backend.availableCalls, ShouldBeGreaterThanOrEqualTo, 1)

			callsAfterWrite := backend.availableCalls

			out := make([]byte, len(payload))
			n, err = builder.Read(out)
			So(err, ShouldBeNil)
			So(n, ShouldEqual, len(payload))
			So(backend.availableCalls, ShouldBeGreaterThan, callsAfterWrite)
		})
	})
}

func TestBuilderSelectsHighestCapacity(t *testing.T) {
	Convey("Given two backends with different capacity", t, func() {
		low := &capacityBackend{capacity: 1}
		high := &capacityBackend{capacity: 10}
		builder := NewBuilder(WithBackend(low), WithBackend(high))

		Convey("It should route to the backend with higher capacity", func() {
			payload := []byte("routed")
			n, err := builder.Write(payload)
			So(err, ShouldBeNil)
			So(n, ShouldEqual, len(payload))
			So(high.writeCount, ShouldEqual, 1)
			So(low.writeCount, ShouldEqual, 0)
		})
	})
}

type capacityBackend struct {
	capacity   int
	writeCount int
	buf        bytes.Buffer
}

func (backend *capacityBackend) Available() (int, error) {
	return backend.capacity, nil
}

func (backend *capacityBackend) Read(p []byte) (int, error) {
	return backend.buf.Read(p)
}

func (backend *capacityBackend) Write(p []byte) (int, error) {
	backend.writeCount++
	return backend.buf.Write(p)
}

func (backend *capacityBackend) Close() error {
	return nil
}
