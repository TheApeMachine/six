package transport

import (
	"bytes"
	"errors"
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

type noopPublishable struct{}

func (noopPublishable) Publish(*primitive.Value, string) error {
	return nil
}

type captureFramesPublishable struct {
	frames [][]byte
}

func (capture *captureFramesPublishable) Publish(value *primitive.Value, _ string) error {
	if value == nil {
		return errors.New("nil value")
	}

	buf := make([]byte, core.Cfg.Value.Bytes)
	_, err := value.Read(buf)

	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}

	capture.frames = append(capture.frames, bytes.Clone(buf))

	return nil
}

func setupStreamWireConfig(tb testing.TB) {
	tb.Helper()

	original := *core.Cfg
	tb.Cleanup(func() {
		*core.Cfg = original
	})

	core.Cfg.Value.Words = 128
	core.Cfg.Value.Bytes = 1024
}

func wireFrame(tb testing.TB, payload []byte) []byte {
	tb.Helper()

	value, err := primitive.NewValue(payload)

	if err != nil {
		tb.Fatal(err)
	}

	defer value.Close()

	buf := make([]byte, core.Cfg.Value.Bytes)
	_, err = value.Read(buf)

	if err != nil && !errors.Is(err, io.EOF) {
		tb.Fatal(err)
	}

	return buf
}

func TestNewStream(t *testing.T) {
	Convey("NewStream rejects invalid arguments", t, func() {
		sink, err := NewStream(0, noopPublishable{})

		So(sink, ShouldBeNil)
		So(err, ShouldNotBeNil)

		sink, err = NewStream(8)

		So(sink, ShouldBeNil)
		So(err, ShouldNotBeNil)
	})
}

func TestStreamWrite(t *testing.T) {
	setupStreamWireConfig(t)

	Convey("Given a frame size and split writes", t, func() {
		frameA := wireFrame(t, []byte("alpha"))
		frameB := wireFrame(t, []byte("beta"))

		So(len(frameA), ShouldEqual, core.Cfg.Value.Bytes)
		So(len(frameB), ShouldEqual, core.Cfg.Value.Bytes)

		capture := &captureFramesPublishable{}
		stream, err := NewStream(core.Cfg.Value.Bytes, capture)

		So(err, ShouldBeNil)

		concat := append(append([]byte{}, frameA...), frameB...)
		split := 555

		n1, e1 := stream.Write(concat[:split])

		So(e1, ShouldBeNil)
		So(n1, ShouldEqual, split)

		n2, e2 := stream.Write(concat[split:])

		So(e2, ShouldBeNil)
		So(n2, ShouldEqual, len(concat)-split)

		So(stream.Close(), ShouldBeNil)

		So(len(capture.frames), ShouldEqual, 2)
		So(capture.frames[0], ShouldResemble, frameA)
		So(capture.frames[1], ShouldResemble, frameB)
	})
}

func TestStreamClose(t *testing.T) {
	setupStreamWireConfig(t)

	Convey("Close fails on partial frame", t, func() {
		prefix := wireFrame(t, []byte("gamma"))[:17]

		stream, err := NewStream(core.Cfg.Value.Bytes, noopPublishable{})

		So(err, ShouldBeNil)

		_, wErr := stream.Write(prefix)

		So(wErr, ShouldBeNil)
		So(stream.Close(), ShouldNotBeNil)
	})
}

func BenchmarkStream_Write(b *testing.B) {
	setupStreamWireConfig(b)

	payload := wireFrame(b, bytes.Repeat([]byte{'z'}, 16))

	stream, err := NewStream(core.Cfg.Value.Bytes, noopPublishable{})

	if err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(core.Cfg.Value.Bytes))
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := stream.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
}
