package vm

import (
	"bytes"
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/transport"
)

type labelCapturePublishable struct {
	labels []string
}

func (capture *labelCapturePublishable) Publish(values ...*primitive.Value) ([]*primitive.Value, error) {
	return capture.PublishLabeled("", values...)
}

func (capture *labelCapturePublishable) PublishLabeled(
	label string,
	values ...*primitive.Value,
) ([]*primitive.Value, error) {
	for _, value := range values {
		if value != nil {
			_ = value.Close()
		}
	}

	capture.labels = append(capture.labels, label)

	return nil, nil
}

/*
propertiesWordCapture records kernel.PropertiesStartWord for each published Value.
*/
type propertiesWordCapture struct {
	words []uint64
}

func (capture *propertiesWordCapture) Publish(values ...*primitive.Value) ([]*primitive.Value, error) {
	for _, value := range values {
		if value != nil {
			capture.words = append(capture.words, (*value)[kernel.PropertiesStartWord])
			_ = value.Close()
		}
	}

	return nil, nil
}

func mustTestQueue(tb testing.TB) *pool.Queue {
	tb.Helper()

	queue, err := pool.NewQueue(context.Background())

	if err != nil {
		tb.Fatal(err)
	}

	tb.Cleanup(func() {
		queue.Drain()
		_ = queue.Close()
	})

	return queue
}

func setupTokenizerValueConfig(tb testing.TB) {
	tb.Helper()

	original := *core.Cfg
	tb.Cleanup(func() {
		*core.Cfg = original
	})

	core.Cfg.Value.Words = 128
	core.Cfg.Value.Bytes = 1024
	if core.Cfg.Programs == nil {
		core.Cfg.Programs = make(map[string]string)
	}
	core.Cfg.Programs["affinity"] = "tokens[0,16] tokens[0,16] affinity[0,5] xor accumulate"
	// Prompt installs these by name; tests do not load cmd/cfg/config.yml.
	core.Cfg.Programs["active_inference"] = core.Cfg.Programs["affinity"]
	core.Cfg.Programs["causal_explore"] = core.Cfg.Programs["affinity"]
}

func TestNewTokenizer(t *testing.T) {
	setupTokenizerValueConfig(t)

	Convey("When queue is nil", t, func() {
		Convey("NewTokenizer should fail validation", func() {
			tokenizer, err := NewTokenizer(t.Context(), nil)

			So(tokenizer, ShouldBeNil)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldEqual, "queue is required")
		})
	})
}

func TestTokenizerRead(t *testing.T) {
	setupTokenizerValueConfig(t)

	Convey("Read returns nil error when a full frame is delivered", t, func() {
		tokenizer, err := NewTokenizer(t.Context(), mustTestQueue(t))

		So(err, ShouldBeNil)
		So(tokenizer, ShouldNotBeNil)

		chunkLen := core.Cfg.Value.Region.MaxTokenIngestBytes()
		chunk := bytes.Repeat([]byte{'q'}, chunkLen)
		frameBuf := make([]byte, core.Cfg.Value.Bytes)

		wn, wErr := tokenizer.Write(chunk)

		So(wErr, ShouldBeNil)
		So(wn, ShouldEqual, chunkLen)

		rn, rErr := tokenizer.Read(frameBuf)

		So(rErr, ShouldBeNil)
		So(rn, ShouldEqual, core.Cfg.Value.Bytes)

		_, wErr = tokenizer.Write(chunk)

		So(wErr, ShouldBeNil)

		rn, rErr = tokenizer.Read(frameBuf)

		So(rErr, ShouldBeNil)
		So(rn, ShouldEqual, core.Cfg.Value.Bytes)
	})
}

func TestTokenizerDrainPublishedValues(t *testing.T) {
	setupTokenizerValueConfig(t)

	Convey("DrainPublishedValues publishes once per fixed chunk after ClosePipeWriter", t, func() {
		tokenizer, err := NewTokenizer(t.Context(), mustTestQueue(t))

		So(err, ShouldBeNil)

		chunkLen := core.Cfg.Value.Region.MaxTokenIngestBytes()
		chunks := 4
		payload := bytes.Repeat([]byte{'d'}, chunkLen*chunks)

		for offset := 0; offset < len(payload); offset += chunkLen {
			n, wErr := tokenizer.Write(payload[offset : offset+chunkLen])

			So(wErr, ShouldBeNil)
			So(n, ShouldEqual, chunkLen)
		}

		So(tokenizer.ClosePipeWriter(), ShouldBeNil)

		counter := &countingPublishable{}
		publishers := []transport.Publishable{counter}

		So(tokenizer.DrainPublishedValues(t.Context(), "", publishers, nil), ShouldBeNil)
		So(counter.n, ShouldEqual, chunks)

		tokenizer.ResetAfterEOF()
	})
}

