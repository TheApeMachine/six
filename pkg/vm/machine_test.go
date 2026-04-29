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
		contextStart, _ := primitive.ContextRegion.WordExtent()

		nearCommunity := primitive.Emit()
		nearCommunity.SetProperty(primitive.COMMUNITY, nearCommunity.ID())
		nearCommunity.SetProperty(primitive.TARGET, nearCommunity.ID())
		nearCommunity.SetProperty(primitive.SURPRISAL, 777)
		nearCommunity.Set(contextStart, 1<<20)
		nearCommunity.Set(affinityStart, 0x03)
		nearCommunity.NormalizeAffinity()
		machine.backend.Submit(nearCommunity)

		nearMember := primitive.Emit(
			primitive.WithCommunity(nearCommunity.ID()),
			primitive.WithLabels(1),
		)
		machine.backend.Submit(nearMember)

		farCommunity := primitive.Emit()
		farCommunity.SetProperty(primitive.COMMUNITY, farCommunity.ID())
		farCommunity.SetProperty(primitive.TARGET, farCommunity.ID())
		farCommunity.SetProperty(primitive.SURPRISAL, 1)
		farCommunity.Set(contextStart, 0x01)
		farCommunity.Set(affinityStart, 1<<20)
		farCommunity.NormalizeAffinity()
		machine.backend.Submit(farCommunity)

		farMember := primitive.Emit(
			primitive.WithCommunity(farCommunity.ID()),
			primitive.WithLabels(2),
		)
		machine.backend.Submit(farMember)

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
				nearSurprisal, nearSurprisalErr := nearCommunity.Property(primitive.SURPRISAL)
				farSurprisal, farSurprisalErr := farCommunity.Property(primitive.SURPRISAL)

				So(communityErr, ShouldBeNil)
				So(labelErr, ShouldBeNil)
				So(nearSurprisalErr, ShouldBeNil)
				So(farSurprisalErr, ShouldBeNil)
				So(community, ShouldEqual, nearCommunity.ID())
				So(label, ShouldEqual, 1)
				So(nearSurprisal, ShouldEqual, 777)
				So(farSurprisal, ShouldEqual, 1)
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
		contextStart, _ := primitive.ContextRegion.WordExtent()

		community := primitive.Emit()
		community.SetProperty(primitive.COMMUNITY, community.ID())
		community.SetProperty(primitive.TARGET, community.ID())
		community.Set(contextStart, 0x0f)
		community.Set(affinityStart, 0x0f)
		community.NormalizeAffinity()
		machine.backend.Submit(community)

		member := primitive.Emit(
			primitive.WithCommunity(community.ID()),
			primitive.WithLabels(2),
		)
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

				label, labelErr := values[0].Property(primitive.LABELS)
				So(labelErr, ShouldBeNil)
				So(label, ShouldEqual, 2)
			})
		})
	})

	Convey("Given a machine with a stale resolved resident before classification", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		machine, err := NewMachine(ctx)
		So(err, ShouldBeNil)
		So(machine, ShouldNotBeNil)
		Reset(func() {
			machine.Close()
		})

		affinityStart, _ := primitive.AffinityRegion.WordExtent()
		contextStart, _ := primitive.ContextRegion.WordExtent()

		community := primitive.Emit()
		community.SetProperty(primitive.COMMUNITY, community.ID())
		community.SetProperty(primitive.TARGET, community.ID())
		community.Set(contextStart, 0x0f)
		community.Set(affinityStart, 0x0f)
		community.NormalizeAffinity()
		machine.backend.Submit(community)

		member := primitive.Emit(
			primitive.WithCommunity(community.ID()),
			primitive.WithLabels(2),
		)
		machine.backend.Submit(member)

		stale := primitive.Emit(
			primitive.WithStatus(uint64(primitive.RESOLVED)),
			primitive.WithLabels(99),
		)
		machine.backend.Submit(stale)

		prompt := primitive.Emit(primitive.WithFirmware(core.CLASSIFY_READOUT))
		prompt.Set(affinityStart, 0x0f)
		prompt.NormalizeAffinity()

		Convey("When Prompt runs readout", func() {
			resolved, err := machine.Prompt(prompt)
			So(err, ShouldBeNil)

			Convey("Then only the injected prompt resolves the task", func() {
				So(len(resolved), ShouldEqual, 1)
				So(resolved[0].ID(), ShouldEqual, prompt.ID())

				label, labelErr := prompt.Property(primitive.LABELS)
				So(labelErr, ShouldBeNil)
				So(label, ShouldEqual, 2)
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
	contextStart, _ := primitive.ContextRegion.WordExtent()

	community := primitive.Emit()
	community.SetProperty(primitive.COMMUNITY, community.ID())
	community.SetProperty(primitive.TARGET, community.ID())
	community.Set(contextStart, 1)
	community.Set(affinityStart, 1)
	community.NormalizeAffinity()
	machine.backend.Submit(community)

	for idx := 0; idx < 8; idx++ {
		member := primitive.Emit(
			primitive.WithCommunity(community.ID()),
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

/*
loadProvider is a test-only provider implementing Generate, Read, and Close
so Load can exercise both the iter.Seq and io.Reader-facing scaffolding.
*/
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
