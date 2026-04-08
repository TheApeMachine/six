package vm

import (
	"bytes"
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/transport"
)

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

		chunkLen := int(core.Cfg.Value.Region.Tokens.Bits / 8)
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

		chunkLen := int(core.Cfg.Value.Region.Tokens.Bits / 8)
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

	chunkLen := int(core.Cfg.Value.Region.Tokens.Bits / 8)
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

	chunkLen := int(core.Cfg.Value.Region.Tokens.Bits / 8)
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
