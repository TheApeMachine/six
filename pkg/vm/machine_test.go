package vm

import (
	"bytes"
	"context"
	"io"
	"iter"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
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

		chunkWords := int(core.Cfg.Value.Region.Tokens.Bits / 8)
		payload := bytes.Repeat([]byte{'m'}, chunkWords*3)
		provider := newBytesProvider(payload)

		So(machine.Load(provider), ShouldBeNil)
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

		prediction, promptErr := machine.Prompt("ping")

		So(promptErr, ShouldBeNil)
		So(prediction, ShouldNotBeNil)
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

	chunkWords := int(core.Cfg.Value.Region.Tokens.Bits / 8)
	payload := bytes.Repeat([]byte{'z'}, chunkWords*16)
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
