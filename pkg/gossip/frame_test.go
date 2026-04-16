package gossip

import (
	"bytes"
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
)

/*
eofPerFrameReader simulates *primitive.Value.Read: one fixed-size payload
per Read, then io.EOF as the frame delimiter.
*/
type eofPerFrameReader struct {
	frameSize int
	remaining int
	payload   byte
}

func (reader *eofPerFrameReader) Read(p []byte) (int, error) {
	if reader.remaining <= 0 {
		return 0, io.EOF
	}

	if len(p) < reader.frameSize {
		return 0, io.ErrShortBuffer
	}

	for idx := 0; idx < reader.frameSize; idx++ {
		p[idx] = reader.payload
	}

	reader.remaining--
	reader.payload++

	return reader.frameSize, io.EOF
}

func TestFrameDelimitedReader(t *testing.T) {
	frameSize := core.Cfg.Value.Bytes

	Convey("Given a source that EOF-delimits each frame", t, func() {
		src := &eofPerFrameReader{frameSize: frameSize, remaining: 3, payload: 0x01}
		wrapped := FrameDelimitedReader(src)

		Convey("io.Copy reads every frame without stopping at the first EOF", func() {
			var buf bytes.Buffer

			// bytes.Buffer implements io.ReaderFrom; io.Copy would use ReadFrom and
			// drive the source with small initial buffers — smaller than one frame.
			// MultiWriter does not, so Copy uses a 32KiB buffer suitable for full frames.
			n, err := io.Copy(io.MultiWriter(&buf), wrapped)

			So(err, ShouldBeNil)
			So(n, ShouldEqual, int64(3*frameSize))
			So(buf.Len(), ShouldEqual, 3*frameSize)
		})

		Convey("io.LimitReader caps total bytes across frames", func() {
			src2 := &eofPerFrameReader{frameSize: frameSize, remaining: 10, payload: 0x10}
			limited := io.LimitReader(FrameDelimitedReader(src2), int64(3*frameSize))

			var buf bytes.Buffer

			n, err := io.Copy(io.MultiWriter(&buf), limited)

			So(err, ShouldBeNil)
			So(n, ShouldEqual, int64(3*frameSize))
		})
	})
}
