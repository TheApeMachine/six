package vm

import (
	"context"
	"io"
	"iter"
	"os"
	"path/filepath"
	"sync"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

var configOnce sync.Once

func loadConfigForTests(t testing.TB) {
	t.Helper()

	configOnce.Do(func() {
		viper.SetConfigType("yml")
		viper.Set("telemetry.enabled", false)
		viper.Set("telemetry.ws_url", "")

		candidates := []string{
			filepath.Join("..", "..", "cmd", "cfg", "config.yml"),
			"cmd/cfg/config.yml",
		}

		for _, path := range candidates {
			if _, err := os.Stat(path); err != nil {
				continue
			}

			viper.SetConfigFile(path)
			if err := viper.ReadInConfig(); err == nil {
				core.NewConfig()
				return
			}
		}

		t.Fatalf("no config.yml found in candidates")
	})
}

func TestNewMachine(t *testing.T) {
	loadConfigForTests(t)

	Convey("Given telemetry is disabled with a configured URL", t, func() {
		enabled := core.Cfg.TelemetryEnabled
		url := core.Cfg.TelemetryWebSocketURL

		core.Cfg.TelemetryEnabled = false
		core.Cfg.TelemetryWebSocketURL = "ws://127.0.0.1:1/ws"

		Reset(func() {
			core.Cfg.TelemetryEnabled = enabled
			core.Cfg.TelemetryWebSocketURL = url
		})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		machine, err := NewMachine(ctx)
		So(err, ShouldBeNil)
		So(machine, ShouldNotBeNil)
		Reset(func() {
			machine.Close()
		})

		Convey("It should keep the bridge inert", func() {
			So(machine.telemetry.Enabled(), ShouldBeFalse)
		})
	})
}

func TestPrompt(t *testing.T) {
	loadConfigForTests(t)

	Convey("Given a machine seeded with two labelled communities", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		machine, err := NewMachine(ctx)
		So(err, ShouldBeNil)
		So(machine, ShouldNotBeNil)
		Reset(func() {
			machine.Close()
		})

		affinityStart, _ := primitive.AffinityRegion.WordExtent()

		nearSecond := primitive.Emit(
			primitive.WithCommunity(10),
			primitive.WithLabels(1),
		)
		nearSecond.Set(affinityStart, 0x03)
		nearSecond.NormalizeAffinity()
		machine.backend.Submit(nearSecond)

		nearFirst := primitive.Emit(
			primitive.WithCommunity(20),
			primitive.WithLabels(2),
		)
		nearFirst.Set(affinityStart, 1<<20)
		nearFirst.NormalizeAffinity()
		machine.backend.Submit(nearFirst)

		prompt := primitive.Emit(primitive.WithFirmware(core.CLASSIFY_READOUT))
		prompt.Set(affinityStart, 0x01)
		prompt.NormalizeAffinity()
		machine.backend.Submit(prompt)

		Convey("When the prompt runs classify_readout over the seeded lane", func() {
			resolved, err := machine.Prompt(prompt)
			So(err, ShouldBeNil)
			So(len(resolved), ShouldBeGreaterThan, 0)

			Convey("Then the prompt itself settles RESOLVED with the nearest community and modal label", func() {
				So(prompt.Status(), ShouldEqual, primitive.RESOLVED)

				community, communityErr := prompt.Property(primitive.COMMUNITY)
				label, labelErr := prompt.Property(primitive.LABELS)

				So(communityErr, ShouldBeNil)
				So(labelErr, ShouldBeNil)
				So(community, ShouldEqual, 10)
				So(label, ShouldEqual, 1)
			})
		})
	})

	Convey("Given a machine with a multi-segment classification prompt", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		machine, err := NewMachine(ctx)
		So(err, ShouldBeNil)
		So(machine, ShouldNotBeNil)
		Reset(func() {
			machine.Close()
		})

		affinityStart, _ := primitive.AffinityRegion.WordExtent()

		member := primitive.Emit(
			primitive.WithCommunity(10),
			primitive.WithLabels(2),
		)
		member.Set(affinityStart, 0x0f)
		member.NormalizeAffinity()
		machine.backend.Submit(member)

		values, err := primitive.NewValue([]byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 abcdefghijklmnopqrstuvwxyz"))
		So(err, ShouldBeNil)
		So(len(values), ShouldBeGreaterThan, 1)

		for _, value := range values {
			ok, firmwareErr := value.InstallFirmware(core.CLASSIFY_READOUT)
			So(firmwareErr, ShouldBeNil)
			So(ok, ShouldBeTrue)
			value.Set(affinityStart, 0x0f)
			value.NormalizeAffinity()
			machine.backend.Submit(value)
		}

		Convey("When Prompt runs the linked readout chain", func() {
			resolved, err := machine.Prompt(values...)
			So(err, ShouldBeNil)
			// Sync yields RESOLVED only, so just the head shows up; the
			// tails retire DONE in-frame and are observable via Status().
			So(len(resolved), ShouldBeGreaterThan, 0)

			Convey("Then the head resolves and the tails retire", func() {
				So(values[0].Status(), ShouldEqual, primitive.RESOLVED)

				for idx := 1; idx < len(values); idx++ {
					So(values[idx].Status(), ShouldEqual, primitive.DONE)
				}
			})
		})
	})
}

