package vm

import (
	"bytes"
	"context"
	"errors"
	"io"
	"iter"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
bytesProvider is a minimal data.Provider for tests: Read streams payload;
Generate yields a single Sample carrying the same bytes for Load.
*/
type bytesProvider struct {
	payload []byte
	offset  int
}

func newBytesProvider(payload []byte) *bytesProvider {
	return &bytesProvider{payload: payload}
}

func (provider *bytesProvider) Read(destination []byte) (n int, err error) {
	if provider.offset >= len(provider.payload) {
		return 0, io.EOF
	}

	n = copy(destination, provider.payload[provider.offset:])
	provider.offset += n

	return n, nil
}

func (provider *bytesProvider) Close() error {
	return nil
}

func (provider *bytesProvider) Reset() {
	if provider == nil {
		return
	}

	provider.offset = 0
}

func (provider *bytesProvider) Generate() iter.Seq[data.Sample] {
	return func(yield func(data.Sample) bool) {
		if len(provider.payload) == 0 {
			return
		}

		_ = yield(data.Sample{Text: provider.payload})
	}
}

/*
staticSampleProvider drives Load tests with explicit data.Sample sequences.
*/
type staticSampleProvider struct {
	seq     iter.Seq[data.Sample]
	readErr error
}

var _ data.Provider = (*staticSampleProvider)(nil)

func newStaticSampleProvider(seq iter.Seq[data.Sample]) *staticSampleProvider {
	return &staticSampleProvider{seq: seq}
}

func (provider *staticSampleProvider) Read(destination []byte) (n int, err error) {
	_ = destination

	if provider.readErr != nil {
		return 0, provider.readErr
	}

	return 0, io.EOF
}

func (provider *staticSampleProvider) Close() error {
	return nil
}

func (provider *staticSampleProvider) Generate() iter.Seq[data.Sample] {
	return provider.seq
}

func setupTokenizerValueConfig() {
	core.Cfg.Value.Region.Tokens.Start = 0
	core.Cfg.Value.Region.Tokens.Bits = 1024
}

func TestNewMachine(t *testing.T) {
	Convey("NewMachine wires host, queue, backend, and tokenizer", t, func() {
		ctx := context.Background()
		machine, err := NewMachine(ctx)

		So(err, ShouldBeNil)
		So(machine, ShouldNotBeNil)

		defer func() {
			So(machine.Close(), ShouldBeNil)
		}()

		So(machine.tokenizer, ShouldNotBeNil)
		So(machine.queue, ShouldNotBeNil)
		So(machine.backend, ShouldNotBeNil)
		So(machine.host, ShouldNotBeNil)
	})
}

func TestMachineClose(t *testing.T) {
	Convey("Close cancels and releases machine parts without panic", t, func() {
		ctx := context.Background()
		machine, err := NewMachine(ctx)

		So(err, ShouldBeNil)

		So(machine.Close(), ShouldBeNil)
	})
}

func TestMachineLoad(t *testing.T) {
	setupTokenizerValueConfig()

	Convey("Load ingests samples through tokenizer IngestSample", t, func() {
		ctx := context.Background()
		machine, err := NewMachine(ctx)

		So(err, ShouldBeNil)

		defer func() {
			So(machine.Close(), ShouldBeNil)
		}()

		chunkBytes := core.Cfg.Value.Region.MaxTokenIngestBytes()
		payload := bytes.Repeat([]byte{'m'}, chunkBytes*3)
		provider := newBytesProvider(payload)

		So(machine.Load(provider), ShouldBeNil)
	})

	Convey("Load ingests labeled samples", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		machine, err := NewMachine(ctx)

		So(err, ShouldBeNil)

		defer func() {
			So(machine.Close(), ShouldBeNil)
		}()

		provider := newStaticSampleProvider(func(yield func(data.Sample) bool) {
			_ = yield(data.Sample{
				Text:  []byte("orbital launch telemetry"),
				Label: []byte("space"),
			})
		})

		So(machine.Load(provider), ShouldBeNil)

		segments, segErr := primitive.NewValue([]byte("orbital launch telemetry"))
		So(segErr, ShouldBeNil)

		cancel()
		_, promptErr := machine.Prompt(segments[len(segments)-1])

		So(errors.Is(promptErr, context.Canceled), ShouldBeTrue)
	})

	Convey("Load accepts unlabeled samples", t, func() {
		ctx := context.Background()
		machine, err := NewMachine(ctx)

		So(err, ShouldBeNil)

		defer func() {
			So(machine.Close(), ShouldBeNil)
		}()

		provider := newStaticSampleProvider(func(yield func(data.Sample) bool) {
			_ = yield(data.Sample{
				Text: []byte("boundary-preserved prompt"),
			})
		})

		So(machine.Load(provider), ShouldBeNil)
	})
}

func TestMachineLoadPrompts(t *testing.T) {
	setupTokenizerValueConfig()

	Convey("Load ingests multiple samples in order", t, func() {
		ctx := context.Background()
		machine, err := NewMachine(ctx)

		So(err, ShouldBeNil)

		defer func() {
			So(machine.Close(), ShouldBeNil)
		}()

		chunkBytes := core.Cfg.Value.Region.MaxTokenIngestBytes()
		textA := string(bytes.Repeat([]byte{'a'}, chunkBytes*2))
		textB := string(bytes.Repeat([]byte{'b'}, chunkBytes))

		provider := newStaticSampleProvider(func(yield func(data.Sample) bool) {
			if !yield(data.Sample{
				Text: []byte(textA), Label: []byte("L1"),
			}) {
				return
			}

			_ = yield(data.Sample{
				Text: []byte(textB), Label: []byte("L2"),
			})
		})

		So(machine.Load(provider), ShouldBeNil)
	})
}

func TestMachinePrompt(t *testing.T) {
	setupTokenizerValueConfig()

	Convey("Prompt loops on Cycle until gap closure or context end", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		machine, err := NewMachine(ctx)

		So(err, ShouldBeNil)

		defer func() {
			So(machine.Close(), ShouldBeNil)
		}()

		segments, segErr := primitive.NewValue([]byte("prompt"))
		So(segErr, ShouldBeNil)

		cancel()

		_, promptErr := machine.Prompt(segments[len(segments)-1])

		So(errors.Is(promptErr, context.Canceled), ShouldBeTrue)
	})
}

func TestMachineError(t *testing.T) {
	Convey("Error is nil after successful NewMachine", t, func() {
		ctx := context.Background()
		machine, err := NewMachine(ctx)

		So(err, ShouldBeNil)

		defer func() {
			So(machine.Close(), ShouldBeNil)
		}()

		So(machine.Error(), ShouldBeNil)
	})
}

func BenchmarkMachine_Load(b *testing.B) {
	setupTokenizerValueConfig()

	ctx := context.Background()
	machine, err := NewMachine(ctx)

	if err != nil {
		b.Fatal(err)
	}

	defer machine.Close()

	chunkBytes := core.Cfg.Value.Region.MaxTokenIngestBytes()
	payload := bytes.Repeat([]byte{'z'}, chunkBytes*16)
	provider := newBytesProvider(payload)

	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		provider.Reset()

		if loadErr := machine.Load(provider); loadErr != nil {
			b.Fatal(loadErr)
		}
	}
}
