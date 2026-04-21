package transport

import (
	"bytes"
	"context"
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestPipeline(t *testing.T) {
	Convey("Given a pipeline with components that produce data", t, func() {
		c1 := bytes.NewBuffer([]byte("data from first"))
		c2 := bytes.NewBuffer([]byte{})
		c3 := bytes.NewBuffer([]byte{})

		pipeline := NewPipeline(context.Background(), c1, c2)

		Convey("When reading without writing first", func() {
			n, err := io.Copy(c3, pipeline)
			So(n, ShouldNotEqual, 0)
			So(err, ShouldBeNil)
			So(c3.String(), ShouldEqual, "data from first")
		})
	})

	Convey("Given two pipelines with components that produce data", t, func() {
		in1 := bytes.NewBuffer([]byte("data from first"))
		buf := bytes.NewBuffer([]byte{})

		pipeline := NewPipeline(context.Background(), bytes.NewBuffer([]byte{}), bytes.NewBuffer([]byte{}))

		Convey("When reading after writing", func() {
			n, err := io.Copy(pipeline, in1)
			So(err, ShouldBeNil)
			So(n, ShouldEqual, len("data from first"))

			n, err = io.Copy(buf, pipeline)
			So(err, ShouldBeNil)
			So(n, ShouldEqual, len("data from first"))
			So(buf.String(), ShouldEqual, "data from first")
		})
	})
}

func TestRead(t *testing.T) {
	Convey("Given a pipeline with components that produce data", t, func() {
		c1 := bytes.NewBuffer([]byte("data from first"))
		c2 := bytes.NewBuffer([]byte{})
		c3 := make([]byte, c1.Len())

		pipeline := NewPipeline(context.Background(), c1, c2)
		n, err := pipeline.Read(c3)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, len("data from first"))
		So(string(c3), ShouldEqual, "data from first")
	})
}

func TestWrite(t *testing.T) {
	Convey("Given a pipeline with components that produce data", t, func() {
		c1 := bytes.NewBuffer([]byte("data from first"))
		c2 := bytes.NewBuffer([]byte{})
		c3 := bytes.NewBuffer([]byte{})
		c4 := bytes.NewBuffer([]byte{})

		pipeline := NewPipeline(context.Background(), c2, c3)
		n, err := io.Copy(pipeline, c1)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, len("data from first"))

		n, err = io.Copy(c4, pipeline)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, len("data from first"))
		So(c4.String(), ShouldEqual, "data from first")
	})
}

func TestEmptyPipeline(t *testing.T) {
	Convey("Given an empty pipeline with no components", t, func() {
		pipeline := NewPipeline(context.Background(), nil)
		buf := make([]byte, 10)

		Convey("When reading", func() {
			n, err := pipeline.Read(buf)
			So(err, ShouldEqual, io.EOF)
			So(n, ShouldEqual, 0)
		})

		Convey("When writing", func() {
			n, err := pipeline.Write([]byte("test"))
			So(err, ShouldEqual, io.ErrClosedPipe)
			So(n, ShouldEqual, 0)
		})
	})
}

func TestLargeDataPipeline(t *testing.T) {
	Convey("Given a pipeline with large data transfer", t, func() {
		// Create 1MB of test data
		largeData := make([]byte, 1024*1024)
		for i := range largeData {
			largeData[i] = byte(i % 256)
		}

		src := bytes.NewBuffer(largeData)
		intermediate := bytes.NewBuffer([]byte{})
		dest := bytes.NewBuffer([]byte{})

		pipeline := NewPipeline(context.Background(), intermediate)

		Convey("When copying large data through pipeline", func() {
			n, err := io.Copy(pipeline, src)
			So(err, ShouldBeNil)
			So(n, ShouldEqual, len(largeData))

			n, err = io.Copy(dest, pipeline)
			So(err, ShouldBeNil)
			So(n, ShouldEqual, len(largeData))
			So(bytes.Equal(dest.Bytes(), largeData), ShouldBeTrue)
		})
	})
}

func TestMultipleWriteReadCycles(t *testing.T) {
	Convey("Given a pipeline with multiple write-read cycles", t, func() {
		buf1 := bytes.NewBuffer([]byte{})
		buf2 := bytes.NewBuffer([]byte{})
		pipeline := NewPipeline(context.Background(), buf1, buf2)

		Convey("When performing multiple write-read cycles", func() {
			testData := []string{
				"first message",
				"second message",
				"third message",
			}

			for _, data := range testData {
				n, err := pipeline.Write([]byte(data))
				So(err, ShouldBeNil)
				So(n, ShouldEqual, len(data))

				result := make([]byte, len(data))
				n, err = pipeline.Read(result)
				So(err, ShouldBeNil)
				So(n, ShouldEqual, len(data))
				So(string(result), ShouldEqual, data)
			}
		})
	})
}

func TestEdgeCases(t *testing.T) {
	Convey("Given a pipeline testing edge cases", t, func() {
		buf1 := bytes.NewBuffer([]byte{})
		buf2 := bytes.NewBuffer([]byte{})
		pipeline := NewPipeline(context.Background(), buf1, buf2)

		Convey("When writing empty data", func() {
			n, err := pipeline.Write([]byte{})
			So(err, ShouldBeNil)
			So(n, ShouldEqual, 0)
		})

		Convey("When writing nil data", func() {
			n, err := pipeline.Write(nil)
			So(err, ShouldBeNil)
			So(n, ShouldEqual, 0)
		})

		Convey("When reading with zero-length buffer", func() {
			n, err := pipeline.Read([]byte{})
			So(err, ShouldEqual, io.EOF)
			So(n, ShouldEqual, 0)
		})

		Convey("When reading with nil buffer", func() {
			n, err := pipeline.Read(nil)
			So(err, ShouldEqual, io.EOF)
			So(n, ShouldEqual, 0)
		})
	})
}

func BenchmarkPipelineSmallData(b *testing.B) {
	payload := []byte("small payload")
	out := make([]byte, len(payload))

	b.ReportAllocs()

	for b.Loop() {
		buf1 := bytes.NewBuffer(make([]byte, 0, len(payload)))
		buf2 := bytes.NewBuffer(make([]byte, 0, len(payload)))
		pipeline := NewPipeline(context.Background(), buf1, buf2)

		if _, err := pipeline.Write(payload); err != nil {
			b.Fatalf("write: %v", err)
		}

		if _, err := pipeline.Read(out); err != nil && err != io.EOF {
			b.Fatalf("read: %v", err)
		}
	}
}

func BenchmarkPipelineLargeData(b *testing.B) {
	largeData := make([]byte, 1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	b.SetBytes(int64(len(largeData)))
	b.ReportAllocs()

	for b.Loop() {
		intermediate := bytes.NewBuffer(make([]byte, 0, len(largeData)))
		pipeline := NewPipeline(context.Background(), intermediate)

		if _, err := io.Copy(pipeline, bytes.NewReader(largeData)); err != nil {
			b.Fatalf("copy in: %v", err)
		}

		if _, err := io.Copy(io.Discard, pipeline); err != nil {
			b.Fatalf("copy out: %v", err)
		}
	}
}
