package vm

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/experiment/data"
)

func TestNewTokenizer(t *testing.T) {
	t.Parallel()

	Convey("NewTokenizer returns a cancellable handle", t, func() {
		tokenizer, err := NewTokenizer(context.Background())

		So(err, ShouldBeNil)
		So(tokenizer, ShouldNotBeNil)
		So(tokenizer.Close(), ShouldBeNil)
	})
}

func TestTokenizer_Close(t *testing.T) {
	t.Parallel()

	Convey("Close cancels the tokenizer context", t, func() {
		ctx := context.Background()
		tokenizer, err := NewTokenizer(ctx)

		So(err, ShouldBeNil)

		So(tokenizer.Close(), ShouldBeNil)
		So(tokenizer.ctx.Err(), ShouldNotBeNil)
	})
}

func TestTokenizer_Error(t *testing.T) {
	t.Parallel()

	Convey("Error is nil before any ingest failure", t, func() {
		tokenizer, err := NewTokenizer(context.Background())

		So(err, ShouldBeNil)
		So(tokenizer.Error(), ShouldBeNil)
		So(tokenizer.Close(), ShouldBeNil)
	})
}

func TestTokenizer_IngestSample(t *testing.T) {
	setupTokenizerValueConfig()

	Convey("IngestSample rejects a nil Tokenizer", t, func() {
		var tokenizer *Tokenizer

		out, ingestErr := tokenizer.IngestSample(context.Background(), data.Sample{Text: []byte("x")})

		So(out, ShouldBeNil)
		So(ingestErr, ShouldNotBeNil)
	})

	Convey("IngestSample returns nil for empty text", t, func() {
		tokenizer, err := NewTokenizer(context.Background())

		So(err, ShouldBeNil)
		defer tokenizer.Close()

		out, ingestErr := tokenizer.IngestSample(context.Background(), data.Sample{Text: nil})

		So(ingestErr, ShouldBeNil)
		So(out, ShouldBeNil)

		out, ingestErr = tokenizer.IngestSample(context.Background(), data.Sample{Text: []byte{}})

		So(ingestErr, ShouldBeNil)
		So(out, ShouldBeNil)
	})

	Convey("IngestSample tokenizes non-empty text", t, func() {
		tokenizer, err := NewTokenizer(context.Background())

		So(err, ShouldBeNil)
		defer tokenizer.Close()

		out, ingestErr := tokenizer.IngestSample(
			context.Background(),
			data.Sample{Text: []byte("tokenizer coverage")},
		)

		So(ingestErr, ShouldBeNil)
		So(len(out), ShouldBeGreaterThan, 0)
	})

	Convey("IngestSample carries an optional label", t, func() {
		tokenizer, err := NewTokenizer(context.Background())

		So(err, ShouldBeNil)
		defer tokenizer.Close()

		out, ingestErr := tokenizer.IngestSample(
			context.Background(),
			data.Sample{
				Text:  []byte("labeled"),
				Label: []byte("L1"),
			},
		)

		So(ingestErr, ShouldBeNil)
		So(len(out), ShouldBeGreaterThan, 0)
	})
}

func BenchmarkTokenizerIngestSample(b *testing.B) {
	setupTokenizerValueConfig()

	tokenizer, err := NewTokenizer(context.Background())

	if err != nil {
		b.Fatal(err)
	}

	defer tokenizer.Close()

	sample := data.Sample{Text: []byte("benchmark tokenizer ingest path")}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		if _, ingestErr := tokenizer.IngestSample(context.Background(), sample); ingestErr != nil {
			b.Fatal(ingestErr)
		}
	}
}
