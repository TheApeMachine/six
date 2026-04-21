package transport

import (
	"bytes"
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestCollectorLenNextWrite(t *testing.T) {
	Convey("Collector accumulates bytes and exposes Buffer-like Len/Next", t, func() {
		c := NewCollector()
		So(c.Len(), ShouldEqual, 0)

		n, err := c.Write([]byte{1, 2, 3})
		So(err, ShouldBeNil)
		So(n, ShouldEqual, 3)
		So(c.Len(), ShouldEqual, 3)

		chunk := c.Next(2)
		So(chunk, ShouldResemble, []byte{1, 2})
		So(c.Len(), ShouldEqual, 1)

		rest := c.Next(1)
		So(rest, ShouldResemble, []byte{3})
		So(c.Len(), ShouldEqual, 0)
		So(c.Next(1), ShouldBeNil)
	})
}

func TestCollectorWriteCopiesNotAliases(t *testing.T) {
	Convey("Write copies so reused buffers do not corrupt prior data", t, func() {
		c := NewCollector()
		shared := []byte{1, 2, 3}

		_, err := c.Write(shared)
		So(err, ShouldBeNil)

		shared[0] = 9

		So(c.Next(3), ShouldResemble, []byte{1, 2, 3})
	})
}

func TestCollectorRead(t *testing.T) {
	Convey("Read drains from the front", t, func() {
		c := NewCollector()
		_, _ = c.Write([]byte("abcd"))

		buf := make([]byte, 2)
		n, err := c.Read(buf)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, 2)
		So(buf, ShouldResemble, []byte("ab"))
		So(c.Len(), ShouldEqual, 2)

		n, err = c.Read(buf)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, 2)
		So(buf, ShouldResemble, []byte("cd"))

		n, err = c.Read(buf)
		So(n, ShouldEqual, 0)
		So(err, ShouldEqual, io.EOF)
	})
}

func TestCollectorIoCopyDoesNotUseReaderFrom(t *testing.T) {
	Convey("io.Copy into Collector uses Write path (smoke)", t, func() {
		c := NewCollector()
		src := bytes.NewReader(bytes.Repeat([]byte{'x'}, 100))

		written, err := io.Copy(c, src)
		So(err, ShouldBeNil)
		So(written, ShouldEqual, 100)
		So(c.Len(), ShouldEqual, 100)
	})
}
