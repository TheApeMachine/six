package cpu

import (
	"context"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/program"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

const toyWorldFeedOrderingSkip = "TODO: re-enable once multi-instruction causal toy is validated under feed-only compilation ordering (tracking: file issue/PR when picking this up — pkg/compute/kernel/cpu/toy_world_test.go)"

func TestToyWorld_CausalIntervention(t *testing.T) {
	SkipConvey(toyWorldFeedOrderingSkip, t, func() {
		Convey("Given layout and causal intervention feed program", func() {
			lay := program.Layout{
				Regions: map[string]program.RegionExtent{
					"program":    {Start: 16, Words: 16},
					"signals":    {Start: 32, Words: 8},
					"context":    {Start: 40, Words: 8},
					"properties": {Start: 56, Words: 16},
					"id":         {Start: 122, Words: 1},
				},
				Properties: map[string]int{
					"ttl":          3,
					"continuation": 15,
				},
				Opcodes: program.Opcodes,
			}

			src := `
	[ { A(signals[7,1]) B(context) B(signals) -> any_zero } ]
	[ { { A(signals[7,1]) 0 != } ? } ]
	[ { A(properties.ttl) B(properties.ttl) 1 \ } ]
	[ { { A(properties.ttl) 0 == } ? } ]
	[ { A(properties.continuation) 0 B } ]
	`

			comp, err := program.Compile(context.Background(), src, lay)
			So(err, ShouldBeNil)

			Convey("When HypercubeGossip runs with intervention state", func() {
				v1 := primitive.AllocValue()
				v1.StampID()
				frame := (*[128]uint64)(unsafe.Pointer(v1))

				copy(frame[primitive.ProgramStartWord:primitive.ProgramStartWord+primitive.ProgramWords], comp.Words)

				frame[59] = 1
				frame[71] = v1.ID()
				frame[40] = 1
				frame[32] = 0

				HypercubeGossip(nil, []*primitive.Value{v1})

				So(frame[39], ShouldEqual, 1)
				So(frame[59], ShouldEqual, 0)
				So(frame[71], ShouldEqual, 0)
			})
		})
	})
}

func TestToyWorld_SpawnedLineage(t *testing.T) {
	SkipConvey(toyWorldFeedOrderingSkip, t, func() {
		Convey("Given layout and spawn-on-falsify feed program", func() {
			lay := program.Layout{
				Regions: map[string]program.RegionExtent{
					"program":    {Start: 16, Words: 16},
					"signals":    {Start: 32, Words: 8},
					"context":    {Start: 40, Words: 8},
					"properties": {Start: 56, Words: 16},
					"id":         {Start: 122, Words: 1},
				},
				Properties: map[string]int{
					"ttl":          3,
					"continuation": 15,
				},
				Opcodes: program.Opcodes,
			}

			src := `
	[ { A(signals[7,1]) B(context) B(signals) -> any_zero } ]
	[ { { A(signals[7,1]) 0 != } ? } ]
	[ { A(context) 0 spawn } ]
	`

			comp, err := program.Compile(context.Background(), src, lay)
			So(err, ShouldBeNil)

			Convey("When gossip runs until spawn", func() {
				v1 := primitive.AllocValue()
				v1.StampID()
				frame := (*[128]uint64)(unsafe.Pointer(v1))

				copy(frame[primitive.ProgramStartWord:primitive.ProgramStartWord+primitive.ProgramWords], comp.Words)
				frame[40] = 1
				frame[32] = 0
				frame[59] = 10

				HypercubeGossip(nil, []*primitive.Value{v1})
				spawned := HypercubeGossip(nil, []*primitive.Value{v1})

				So(len(spawned), ShouldEqual, 1)

				sFrame := (*[128]uint64)(unsafe.Pointer(spawned[0]))
				So(sFrame[122] == 0 || sFrame[122] == v1.ID(), ShouldBeFalse)
				So(sFrame[59], ShouldEqual, 10)
				So(spawned[0].HasProgram(), ShouldBeFalse)
				So(spawned[0].SchedulingNext(), ShouldEqual, 0)
				So(spawned[0].Status(), ShouldEqual, primitive.PENDING)
			})
		})
	})
}

func TestStructuralComponentEmitsThroughProgram(t *testing.T) {
	loadHypercubeGossipConfig(t)

	owner := primitive.Emit()
	defer owner.Close()
	candidate := primitive.Emit()
	defer candidate.Close()

	compiled := core.Cfg.Programs[core.FOLD_SUBSTRATE].Compiled()

	if !owner.InstallProgram(compiled) {
		t.Fatal("install structural_component failed")
	}

	ownerFrame := (*[128]uint64)(unsafe.Pointer(owner))
	candidateFrame := (*[128]uint64)(unsafe.Pointer(candidate))
	ownerFrame[primitive.TokensStartWord] = 0b1010
	candidateFrame[primitive.TokensStartWord] = 0b1110
	ownerFrame[primitive.TokensStartWord+8] = ^uint64(0)
	candidateFrame[primitive.TokensStartWord+8] = ^uint64(0)

	spawned := HypercubeGossip(owner, []*primitive.Value{owner, candidate})
	defer primitive.CloseAll(spawned)

	if len(spawned) == 0 {
		t.Fatalf("expected structural_component emit sites to spawn values")
	}

	frame := (*[128]uint64)(unsafe.Pointer(spawned[0]))
	if frame[primitive.IDStartWord] == 0 || frame[primitive.IDStartWord] == candidate.ID() {
		t.Fatalf("expected emitted value to receive a fresh id")
	}

	if frame[primitive.PrevStartWord] == 0 {
		t.Fatalf("expected emitted value prev to be set by the program")
	}
	if frame[primitive.NextStartWord] == 0 {
		t.Fatalf("expected emitted value next to be set by the program")
	}
}
