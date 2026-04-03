package vm

import (
	"bytes"
	"context"
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/theapemachine/six/pkg/cluster"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

// tokenizerTestContext returns a context that is not canceled when the
// testing.T completes. The ring buffer wires WithCancel to tokenizer.ctx; if
// the parent were t.Context(), the post-test cancellation race could close
// the buffer while synchronous Read/Write tests still run.
func tokenizerTestContext() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

func tokenizerTestComputeBackend(ctx context.Context) *compute.Backend {
	return compute.NewBackend(ctx)
}

func TestNewTokenizer(t *testing.T) {
	Convey("NewTokenizer", t, func() {
		Convey("wires a non-nil compute Backend via TokenizerWithBackend", func() {
			ctx, cancel := tokenizerTestContext()
			defer cancel()

			store := cluster.NewControlPlane(ctx)
			tokenizer, err := NewTokenizer(
				ctx,
				TokenizerWithStore(store),
				TokenizerWithBackend(tokenizerTestComputeBackend(ctx)),
			)
			So(err, ShouldBeNil)
			So(tokenizer, ShouldNotBeNil)
			So(tokenizer.Close(), ShouldNotBeNil)
		})
	})
}

func TestTokenizerRead(t *testing.T) {
	Convey("Read", t, func() {
		Convey("returns frame bytes once the ring has been fed", func() {
			ctx, cancel := tokenizerTestContext()
			defer cancel()

			store := cluster.NewControlPlane(ctx)
			tokenizer, err := NewTokenizer(
				ctx,
				TokenizerWithStore(store),
				TokenizerWithBackend(tokenizerTestComputeBackend(ctx)),
				TokenizerWithBuffer(8),
			)
			So(err, ShouldBeNil)
			defer tokenizer.Close()

			payload := bytes.Repeat([]byte("q"), core.Cfg.Value.Bytes)
			_, err = tokenizer.Write(payload)
			So(err, ShouldBeNil)

			out := make([]byte, 4096)
			n, rerr := tokenizer.Read(out)
			// Value.Read signals end-of-frame with io.EOF after filling the buffer.
			So(rerr, ShouldEqual, io.EOF)
			So(n, ShouldEqual, core.Cfg.Value.Bytes)
		})
	})
}

func TestTokenizerWrite(t *testing.T) {
	Convey("Write", t, func() {
		Convey("queues encoded frames for Read", func() {
			ctx, cancel := tokenizerTestContext()
			defer cancel()

			store := cluster.NewControlPlane(ctx)
			tokenizer, err := NewTokenizer(
				ctx,
				TokenizerWithStore(store),
				TokenizerWithBackend(tokenizerTestComputeBackend(ctx)),
				TokenizerWithBuffer(8),
			)
			So(err, ShouldBeNil)
			defer tokenizer.Close()

			payload := bytes.Repeat([]byte("w"), core.Cfg.Value.Bytes)
			n, werr := tokenizer.Write(payload)
			So(werr, ShouldBeNil)
			So(n, ShouldBeGreaterThan, 0)
		})
	})
}

func TestTokenizerWriteSerializedFrame(t *testing.T) {
	Convey("Write", t, func() {
		Convey("preserves serialized prompt metadata", func() {
			ctx, cancel := tokenizerTestContext()
			defer cancel()

			store := cluster.NewControlPlane(ctx)
			tokenizer, err := NewTokenizer(
				ctx,
				TokenizerWithStore(store),
				TokenizerWithBackend(tokenizerTestComputeBackend(ctx)),
				TokenizerWithBuffer(8),
			)
			So(err, ShouldBeNil)
			defer tokenizer.Close()

			payload := bytes.Repeat([]byte("p"), core.Cfg.Value.Bytes)
			inputValue, err := primitive.NewValue(payload)
			So(err, ShouldBeNil)

			err = inputValue.InstallFirmware(core.FirmwareTypePrompt)
			So(err, ShouldBeNil)
			inputValue.SetWord(core.Cfg.Value.Region.ID.Start, 4040)
			inputValue.Link(7, 9)

			inputBytes, err := inputValue.Bytes()
			So(err, ShouldBeNil)
			So(len(inputBytes), ShouldEqual, core.Cfg.Value.Bytes)

			n, writeErr := tokenizer.Write(inputBytes)
			So(writeErr, ShouldBeNil)
			So(n, ShouldEqual, int(core.Cfg.Value.Bytes))

			out := make([]byte, core.Cfg.Value.Bytes)
			readN, readErr := tokenizer.Read(out)
			So(readErr, ShouldEqual, io.EOF)
			So(readN, ShouldEqual, core.Cfg.Value.Bytes)

			outputValue := primitive.BytesToValue(out)
			So(outputValue.IsPrompt(), ShouldBeTrue)
			So(outputValue.GetWord(core.Cfg.Value.Region.Registers.FW), ShouldEqual, uint64(core.FirmwareTypePrompt))
			So(outputValue.GetWord(core.Cfg.Value.Region.Prev.Start), ShouldEqual, 7)
			So(outputValue.GetWord(core.Cfg.Value.Region.Next.Start), ShouldEqual, 9)
			So(outputValue.GetWord(core.Cfg.Value.Region.ID.Start), ShouldEqual, 4040)
			So(outputValue.GetWord(core.Cfg.Value.Region.ID.Start), ShouldEqual, inputValue.GetWord(core.Cfg.Value.Region.ID.Start))
		})
	})
}

func TestTokenizerWriteSerializesIntoStoreByValueID(t *testing.T) {
	Convey("Write", t, func() {
		Convey("indexes serialized frames by ValueID in the control plane", func() {
			ctx, cancel := tokenizerTestContext()
			defer cancel()

			store := cluster.NewControlPlane(ctx)
			tokenizer, err := NewTokenizer(
				ctx,
				TokenizerWithStore(store),
				TokenizerWithBackend(tokenizerTestComputeBackend(ctx)),
				TokenizerWithBuffer(8),
			)
			So(err, ShouldBeNil)
			defer tokenizer.Close()

			payload := bytes.Repeat([]byte("z"), 1)
			inputValue, err := primitive.NewValue(payload)
			So(err, ShouldBeNil)

			inputBytes, err := inputValue.Bytes()
			So(err, ShouldBeNil)
			valueID := inputValue.GetWord(core.Cfg.Value.Region.ID.Start)

			_, writeErr := tokenizer.Write(inputBytes)
			So(writeErr, ShouldBeNil)

			storedFrame, ok := store.FrameByValueID(valueID)
			So(ok, ShouldBeTrue)
			So(storedFrame[core.Cfg.Value.Region.ID.Start], ShouldEqual, valueID)
		})
	})
}

func TestTokenizerClose(t *testing.T) {
	Convey("Close", t, func() {
		Convey("cancels tokenizer context", func() {
			ctx, cancel := tokenizerTestContext()
			defer cancel()

			store := cluster.NewControlPlane(ctx)
			tokenizer, err := NewTokenizer(
				ctx,
				TokenizerWithStore(store),
				TokenizerWithBackend(tokenizerTestComputeBackend(ctx)),
			)
			So(err, ShouldBeNil)
			So(tokenizer.Close(), ShouldNotBeNil)

			_, rerr := tokenizer.Read(make([]byte, 64))
			So(rerr, ShouldNotBeNil)
		})
	})
}

func TestTokenizerWithBuffer(t *testing.T) {
	Convey("TokenizerWithBuffer", t, func() {
		Convey("scales ring capacity", func() {
			ctx, cancel := tokenizerTestContext()
			defer cancel()

			store := cluster.NewControlPlane(ctx)
			tokenizer, err := NewTokenizer(
				ctx,
				TokenizerWithStore(store),
				TokenizerWithBackend(tokenizerTestComputeBackend(ctx)),
				TokenizerWithBuffer(32),
			)
			So(err, ShouldBeNil)
			So(tokenizer.Close(), ShouldNotBeNil)
		})
	})
}

func BenchmarkTokenizerWriteRead(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := cluster.NewControlPlane(ctx)
	tokenizer, err := NewTokenizer(
		ctx,
		TokenizerWithBuffer(256),
		TokenizerWithStore(store),
		TokenizerWithBackend(tokenizerTestComputeBackend(ctx)),
	)

	if err != nil {
		b.Fatal(err)
	}

	defer tokenizer.Close()

	payload := bytes.Repeat([]byte("b"), core.Cfg.Value.Bytes)
	out := make([]byte, 4096)
	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_, _ = tokenizer.Write(payload)
		_, _ = tokenizer.Read(out)
	}
}
