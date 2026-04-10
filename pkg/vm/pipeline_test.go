package vm

import (
	"bytes"
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/transport"
)

type countingPublishable struct {
	n int
}

func (counter *countingPublishable) Publish(value *primitive.Value, _ string) error {
	if value == nil {
		return nil
	}

	counter.n++

	return nil
}

func TestPipeline(t *testing.T) {
	setupTokenizerValueConfig(t)

	Convey("Pipeline copies dataset through tokenizer into Publish", t, func() {
		ctx := context.Background()
		tokenizer, err := NewTokenizer(ctx, mustTestQueue(t))

		So(err, ShouldBeNil)

		chunkBytes := core.Cfg.Value.Region.MaxTokenIngestBytes()
		payload := bytes.Repeat([]byte{'p'}, chunkBytes*5)
		source := bytes.NewReader(payload)

		counter := &countingPublishable{}

		pipeline, err := transport.NewPipeline(
			ctx,
			false,
			tokenizer,
			counter,
		)

		So(err, ShouldBeNil)

		err = pipeline.LoadFrom(source)

		So(err, ShouldBeNil)
		So(counter.n, ShouldEqual, 5)
	})
}

func TestNestedPipelineBothStagesPublish(t *testing.T) {
	setupTokenizerValueConfig(t)

	Convey("Outer pipeline stages inner pipeline publishes every frame twice", t, func() {
		ctx := context.Background()
		tokenizer, err := NewTokenizer(ctx, mustTestQueue(t))

		So(err, ShouldBeNil)

		chunkBytes := core.Cfg.Value.Region.MaxTokenIngestBytes()
		frames := 4
		payload := bytes.Repeat([]byte{'n'}, chunkBytes*frames)
		source := bytes.NewReader(payload)

		innerPub := &countingPublishable{}
		outerPub := &countingPublishable{}

		innerPipeline, err := transport.NewPipeline(
			ctx,
			true,
			tokenizer,
			innerPub,
		)

		So(err, ShouldBeNil)

		outerPipeline, err := transport.NewPipeline(
			ctx,
			false,
			innerPipeline,
			outerPub,
		)

		So(err, ShouldBeNil)

		err = outerPipeline.LoadFrom(source)

		So(err, ShouldBeNil)
		So(innerPub.n, ShouldEqual, frames)
		So(outerPub.n, ShouldEqual, frames)
	})
}

func BenchmarkPipeline_LoadFrom(b *testing.B) {
	setupTokenizerValueConfig(b)

	ctx := context.Background()
	queue, err := pool.NewQueue(ctx)

	if err != nil {
		b.Fatal(err)
	}

	defer func() {
		queue.Drain()
		_ = queue.Close()
	}()

	tokenizer, err := NewTokenizer(ctx, queue)

	if err != nil {
		b.Fatal(err)
	}

	defer tokenizer.Close()

	chunkBytes := core.Cfg.Value.Region.MaxTokenIngestBytes()
	payload := bytes.Repeat([]byte{'q'}, chunkBytes*32)
	counter := &countingPublishable{}

	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		pipeline, prepErr := transport.NewPipeline(
			ctx,
			false,
			tokenizer,
			counter,
		)

		if prepErr != nil {
			b.Fatal(prepErr)
		}

		if loadErr := pipeline.LoadFrom(bytes.NewReader(payload)); loadErr != nil {
			b.Fatal(loadErr)
		}
	}
}

func BenchmarkNestedPipeline_LoadFrom(b *testing.B) {
	setupTokenizerValueConfig(b)

	ctx := context.Background()
	queue, err := pool.NewQueue(ctx)

	if err != nil {
		b.Fatal(err)
	}

	defer func() {
		queue.Drain()
		_ = queue.Close()
	}()

	tokenizer, err := NewTokenizer(ctx, queue)

	if err != nil {
		b.Fatal(err)
	}

	defer tokenizer.Close()

	chunkBytes := core.Cfg.Value.Region.MaxTokenIngestBytes()
	payload := bytes.Repeat([]byte{'u'}, chunkBytes*16)
	innerPub := &countingPublishable{}
	outerPub := &countingPublishable{}

	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		innerPipeline, innerErr := transport.NewPipeline(
			ctx,
			true,
			tokenizer,
			innerPub,
		)

		if innerErr != nil {
			b.Fatal(innerErr)
		}

		outerPipeline, outerErr := transport.NewPipeline(
			ctx,
			false,
			innerPipeline,
			outerPub,
		)

		if outerErr != nil {
			b.Fatal(outerErr)
		}

		if loadErr := outerPipeline.LoadFrom(bytes.NewReader(payload)); loadErr != nil {
			b.Fatal(loadErr)
		}
	}
}
