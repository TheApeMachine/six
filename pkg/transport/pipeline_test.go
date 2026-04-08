package transport

import (
	"context"
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

type emptyFramePipe struct{}

func (emptyFramePipe) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	return 0, io.EOF
}

func (emptyFramePipe) Write(p []byte) (int, error) {
	return len(p), nil
}

func (emptyFramePipe) CloseWrite() error {
	return nil
}

func TestPipelineReadNoEgress(t *testing.T) {
	Convey("Read without egress returns ErrPipelineNoEgress", t, func() {
		ctx := context.Background()
		pipeline, err := NewPipeline(ctx, false, emptyFramePipe{}, noopPublishable{})

		So(err, ShouldBeNil)

		_, rErr := pipeline.Read(make([]byte, 4))
		So(rErr, ShouldEqual, ErrPipelineNoEgress)

		So(pipeline.Finish(), ShouldBeNil)
	})
}

func TestPipelineFinishIdempotent(t *testing.T) {
	Convey("Finish is idempotent", t, func() {
		ctx := context.Background()
		pipeline, err := NewPipeline(ctx, false, emptyFramePipe{}, noopPublishable{})

		So(err, ShouldBeNil)
		So(pipeline.Finish(), ShouldBeNil)
		So(pipeline.Finish(), ShouldBeNil)
	})
}

func BenchmarkPipelineDrain(b *testing.B) {
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		pipeline, err := NewPipeline(ctx, false, emptyFramePipe{}, noopPublishable{})

		if err != nil {
			b.Fatal(err)
		}

		if finishErr := pipeline.Finish(); finishErr != nil {
			b.Fatal(finishErr)
		}
	}
}
