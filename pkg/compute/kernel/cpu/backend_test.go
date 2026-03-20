package cpu

import (
	"bytes"
	"io"
	"math/rand"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestBackendAvailable(t *testing.T) {
	Convey("Given a CPU backend", t, func() {
		backend := &Backend{}

		Convey("It should report at least one CPU", func() {
			n, err := backend.Available()
			So(err, ShouldBeNil)
			So(n, ShouldBeGreaterThan, 0)
		})
	})
}

func TestBackendWriteReadRoundTrip(t *testing.T) {
	Convey("Given a CPU backend", t, func() {
		backend := &Backend{}

		cases := []struct {
			label string
			data  []byte
		}{
			{"empty", nil},
			{"single byte", []byte{0x42}},
			{"small", []byte("hello world")},
			{"4k", make([]byte, 4096)},
		}

		rand.Read(cases[3].data)

		for _, tc := range cases {
			Convey("It should round-trip "+tc.label, func() {
				if len(tc.data) > 0 {
					n, err := backend.Write(tc.data)
					So(err, ShouldBeNil)
					So(n, ShouldEqual, len(tc.data))
				}

				out := new(bytes.Buffer)
				nn, err := io.Copy(out, backend)

				So(err, ShouldBeNil)
				So(nn, ShouldEqual, int64(len(tc.data)))
				So(bytes.Equal(out.Bytes(), tc.data), ShouldBeTrue)

				backend.Reset()
			})
		}
	})
}

func BenchmarkBackendRoundTrip(b *testing.B) {
	payload := make([]byte, 4096)
	rand.Read(payload)

	backend := &Backend{}

	var out bytes.Buffer
	out.Grow(len(payload))

	b.ReportAllocs()

	for b.Loop() {
		backend.Reset()
		backend.Write(payload)

		out.Reset()
		io.Copy(&out, backend)
	}
}
