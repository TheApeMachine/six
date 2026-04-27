package vm

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

var configOnce sync.Once

func loadConfigForTests(t *testing.T) {
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
			err := machine.Cycle()
			So(err, ShouldBeNil)

			Convey("Then the query program had its expected instruction shape", func() {
				words := query.Get(primitive.ProgramRegion)
				// Seed (topo=PopQueue), write reference, stage(B) with popEnd bit.
				So(words[0]>>55&3, ShouldEqual, 1)
				So(words[1]>>55&3, ShouldEqual, 0)
				So(words[2]>>55&3, ShouldEqual, 0)
				So(words[2]>>62&1, ShouldEqual, 1) // stage bit
				So(words[2]>>63&1, ShouldEqual, 1) // pop end
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
