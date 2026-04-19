package errnie

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

type stubTraceStringer struct {
	s string
}

func (stub stubTraceStringer) TraceString() string {
	return stub.s
}

func TestFormatForTrace(t *testing.T) {
	t.Parallel()

	Convey("nil passes through", t, func() {
		So(formatForTrace(nil), ShouldBeNil)
	})

	Convey("TraceStringer values use TraceString", t, func() {
		So(formatForTrace(stubTraceStringer{s: "hello"}), ShouldEqual, "hello")
	})

	Convey("empty byte slice formats as len=0", t, func() {
		So(formatForTrace([]byte{}), ShouldEqual, "len=0")
	})

	Convey("short byte slice uses full hex", t, func() {
		So(formatForTrace([]byte{0xab, 0xcd}), ShouldEqual, "len=2 hex=abcd")
	})

	Convey("long byte slice truncates with prefix", t, func() {
		p := make([]byte, TraceByteHexPrefixLen+8)

		for index := range p {
			p[index] = byte(index)
		}

		out := formatForTrace(p).(string)

		So(out, ShouldStartWith, "len=")
		So(out, ShouldContainSubstring, "hex_prefix=")
	})

	Convey("opaque values pass through unchanged", t, func() {
		So(formatForTrace(42), ShouldEqual, 42)
	})
}

func TestTraceKeyvalsFormatted(t *testing.T) {
	t.Parallel()

	Convey("empty slice returns nil", t, func() {
		So(traceKeyvalsFormatted(nil), ShouldBeNil)
		So(traceKeyvalsFormatted([]any{}), ShouldBeNil)
	})

	Convey("pairs format odd-index values", t, func() {
		in := []any{"k", []byte{0x01}}

		out := traceKeyvalsFormatted(in)

		So(out[0], ShouldEqual, "k")
		So(out[1], ShouldEqual, "len=1 hex=01")
	})

	Convey("odd-length slice leaves trailing key and warns once", t, func() {
		out := traceKeyvalsFormatted([]any{"orphan"})

		So(len(out), ShouldEqual, 1)
		So(out[0], ShouldEqual, "orphan")
	})
}

func BenchmarkFormatForTrace_byteSlice(b *testing.B) {
	payload := make([]byte, TraceByteHexPrefixLen)

	for index := range payload {
		payload[index] = byte(index)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_ = formatForTrace(payload)
	}
}