func TestTokenizerIngestSample(t *testing.T) {
	setupTokenizerValueConfig(t)

	Convey("IngestSample stamps the same Properties word on every Morton segment", t, func() {
		tokenizer, err := NewTokenizer(t.Context(), mustTestQueue(t))

		So(err, ShouldBeNil)

		payload := bytes.Repeat([]byte{'m'}, 400)

		segments, segErr := primitive.NewValue(payload)
		So(segErr, ShouldBeNil)
		So(len(segments), ShouldBeGreaterThan, 1)

		capture := &propertiesWordCapture{}
		sample := data.Sample{
			Text:  payload,
			Label: []byte("9"),
		}

		So(tokenizer.IngestSample(t.Context(), sample, []transport.Publishable{capture}), ShouldBeNil)

		expected := kernel.LabelPropertiesWord(sample.Label)
		So(len(capture.words), ShouldEqual, len(segments))

		for _, word := range capture.words {
			So(word, ShouldEqual, expected)
		}

		tokenizer.ResetAfterEOF()
	})
}

func TestTokenizerIngestReader(t *testing.T) {
	setupTokenizerValueConfig(t)

	Convey("IngestReader publishes every chunk with the same label per reader", t, func() {
		tokenizer, err := NewTokenizer(t.Context(), mustTestQueue(t))

		So(err, ShouldBeNil)

		chunkLen := core.Cfg.Value.Region.MaxTokenIngestBytes()
		chunks := 3
		payload := bytes.Repeat([]byte{'i'}, chunkLen*chunks)
		capture := &labelCapturePublishable{}
		publishers := []transport.Publishable{capture}

		So(tokenizer.IngestReader(
			t.Context(),
			bytes.NewReader(payload),
			"class-a",
			publishers,
			nil,
		), ShouldBeNil)

		So(len(capture.labels), ShouldEqual, chunks)

		for _, label := range capture.labels {
			So(label, ShouldEqual, "class-a")
		}

		So(tokenizer.IngestReader(
			t.Context(),
			bytes.NewReader(bytes.Repeat([]byte{'j'}, chunkLen*2)),
			"class-b",
			publishers,
			nil,
		), ShouldBeNil)

		So(len(capture.labels), ShouldEqual, chunks+2)

		So(capture.labels[chunks], ShouldEqual, "class-b")
		So(capture.labels[chunks+1], ShouldEqual, "class-b")
	})
}

func BenchmarkTokenizerRead(b *testing.B) {
	setupTokenizerValueConfig(b)

	queue, err := pool.NewQueue(b.Context())

	if err != nil {
		b.Fatal(err)
	}

	defer func() {
		queue.Drain()
		_ = queue.Close()
	}()

	tokenizer, err := NewTokenizer(b.Context(), queue)

	if err != nil {
		b.Fatal(err)
	}

	chunkLen := core.Cfg.Value.Region.MaxTokenIngestBytes()
	chunk := bytes.Repeat([]byte{'r'}, chunkLen)
	frameBuf := make([]byte, core.Cfg.Value.Bytes)

	b.SetBytes(int64(core.Cfg.Value.Bytes))
	b.ResetTimer()

	for b.Loop() {
		if _, wErr := tokenizer.Write(chunk); wErr != nil {
			b.Fatal(wErr)
		}

		if _, rErr := tokenizer.Read(frameBuf); rErr != nil {
			b.Fatal(rErr)
		}
	}
}

func BenchmarkTokenizerDrainPublishedValues(b *testing.B) {
	setupTokenizerValueConfig(b)

	queue, err := pool.NewQueue(b.Context())

	if err != nil {
		b.Fatal(err)
	}

	defer func() {
		queue.Drain()
		_ = queue.Close()
	}()

	tokenizer, err := NewTokenizer(b.Context(), queue)

	if err != nil {
		b.Fatal(err)
	}

	defer tokenizer.Close()

	chunkLen := core.Cfg.Value.Region.MaxTokenIngestBytes()
	chunk := bytes.Repeat([]byte{'e'}, chunkLen)
	counter := &countingPublishable{}
	publishers := []transport.Publishable{counter}

	b.SetBytes(int64(chunkLen))
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		counter.n = 0

		if _, wErr := tokenizer.Write(chunk); wErr != nil {
			b.Fatal(wErr)
		}

		if cwErr := tokenizer.ClosePipeWriter(); cwErr != nil {
			b.Fatal(cwErr)
		}

		if dErr := tokenizer.DrainPublishedValues(b.Context(), "", publishers, nil); dErr != nil {
			b.Fatal(dErr)
		}

		tokenizer.ResetAfterEOF()
	}
}

func BenchmarkTokenizerIngestReader(b *testing.B) {
	setupTokenizerValueConfig(b)

	queue, err := pool.NewQueue(b.Context())

	if err != nil {
		b.Fatal(err)
	}

	defer func() {
		queue.Drain()
		_ = queue.Close()
	}()

	tokenizer, err := NewTokenizer(b.Context(), queue)

	if err != nil {
		b.Fatal(err)
	}

	defer tokenizer.Close()

	chunkLen := core.Cfg.Value.Region.MaxTokenIngestBytes()
	payload := bytes.Repeat([]byte{'g'}, chunkLen*4)
	counter := &countingPublishable{}
	publishers := []transport.Publishable{counter}

	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		counter.n = 0

		if err := tokenizer.IngestReader(
			b.Context(),
			bytes.NewReader(payload),
			"bench-lbl",
			publishers,
			nil,
		); err != nil {
			b.Fatal(err)
		}

		if counter.n != 4 {
			b.Fatalf("chunks: got %d want 4", counter.n)
		}
	}
}
