package vm

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
	"github.com/theapemachine/six/experiment/data/local"
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

func TestCycle(t *testing.T) {
	loadConfigForTests(t)

	Convey("Given a machine seeded with a query and recruiter over a small community", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		machine, err := NewMachine(ctx)
		So(err, ShouldBeNil)
		So(machine, ShouldNotBeNil)
		Reset(func() {
			machine.Close()
		})

		members := make([]*primitive.Value, 0, 4)
		for idx := 0; idx < 4; idx++ {
			value := primitive.Emit()
			affinityStart, _ := primitive.AffinityRegion.WordExtent()
			value.Set(affinityStart, uint64(1)<<uint64(idx))
			value.NormalizeAffinity()

			members = append(members, value)
			machine.community.Store(value.ID(), value)
		}

		recruiter := primitive.Emit(
			primitive.WithFirmware(core.RECRUIT_COMMUNITY),
		)

		query := primitive.Emit(
			primitive.WithFirmware(core.QUERY),
			primitive.WithReference(recruiter.ID()),
		)

		machine.community.Store(recruiter.ID(), recruiter)
		machine.community.Store(query.ID(), query)

		for _, value := range members {
			machine.backend.StageInto(query.ID(), value)
		}

		Convey("When Cycle runs", func() {
			queryWords := append([]uint64(nil), query.Get(primitive.ProgramRegion)...)

			err := machine.Cycle()
			So(err, ShouldBeNil)

			Convey("Then the query program had its expected instruction shape", func() {
				words := queryWords
				// Seed (topo=PopQueue), B-side orphan predicate, write
				// reference, stage(B) with popEnd bit, then retire query.
				So(words[0]>>55&3, ShouldEqual, 1)
				So(words[1]>>57&1, ShouldEqual, 1) // predicate bit
				So(words[1]>>61&1, ShouldEqual, 1) // B-side predicate source
				So(words[3]>>62&1, ShouldEqual, 1) // stage bit
				So(words[3]>>63&1, ShouldEqual, 1) // pop end
			})

			Convey("Then the query stamps every staged member with the recruiter's id", func() {
				for _, value := range members {
					ref, refErr := value.Property(primitive.REFERENCE)
					So(refErr, ShouldBeNil)
					So(ref, ShouldEqual, recruiter.ID())
				}
			})

			Convey("Then the recruiter's lane is drained after consumption", func() {
				lane := machine.backend.Lane(recruiter)
				So(len(lane), ShouldEqual, 0)
			})

			Convey("Then the recruiter's affinity union covers the seeded bits", func() {
				affinityStart, _ := primitive.AffinityRegion.WordExtent()
				word := recruiter.Get(primitive.AffinityRegion)[0]

				var expected uint64
				for idx := range members {
					expected |= uint64(1) << uint64(idx)
				}

				_ = affinityStart
				So(word&expected, ShouldEqual, expected)
			})

			Convey("Then the query and recruiter retired in-band", func() {
				So(query.Status(), ShouldEqual, primitive.DONE)
				So(query.SchedulingNext(), ShouldEqual, uint64(0))
				So(recruiter.Status(), ShouldEqual, primitive.DONE)
				So(recruiter.SchedulingNext(), ShouldEqual, uint64(0))
			})

			Convey("Then every gossiped peer is stamped with the recruiter's id as its community", func() {
				for _, value := range members {
					community, communityErr := value.Property(primitive.COMMUNITY)
					So(communityErr, ShouldBeNil)
					So(community, ShouldEqual, recruiter.ID())
				}
			})

			Convey("And the recruiter id is non-zero (sanity check)", func() {
				So(recruiter.ID(), ShouldNotEqual, uint64(0))
			})

			Convey("And the recruiter id differs from member ids (sanity check)", func() {
				for _, value := range members {
					So(value.ID(), ShouldNotEqual, recruiter.ID())
				}
			})

			Convey("And reading the raw COMMUNITY word at offset 64 confirms the stamp", func() {
				for _, value := range members {
					raw := value.Get(primitive.PropertiesRegion)[primitive.COMMUNITY]
					So(raw, ShouldEqual, recruiter.ID())
				}
			})
		})
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

func TestPromptClassifyReadout(t *testing.T) {
	loadConfigForTests(t)

	Convey("Given a machine with labelled categorical communities", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		machine, err := NewMachine(ctx)
		So(err, ShouldBeNil)
		So(machine, ShouldNotBeNil)
		Reset(func() {
			machine.Close()
		})

		affinityStart, _ := primitive.AffinityRegion.WordExtent()

		for _, spec := range []struct {
			community uint64
			label     uint64
			affinity  uint64
		}{
			{community: 10, label: 1, affinity: 1 << 24},
			{community: 10, label: 1, affinity: 1 << 25},
			{community: 10, label: 2, affinity: 1 << 26},
			{community: 20, label: 2, affinity: 0x0f},
			{community: 20, label: 2, affinity: 0x0e},
			{community: 20, label: 3, affinity: 0x07},
		} {
			member := primitive.Emit(
				primitive.WithCommunity(spec.community),
				primitive.WithLabels(spec.label),
			)
			member.Set(affinityStart, spec.affinity)
			member.NormalizeAffinity()
			machine.community.Store(member.ID(), member)
		}

		prompt := primitive.Emit(primitive.WithFirmware(core.CLASSIFY_READOUT))
		prompt.Set(affinityStart, 0x0f)
		prompt.NormalizeAffinity()

		Convey("When the prompt runs resident firmware over the staged lane", func() {
			resolved, err := machine.Prompt(prompt)
			So(err, ShouldBeNil)
			So(len(resolved), ShouldBeGreaterThan, 0)

			Convey("Then generic reducers select the nearest community and modal value", func() {
				community, communityErr := prompt.Property(primitive.COMMUNITY)
				label, labelErr := prompt.Property(primitive.LABELS)

				So(communityErr, ShouldBeNil)
				So(labelErr, ShouldBeNil)
				So(community, ShouldEqual, 20)
				So(label, ShouldEqual, 2)
				So(prompt.Status(), ShouldEqual, primitive.RESOLVED)
			})
		})
	})
}

func TestPrompt(t *testing.T) {
	loadConfigForTests(t)

	Convey("Given a machine that already produced a prompt readout", t, func() {
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
		machine.community.Store(nearSecond.ID(), nearSecond)

		nearFirst := primitive.Emit(
			primitive.WithCommunity(20),
			primitive.WithLabels(2),
		)
		nearFirst.Set(affinityStart, 1<<20)
		nearFirst.NormalizeAffinity()
		machine.community.Store(nearFirst.ID(), nearFirst)

		firstPrompt := primitive.Emit(primitive.WithFirmware(core.CLASSIFY_READOUT))
		firstPrompt.Set(affinityStart, 1<<20)
		firstPrompt.NormalizeAffinity()

		_, err = machine.Prompt(firstPrompt)
		So(err, ShouldBeNil)

		secondPrompt := primitive.Emit(primitive.WithFirmware(core.CLASSIFY_READOUT))
		secondPrompt.Set(affinityStart, 0x01)
		secondPrompt.NormalizeAffinity()

		Convey("When another prompt runs over the same machine", func() {
			resolved, err := machine.Prompt(secondPrompt)
			So(err, ShouldBeNil)
			So(len(resolved), ShouldBeGreaterThan, 0)

			Convey("Then only the new prompt result is reported and prior prompt values are not candidates", func() {
				for _, value := range resolved {
					So(value.ID(), ShouldNotEqual, firstPrompt.ID())
				}

				community, communityErr := secondPrompt.Property(primitive.COMMUNITY)
				label, labelErr := secondPrompt.Property(primitive.LABELS)

				So(communityErr, ShouldBeNil)
				So(labelErr, ShouldBeNil)
				So(community, ShouldEqual, 10)
				So(label, ShouldEqual, 1)
				So(secondPrompt.Role(), ShouldEqual, primitive.ValueRolePrompt)
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
		machine.community.Store(member.ID(), member)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		prompt := primitive.Emit(primitive.WithFirmware(core.CLASSIFY_READOUT))
		prompt.Set(affinityStart, 1)
		prompt.NormalizeAffinity()

		resolved, err := machine.Prompt(prompt)
		if err != nil {
			b.Fatal(err)
		}

		if len(resolved) == 0 {
			b.Fatal("prompt produced no resolved values")
		}

		machine.community.Delete(prompt.ID())
	}
}

func TestCycleRecruitResidual(t *testing.T) {
	loadConfigForTests(t)

	Convey("Given staged candidates that require saturated and residual recruiters", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		machine, err := NewMachine(ctx)
		So(err, ShouldBeNil)
		So(machine, ShouldNotBeNil)
		Reset(func() {
			machine.Close()
		})

		members := []*primitive.Value{
			emitWithAffinityRange(0, 100),
			emitWithAffinityRange(21, 100),
			emitWithAffinityRange(128, 50),
			emitWithAffinityRange(200, 50),
		}

		recruiter := primitive.Emit(
			primitive.WithFirmware(core.RECRUIT_COMMUNITY),
		)

		query := primitive.Emit(
			primitive.WithFirmware(core.QUERY),
			primitive.WithReference(recruiter.ID()),
		)

		machine.community.Store(recruiter.ID(), recruiter)
		machine.community.Store(query.ID(), query)

		for _, value := range members {
			machine.community.Store(value.ID(), value)
			machine.backend.StageInto(query.ID(), value)
		}

		Convey("When Cycle runs", func() {
			err := machine.Cycle()
			So(err, ShouldBeNil)

			Convey("Then residual candidates keep forming fresh communities", func() {
				communities := map[uint64]bool{}
				for _, value := range members {
					community, communityErr := value.Property(primitive.COMMUNITY)
					So(communityErr, ShouldBeNil)
					So(community, ShouldNotEqual, uint64(0))
					communities[community] = true
				}

				firstCommunity, _ := members[0].Property(primitive.COMMUNITY)
				secondCommunity, _ := members[2].Property(primitive.COMMUNITY)
				thirdCommunity, _ := members[3].Property(primitive.COMMUNITY)

				So(len(communities), ShouldEqual, 3)
				So(secondCommunity, ShouldNotEqual, firstCommunity)
				So(thirdCommunity, ShouldNotEqual, firstCommunity)
				So(thirdCommunity, ShouldNotEqual, secondCommunity)
				So(members[1].Get(primitive.PropertiesRegion)[primitive.COMMUNITY], ShouldEqual, firstCommunity)
			})
		})
	})
}

func TestLoad(t *testing.T) {
	loadConfigForTests(t)

	Convey("Given a machine and the default Alice corpus", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		machine, err := NewMachine(ctx)
		So(err, ShouldBeNil)
		So(machine, ShouldNotBeNil)
		Reset(func() {
			machine.Close()
		})

		corpus, err := os.ReadFile(filepath.Join("..", "..", "cmd", "cfg", "alice.txt"))
		So(err, ShouldBeNil)

		dataset := local.New(local.WithBytes(corpus))

		Convey("When Load runs to quiescence", func() {
			err := machine.Load(dataset)
			So(err, ShouldBeNil)

			Convey("Then every token Value has joined a community", func() {
				dataValues := 0
				orphanValues := 0
				communities := map[uint64]bool{}
				communityAffinity := map[uint64][primitive.AffinityWords]uint64{}

				machine.community.Range(func(key, value any) bool {
					member := value.(*primitive.Value)
					if !hasTokenWords(member) {
						return true
					}

					dataValues++

					community, communityErr := member.Property(primitive.COMMUNITY)
					So(communityErr, ShouldBeNil)
					if community == 0 {
						orphanValues++
						return true
					}

					communities[community] = true
					fingerprint := member.AffinityArray()
					union := communityAffinity[community]
					for idx := range union {
						union[idx] |= fingerprint[idx]
					}
					communityAffinity[community] = union
					return true
				})

				So(dataValues, ShouldBeGreaterThan, 0)
				So(orphanValues, ShouldEqual, 0)
				So(len(communities), ShouldBeGreaterThan, 1)
				for _, union := range communityAffinity {
					So(primitive.AffinityBitCount(union), ShouldBeLessThanOrEqualTo, 121)
				}
			})
		})
	})
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
