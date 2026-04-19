package transport

import (
	"bytes"
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestGateWriterDropsWithoutShortMultiWriter(t *testing.T) {
	frame := bytes.Repeat([]byte{'z'}, 1024)

	Convey("Dropped frames still satisfy io.MultiWriter length contract", t, func() {
		var stream bytes.Buffer
		var tele bytes.Buffer

		gate := NewGateWriter(&stream, func(p []byte) []byte { return nil })
		mw := io.MultiWriter(gate, &tele)

		n, err := mw.Write(frame)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, len(frame))
		So(stream.Len(), ShouldEqual, 0)
		So(tele.Len(), ShouldEqual, len(frame))
	})

	Convey("Forwarded frames hit the wrapped writer", t, func() {
		var stream bytes.Buffer
		var tele bytes.Buffer

		gate := NewGateWriter(&stream, func(p []byte) []byte { return p })
		mw := io.MultiWriter(gate, &tele)

		n, err := mw.Write(frame)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, len(frame))
		So(stream.Bytes(), ShouldResemble, frame)
		So(tele.Bytes(), ShouldResemble, frame)
	})
}
