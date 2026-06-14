package vm

import (
	"context"
	"io"
	"iter"
	"math"
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

	Convey("Given a machine seeded with two affinity community roots", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		machine, err := NewMachine(ctx)
		So(err, ShouldBeNil)
		So(machine, ShouldNotBeNil)
		Reset(func() {
			machine.Close()
		})

		nearPrototype := primitive.Emit(primitive.WithLabels(1))
		nearPrototype.SetProperty(primitive.COMMUNITY, nearPrototype.ID())
		nearPrototype.SetProperty(primitive.TARGET, nearPrototype.ID())
		nearPrototype.SetProperty(primitive.ROLE, uint64(primitive.ValueRoleReadout))
		nearPrototype.SetProperty(primitive.SURPRISAL, 777)
		storeContextLanes(nearPrototype, [8]float64{1, 0, 0, 0, 0, 0, 0, 0})
		storeAffinityWords(nearPrototype, 0x01)
		machine.backend.Submit(nearPrototype)

		farPrototype := primitive.Emit(primitive.WithLabels(2))
		farPrototype.SetProperty(primitive.COMMUNITY, farPrototype.ID())
		farPrototype.SetProperty(primitive.TARGET, farPrototype.ID())
		farPrototype.SetProperty(primitive.ROLE, uint64(primitive.ValueRoleReadout))
		farPrototype.SetProperty(primitive.SURPRISAL, 1)
		storeContextLanes(farPrototype, [8]float64{0, 1, 0, 0, 0, 0, 0, 0})
		storeAffinityWords(farPrototype, 1<<20)
		machine.backend.Submit(farPrototype)

		prompt := primitive.Emit(primitive.WithFirmware(core.CLASSIFY_READOUT))
		storeContextLanes(prompt, [8]float64{1, 0, 0, 0, 0, 0, 0, 0})
		storeAffinityWords(prompt, 0x01)
		prompt.SetProperty(primitive.SURPRISAL, 512)
		machine.backend.Submit(prompt)

		Convey("When the prompt runs classify_readout over the seeded lane", func() {
			resolved, err := machine.Prompt(prompt)
			So(err, ShouldBeNil)
			So(len(resolved), ShouldBeGreaterThan, 0)

			Convey("Then the prompt settles RESOLVED with the matching community label", func() {
				So(prompt.Status(), ShouldEqual, primitive.RESOLVED)

				community, communityErr := prompt.Property(primitive.COMMUNITY)
				label, labelErr := prompt.Property(primitive.LABELS)
				nearSurprisal, nearSurprisalErr := nearPrototype.Property(primitive.SURPRISAL)
				farSurprisal, farSurprisalErr := farPrototype.Property(primitive.SURPRISAL)

				So(communityErr, ShouldBeNil)
				So(labelErr, ShouldBeNil)
				So(nearSurprisalErr, ShouldBeNil)
				So(farSurprisalErr, ShouldBeNil)
				So(community, ShouldEqual, nearPrototype.ID())
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

		prototype := primitive.Emit(primitive.WithLabels(2))
		prototype.SetProperty(primitive.COMMUNITY, prototype.ID())
		prototype.SetProperty(primitive.TARGET, prototype.ID())
		prototype.SetProperty(primitive.ROLE, uint64(primitive.ValueRoleReadout))
		storeContextLanes(prototype, [8]float64{1, 0, 0, 0, 0, 0, 0, 0})
		storeAffinityWords(prototype, 0x0f)
		machine.backend.Submit(prototype)

		values, err := primitive.NewValue([]byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 abcdefghijklmnopqrstuvwxyz"))
		So(err, ShouldBeNil)
		So(len(values), ShouldBeGreaterThan, 1)

		for _, value := range values {
			ok, firmwareErr := value.InstallFirmware(core.CLASSIFY_READOUT)
			So(firmwareErr, ShouldBeNil)
			So(ok, ShouldBeTrue)
			storeContextLanes(value, [8]float64{1, 0, 0, 0, 0, 0, 0, 0})
			storeAffinityWords(value, 0x0f)
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

		prototype := primitive.Emit(primitive.WithLabels(2))
		prototype.SetProperty(primitive.COMMUNITY, prototype.ID())
		prototype.SetProperty(primitive.TARGET, prototype.ID())
		prototype.SetProperty(primitive.ROLE, uint64(primitive.ValueRoleReadout))
		storeContextLanes(prototype, [8]float64{1, 0, 0, 0, 0, 0, 0, 0})
		storeAffinityWords(prototype, 0x0f)
		machine.backend.Submit(prototype)

		stale := primitive.Emit(
			primitive.WithStatus(uint64(primitive.RESOLVED)),
			primitive.WithLabels(99),
		)
		machine.backend.Submit(stale)

		prompt := primitive.Emit(primitive.WithFirmware(core.CLASSIFY_READOUT))
		storeContextLanes(prompt, [8]float64{1, 0, 0, 0, 0, 0, 0, 0})
		storeAffinityWords(prompt, 0x0f)
		prompt.SetProperty(primitive.SURPRISAL, 512)

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

func TestSeedStructuralAssociations(t *testing.T) {
	loadConfigForTests(t)

	Convey("Given a machine seeded with structural story Values", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		machine, err := NewMachine(ctx)
		So(err, ShouldBeNil)
		So(machine, ShouldNotBeNil)
		Reset(func() {
			machine.Close()
		})

		story := []*primitive.Value{
			primitive.Emit(),
			primitive.Emit(),
			primitive.Emit(),
		}
		story[0].Set(0, 0x111100ff)
		story[1].Set(0, 0x222200ff)
		story[2].Set(0, 0x333300ff)

		for _, value := range story {
			So(machine.backend.Submit(value), ShouldBeNil)
		}
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

	contextStart, _ := primitive.ContextRegion.WordExtent()

	community := primitive.Emit()
	community.SetProperty(primitive.COMMUNITY, community.ID())
	community.SetProperty(primitive.TARGET, community.ID())
	community.SetProperty(primitive.ROLE, uint64(primitive.ValueRoleReadout))
	community.Set(contextStart, 1)
	storeAffinityWords(community, 1)
	machine.backend.Submit(community)

	for idx := 0; idx < 8; idx++ {
		member := primitive.Emit(
			primitive.WithCommunity(community.ID()),
			primitive.WithLabels(1),
		)
		storeAffinityWords(member, uint64(idx+1))
		machine.backend.Submit(member)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		prompt := primitive.Emit(primitive.WithFirmware(core.CLASSIFY_READOUT))
		storeAffinityWords(prompt, 1)
		prompt.SetProperty(primitive.SURPRISAL, 512)
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

func storeAffinityWords(value *primitive.Value, words ...uint64) {
	affinityStart, affinityWords := primitive.AffinityRegion.WordExtent()

	for idx := 0; idx < int(affinityWords); idx++ {
		word := uint64(0)
		if idx < len(words) {
			word = words[idx]
		}

		value.Set(affinityStart+idx, word)
	}

	value.NormalizeAffinity()
}

func hasTokenWords(value *primitive.Value) bool {
	for _, word := range value.Get(primitive.TokenRegion) {
		if word != 0 {
			return true
		}
	}

	return false
}

func hasContextWords(value *primitive.Value) bool {
	for _, word := range value.Get(primitive.ContextRegion) {
		if word != 0 {
			return true
		}
	}

	return false
}

func storeContextLanes(value *primitive.Value, lanes [8]float64) {
	contextStart, contextWords := primitive.ContextRegion.WordExtent()

	for lane := 0; lane < int(contextWords) && lane < len(lanes); lane++ {
		value.Set(contextStart+lane, math.Float64bits(lanes[lane]))
	}
}
