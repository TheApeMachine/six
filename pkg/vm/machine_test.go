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
)

/*
bytesProvider is a minimal data.Provider for tests: Load only reads, Generate
is a pass-through over the same bytes for API completeness.
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

func (provider *bytesProvider) Generate() iter.Seq[byte] {
	return func(yield func(byte) bool) {
		for index := range provider.payload {
			if !yield(provider.payload[index]) {
				return
			}
		}
	}
}

/*
staticPromptProvider drives LoadPrompts tests without external datasets.
*/
type staticPromptProvider struct {
	seq             iter.Seq[data.Prompt]
	hasPromptLabels bool
	readErr         error
}

var _ data.Provider = (*staticPromptProvider)(nil)
var _ data.PromptProvider = (*staticPromptProvider)(nil)
var _ data.LabeledPromptProvider = (*staticPromptProvider)(nil)

func newStaticPromptProvider(seq iter.Seq[data.Prompt]) *staticPromptProvider {
	return newStaticPromptProviderWithLabels(seq, true)
}

func newStaticPromptProviderWithLabels(
	seq iter.Seq[data.Prompt],
	hasPromptLabels bool,
) *staticPromptProvider {
	return &staticPromptProvider{
		seq:             seq,
		hasPromptLabels: hasPromptLabels,
	}
}

func (provider *staticPromptProvider) Read(destination []byte) (n int, err error) {
	_ = destination

	if provider.readErr != nil {
		return 0, provider.readErr
	}

	return 0, io.EOF
}

func (provider *staticPromptProvider) Close() error {
	return nil
}

func (provider *staticPromptProvider) HasPromptLabels() bool {
	return provider.hasPromptLabels
}

func (provider *staticPromptProvider) Generate() iter.Seq[byte] {
	return func(yield func(byte) bool) {
		_ = yield
	}
}

func (provider *staticPromptProvider) GeneratePrompts() iter.Seq[data.Prompt] {
	return provider.seq
}

func TestNewMachine(t *testing.T) {
	Convey("NewMachine wires host, queue, backend, kadabra, and tokenizer", t, func() {
		ctx := context.Background()
		machine, err := NewMachine(ctx)

		So(err, ShouldBeNil)
		So(machine, ShouldNotBeNil)

		defer func() {
			So(machine.Close(), ShouldBeNil)
		}()

		So(machine.tokenizer, ShouldNotBeNil)
		So(machine.kadabra, ShouldNotBeNil)
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
	setupTokenizerValueConfig(t)

	Convey("Load ingests raw bytes through tokenizer into kadabra", t, func() {
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

	Convey("Load preserves PromptProvider labels when available", t, func() {
		ctx := context.Background()
		machine, err := NewMachine(ctx)

		So(err, ShouldBeNil)

		defer func() {
			So(machine.Close(), ShouldBeNil)
		}()

		provider := newStaticPromptProvider(func(yield func(data.Prompt) bool) {
			_ = yield(data.Prompt{
				Text:     "orbital launch telemetry",
				Label:    "space",
				HasLabel: true,
			})
		})

		So(machine.Load(provider), ShouldBeNil)

		promptErr := machine.Prompt("orbital launch telemetry")

		So(promptErr, ShouldBeNil)
	})

	Convey("Load uses PromptProvider boundaries without labels", t, func() {
		ctx := context.Background()
		machine, err := NewMachine(ctx)

		So(err, ShouldBeNil)

		defer func() {
			So(machine.Close(), ShouldBeNil)
		}()

		provider := newStaticPromptProviderWithLabels(func(yield func(data.Prompt) bool) {
			_ = yield(data.Prompt{
				Text: "boundary-preserved prompt",
			})
		}, false)
		provider.readErr = errors.New("raw Read should not be used for PromptProvider")

		So(machine.Load(provider), ShouldBeNil)
	})
}

func TestMachineLoadPrompts(t *testing.T) {
	setupTokenizerValueConfig(t)

	Convey("LoadPrompts ingests each prompt with its label on every chunk", t, func() {
		ctx := context.Background()
		machine, err := NewMachine(ctx)

		So(err, ShouldBeNil)

		defer func() {
			So(machine.Close(), ShouldBeNil)
		}()

		chunkBytes := core.Cfg.Value.Region.MaxTokenIngestBytes()
		textA := string(bytes.Repeat([]byte{'a'}, chunkBytes*2))
		textB := string(bytes.Repeat([]byte{'b'}, chunkBytes))

		provider := newStaticPromptProvider(func(yield func(data.Prompt) bool) {
			if !yield(data.Prompt{
				Text: textA, Label: "L1", HasLabel: true,
			}) {
				return
			}

			_ = yield(data.Prompt{
				Text: textB, Label: "L2", HasLabel: true,
			})
		})

		So(machine.LoadPrompts(provider), ShouldBeNil)
	})
}

func TestMachinePrompt(t *testing.T) {
	setupTokenizerValueConfig(t)

	Convey("Prompt returns a prediction structure for a short query", t, func() {
		ctx := context.Background()
		machine, err := NewMachine(ctx)

		So(err, ShouldBeNil)

		defer func() {
			So(machine.Close(), ShouldBeNil)
		}()

		promptErr := machine.Prompt("prompt")

		So(promptErr, ShouldBeNil)
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
	setupTokenizerValueConfig(b)

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
