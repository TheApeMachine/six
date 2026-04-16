package vm

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"iter"
	"sync"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
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

	Convey("Load should link sequential samples and compute affinity through the real runtime path", t, func() {
		ctx := context.Background()
		machine, err := NewMachine(ctx)

		So(err, ShouldBeNil)

		defer func() {
			So(machine.Close(), ShouldBeNil)
		}()

		provider := newStaticSampleProvider(func(yield func(data.Sample) bool) {
			if !yield(data.Sample{Text: []byte("first")}) {
				return
			}

			_ = yield(data.Sample{Text: []byte("second")})
		})

		So(machine.Load(provider), ShouldBeNil)

		var firstValue *primitive.Value
		var secondValue *primitive.Value

		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			for _, peer := range machine.orchestrator.router.route {
				community, ok := peer.Dst().(*geometry.Field)
				if !ok || community == nil {
					continue
				}

				for _, value := range community.Values {
					if value == nil {
						continue
					}

					switch value.String() {
					case "first":
						firstValue = value
					case "second":
						secondValue = value
					}
				}
			}

			if firstValue != nil && secondValue != nil {
				break
			}

			time.Sleep(10 * time.Millisecond)
		}

		So(firstValue, ShouldNotBeNil)
		So(secondValue, ShouldNotBeNil)
		So((*firstValue)[121], ShouldEqual, secondValue.ID())
		So((*secondValue)[120], ShouldEqual, firstValue.ID())
		So((*firstValue)[123] != 0 || (*firstValue)[124] != 0 || (*firstValue)[125] != 0 || (*firstValue)[126] != 0 || (*firstValue)[127] != 0, ShouldBeTrue)
		So((*secondValue)[123] != 0 || (*secondValue)[124] != 0 || (*secondValue)[125] != 0 || (*secondValue)[126] != 0 || (*secondValue)[127] != 0, ShouldBeTrue)
	})
}

func TestMachineLoadPublishesUpdatedWireFrameWithNonZeroAffinity(t *testing.T) {
	setupTokenizerValueConfig()

	Convey("Machine.Load emits a later raw wire frame whose affinity words are non-zero", t, func() {
		ctx := context.Background()
		machine, err := NewMachine(ctx)
		So(err, ShouldBeNil)
		defer func() {
			So(machine.Close(), ShouldBeNil)
		}()

		var framesMu sync.Mutex
		framesByID := map[uint64][][]byte{}
		telemetry.SetWireValueFrameSink(func(payload []byte) {
			ft, _, _, _, _, valueID, wire, err := telemetry.UnmarshalWireMessage(payload)
			if err != nil || ft != byte(telemetry.WireFrameValue) || valueID == 0 || len(wire) == 0 {
				return
			}

			copyWire := append([]byte(nil), wire...)
			framesMu.Lock()
			framesByID[valueID] = append(framesByID[valueID], copyWire)
			framesMu.Unlock()
		})
		defer telemetry.SetWireValueFrameSink(nil)

		provider := newStaticSampleProvider(func(yield func(data.Sample) bool) {
			if !yield(data.Sample{Text: []byte("first")}) {
				return
			}
			_ = yield(data.Sample{Text: []byte("second")})
		})

		So(machine.Load(provider), ShouldBeNil)

		var firstValue *primitive.Value
		var secondValue *primitive.Value
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			for _, peer := range machine.orchestrator.router.route {
				community, ok := peer.Dst().(*geometry.Field)
				if !ok || community == nil {
					continue
				}
				for _, value := range community.Values {
					if value == nil {
						continue
					}
					switch value.String() {
					case "first":
						firstValue = value
					case "second":
						secondValue = value
					}
				}
			}

			if firstValue != nil && secondValue != nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		So(firstValue, ShouldNotBeNil)
		So(secondValue, ShouldNotBeNil)

		deadline = time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			ids := []uint64{firstValue.ID(), secondValue.ID()}
			ready := 0
			framesMu.Lock()
			for _, valueID := range ids {
				frames := framesByID[valueID]
				if len(frames) < 2 {
					continue
				}
				last := frames[len(frames)-1]
				affinityNonZero := false
				for word := 123; word <= 127; word++ {
					off := word * 8
					if binary.LittleEndian.Uint64(last[off:off+8]) != 0 {
						affinityNonZero = true
						break
					}
				}
				if affinityNonZero {
					ready++
				}
			}
			framesMu.Unlock()
			if ready == len(ids) {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		framesMu.Lock()
		for _, value := range []*primitive.Value{firstValue, secondValue} {
			frames := framesByID[value.ID()]
			So(len(frames), ShouldBeGreaterThanOrEqualTo, 2)
			last := frames[len(frames)-1]
			affinityWords := make([]string, 0, 5)
			affinityNonZero := false
			for word := 123; word <= 127; word++ {
				off := word * 8
				got := binary.LittleEndian.Uint64(last[off : off+8])
				affinityWords = append(affinityWords, fmt.Sprintf("%016x", got))
				if got != 0 {
					affinityNonZero = true
				}
			}
			SoMsg(fmt.Sprintf("value %q (%x) final published frame affinity %v", value.String(), value.ID(), affinityWords), affinityNonZero, ShouldBeTrue)
		}
		framesMu.Unlock()
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