func BenchmarkPrompt(b *testing.B) {
	loadConfigForTests(b)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	machine, err := NewMachine(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer machine.Close()

	affinityStart, _ := primitive.AffinityRegion.WordExtent()

	for idx := 0; idx < 8; idx++ {
		member := primitive.Emit(
			primitive.WithCommunity(10),
			primitive.WithLabels(1),
		)
		member.Set(affinityStart, uint64(idx+1))
		member.NormalizeAffinity()
		machine.backend.Submit(member)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		prompt := primitive.Emit(primitive.WithFirmware(core.CLASSIFY_READOUT))
		prompt.Set(affinityStart, 1)
		prompt.NormalizeAffinity()
		machine.backend.Submit(prompt)

		resolved, err := machine.Prompt(prompt)
		if err != nil {
			b.Fatal(err)
		}

		if len(resolved) == 0 {
			b.Fatal("prompt produced no resolved values")
		}
	}
}

func TestLoad(t *testing.T) {
	loadConfigForTests(t)

	Convey("Given a machine loading unassigned token Values", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		machine, err := NewMachine(ctx)
		So(err, ShouldBeNil)
		So(machine, ShouldNotBeNil)
		Reset(func() {
			machine.Close()
		})

		dataset := &loadProvider{
			samples: []data.Sample{
				{SampleID: 1, Text: []byte("alpha beta gamma delta"), LabelInt: 1},
				{SampleID: 2, Text: []byte("alpha beta gamma epsilon"), LabelInt: 1},
			},
		}

		Convey("When Load drains query and recruitment firmware", func() {
			err := machine.Load(dataset)
			So(err, ShouldBeNil)

			Convey("Then at least one resident token is stamped into a community", func() {
				stamped := 0

				machine.backend.Range(func(resident *primitive.Value) bool {
					community, communityErr := resident.Property(primitive.COMMUNITY)

					if communityErr == nil && community != 0 && hasTokenWords(resident) {
						stamped++
					}

					return true
				})

				So(stamped, ShouldBeGreaterThan, 0)
			})
		})
	})
}

type loadProvider struct {
	samples []data.Sample
}

func (provider *loadProvider) Generate() iter.Seq[data.Sample] {
	return func(yield func(data.Sample) bool) {
		for _, sample := range provider.samples {
			if !yield(sample) {
				return
			}
		}
	}
}

func (provider *loadProvider) Read(p []byte) (int, error) {
	return 0, io.EOF
}

func (provider *loadProvider) Close() error {
	return nil
}

func emitWithAffinityRange(startBit, count int) *primitive.Value {
	value := primitive.Emit()
	affinityStart, _ := primitive.AffinityRegion.WordExtent()

	for bit := startBit; bit < startBit+count && bit < 257; bit++ {
		word := affinityStart + (bit / 64)
		mask := uint64(1) << uint64(bit%64)
		value.Set(word, value.Get(primitive.AffinityRegion)[word-affinityStart]|mask)
	}

	value.NormalizeAffinity()

	return value
}

func hasTokenWords(value *primitive.Value) bool {
	for _, word := range value.Get(primitive.TokenRegion) {
		if word != 0 {
			return true
		}
	}

	return false
}
