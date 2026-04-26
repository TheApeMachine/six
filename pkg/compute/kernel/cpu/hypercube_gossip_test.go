package cpu

import (
	"errors"
	"math/bits"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/program"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
)

const hypercubeGossipConfigEnv = "CONFIG_PATH"
const DefaultConfigPath = "cmd/cfg/config.yml"

var hypercubeGossipConfigOnce sync.Once
var hypercubeGossipConfigErr error

func TestHypercubeGossip(t *testing.T) {
	Convey("Given a fold topology instruction", t, func() {
		layout := program.Layout{
			Regions: map[string]program.RegionExtent{
				"tokens":  {Start: primitive.TokensStartWord, Words: primitive.TokensWords},
				"signals": {Start: primitive.SignalsStartWord, Words: primitive.SignalsWords},
			},
		}
		compiled, err := program.Compile(`[ { A(signals[0,1]) B(tokens[0,1]) | fold } ]`, layout)
		So(err, ShouldBeNil)

		first := primitive.Emit()
		defer first.Close()
		second := primitive.Emit()
		defer second.Close()
		third := primitive.Emit()
		defer third.Close()

		So(first.InstallProgram(compiled.Words), ShouldBeTrue)
		So(second.InstallProgram(compiled.Words), ShouldBeTrue)
		So(third.InstallProgram(compiled.Words), ShouldBeTrue)

		first.Set(primitive.TokensStartWord, 0b001)
		second.Set(primitive.TokensStartWord, 0b010)
		third.Set(primitive.TokensStartWord, 0b100)

		HypercubeGossip(nil, []*primitive.Value{first, second, third})

		So(first.Get(primitive.SignalsRegion)[0], ShouldEqual, 0b111)
		So(second.Get(primitive.SignalsRegion)[0], ShouldEqual, 0b111)
		So(third.Get(primitive.SignalsRegion)[0], ShouldEqual, 0b111)
	})

	Convey("Given an in-band geometric instruction", t, func() {
		composeOp := program.Opcodes["compose"]
		compiled := program.Compiled{Words: []uint64{
			program.EncodeInstruction(
				primitive.ContextStartWord, primitive.ContextWords,
				primitive.GradientStartWord, primitive.GradientWords,
				primitive.SignalsStartWord, primitive.SignalsWords,
				composeOp, program.ModeGeometric, program.TopologySelf,
				0, 0, 0, program.InstrBTypeDirect,
			) | program.InstrFlagTargetOwner,
		}}

		actual := primitive.Emit()
		defer actual.Close()
		reference := primitive.Emit()
		defer reference.Close()

		So(actual.InstallProgram(compiled.Words), ShouldBeTrue)

		actualFrame := (*[primitive.WordCount]uint64)(unsafe.Pointer(actual))
		referenceFrame := (*[primitive.WordCount]uint64)(unsafe.Pointer(reference))

		for lane := 0; lane < primitive.ContextWords; lane++ {
			value := float64(lane + 1)
			gradient := float64(lane+1) / 8.0
			(*[primitive.ContextWords]float64)(unsafe.Pointer(&actualFrame[primitive.ContextStartWord]))[lane] = value
			(*[primitive.GradientWords]float64)(unsafe.Pointer(&actualFrame[primitive.GradientStartWord]))[lane] = gradient
			(*[primitive.ContextWords]float64)(unsafe.Pointer(&referenceFrame[primitive.ContextStartWord]))[lane] = value
			(*[primitive.GradientWords]float64)(unsafe.Pointer(&referenceFrame[primitive.GradientStartWord]))[lane] = gradient
		}

		So(geometricFrameGeneric(unsafe.Pointer(reference), 0x10), ShouldBeTrue)
		HypercubeGossip(nil, []*primitive.Value{actual})

		for lane := 0; lane < primitive.SignalsWords; lane++ {
			So(actualFrame[primitive.SignalsStartWord+lane], ShouldEqual, referenceFrame[primitive.SignalsStartWord+lane])
		}
	})

	Convey("Given a bare A/B implicit map pipeline", t, func() {
		layout := program.Layout{
			Regions: map[string]program.RegionExtent{
				"signals": {Start: primitive.SignalsStartWord, Words: primitive.SignalsWords},
			},
		}
		compiled, err := program.Compile(`[(B popcnt)] <= [(A B ^)]`, layout)
		So(err, ShouldBeNil)

		owner := primitive.Emit()
		defer owner.Close()
		candidate := primitive.Emit()
		defer candidate.Close()

		So(owner.InstallProgram(compiled.Words), ShouldBeTrue)

		owner.Set(primitive.SignalsStartWord, 0b1010)
		candidate.Set(primitive.SignalsStartWord, 0b1111)

		HypercubeGossip(owner, []*primitive.Value{owner, candidate})

		So(candidate.Get(primitive.SignalsRegion)[0], ShouldEqual, 2)
	})

	Convey("Given a geometric instruction with encoded operands and destination", t, func() {
		composeOp := program.Opcodes["compose"]
		compiled := program.Compiled{Words: []uint64{
			program.EncodeInstruction(
				primitive.TokensStartWord, 8,
				primitive.GradientStartWord, primitive.GradientWords,
				primitive.AssetStartWord+8, 8,
				composeOp, program.ModeGeometric, program.TopologySelf,
				0, 0, 0, program.InstrBTypeDirect,
			) | program.InstrFlagTargetOwner,
		}}

		actual := primitive.Emit()
		defer actual.Close()

		reference := primitive.Emit()
		defer reference.Close()

		So(actual.InstallProgram(compiled.Words), ShouldBeTrue)

		actualFrame := (*[primitive.WordCount]uint64)(unsafe.Pointer(actual))
		referenceFrame := (*[primitive.WordCount]uint64)(unsafe.Pointer(reference))

		for lane := 0; lane < primitive.ContextWords; lane++ {
			left := float64(lane+2) / 3.0
			right := float64(lane+5) / 7.0
			(*[primitive.ContextWords]float64)(unsafe.Pointer(&actualFrame[primitive.TokensStartWord]))[lane] = left
			(*[primitive.GradientWords]float64)(unsafe.Pointer(&actualFrame[primitive.GradientStartWord]))[lane] = right
			(*[primitive.ContextWords]float64)(unsafe.Pointer(&referenceFrame[primitive.ContextStartWord]))[lane] = left
			(*[primitive.GradientWords]float64)(unsafe.Pointer(&referenceFrame[primitive.GradientStartWord]))[lane] = right
		}

		So(GeometricFrame(unsafe.Pointer(reference), 0x10), ShouldBeTrue)
		HypercubeGossip(nil, []*primitive.Value{actual})

		for lane := 0; lane < primitive.SignalsWords; lane++ {
			So(actualFrame[primitive.AssetStartWord+8+lane], ShouldEqual, referenceFrame[primitive.SignalsStartWord+lane])
			So(actualFrame[primitive.SignalsStartWord+lane], ShouldEqual, 0)
		}
	})

	Convey("Given program_select running as a resident selector", t, func() {
		loadHypercubeGossipConfig(t)

		selector := primitive.Emit(primitive.WithFirmware(core.PROGRAM_SELECT))
		defer selector.Close()
		selector.SetProperty(primitive.COMMUNITY, 1)

		candidate := primitive.Emit()
		defer candidate.Close()

		candidate.SetProperty(primitive.COMMUNITY, 1)
		candidate.SetProperty(primitive.SURPRISAL, 512)

		HypercubeGossip(selector, []*primitive.Value{selector, candidate})

		programID, err := candidate.Property(primitive.PROGRAM_ID)

		So(err, ShouldBeNil)
		So(programID, ShouldEqual, 3)
		So(candidate.SchedulingNext(), ShouldEqual, candidate.ID())
		So(selector.SchedulingNext(), ShouldEqual, 0)
	})

	Convey("Given a refuted Value", t, func() {
		loadHypercubeGossipConfig(t)

		selector := primitive.Emit(primitive.WithFirmware(core.PROGRAM_SELECT))
		defer selector.Close()
		selector.SetProperty(primitive.COMMUNITY, 1)

		candidate := primitive.Emit()
		defer candidate.Close()

		candidate.SetProperty(primitive.COMMUNITY, 1)
		candidate.SetProperty(primitive.SURPRISAL, 512)
		candidate.SetProperty(primitive.NOISE, 1)

		HypercubeGossip(selector, []*primitive.Value{selector, candidate})

		programID, err := candidate.Property(primitive.PROGRAM_ID)

		So(err, ShouldBeNil)
		So(programID, ShouldEqual, 6)
	})

	Convey("Given an unassigned Value", t, func() {
		loadHypercubeGossipConfig(t)

		selector := primitive.Emit(primitive.WithFirmware(core.PROGRAM_SELECT))
		defer selector.Close()
		selector.SetProperty(primitive.COMMUNITY, 1)

		candidate := primitive.Emit()
		defer candidate.Close()

		candidate.SetProperty(primitive.SURPRISAL, 512)

		HypercubeGossip(selector, []*primitive.Value{selector, candidate})

		programID, err := candidate.Property(primitive.PROGRAM_ID)

		So(err, ShouldBeNil)
		So(programID, ShouldEqual, 8)
		So(candidate.SchedulingNext(), ShouldEqual, candidate.ID())
	})

	Convey("Given a community recruiter", t, func() {
		loadHypercubeGossipConfig(t)

		recruiter := primitive.Emit(primitive.WithFirmware(core.RECRUIT_COMMUNITY))
		defer recruiter.Close()

		accepted := primitive.Emit()
		defer accepted.Close()
		accepted.SetStatus(primitive.ERROR)

		assigned := primitive.Emit()
		defer assigned.Close()
		assigned.SetStatus(primitive.ERROR)

		rejected := primitive.Emit()
		defer rejected.Close()

		recruiter.Set(primitive.AffinityStartWord, 0b0011)
		accepted.Set(primitive.AffinityStartWord, 0b0100)
		assigned.Set(primitive.AffinityStartWord, 0b1000)
		assigned.SetProperty(primitive.COMMUNITY, 999)
		for lane := 0; lane < primitive.AffinityWords; lane++ {
			rejected.Set(primitive.AffinityStartWord+lane, ^uint64(0))
		}
		rejected.NormalizeAffinity()

		HypercubeGossip(recruiter, []*primitive.Value{recruiter, accepted, assigned, rejected})

		recruiterCommunity, err := recruiter.Property(primitive.COMMUNITY)
		So(err, ShouldBeNil)
		So(recruiterCommunity, ShouldEqual, recruiter.ID())

		recruiterConfidence, err := recruiter.Property(primitive.CONFIDENCE)
		So(err, ShouldBeNil)
		So(recruiterConfidence, ShouldEqual, 3)

		acceptedCommunity, err := accepted.Property(primitive.COMMUNITY)
		So(err, ShouldBeNil)
		So(acceptedCommunity, ShouldEqual, recruiter.ID())
		So(accepted.Status(), ShouldEqual, primitive.PENDING)
		So(accepted.Get(primitive.SignalsRegion)[0], ShouldEqual, 0)

		assignedCommunity, err := assigned.Property(primitive.COMMUNITY)
		So(err, ShouldBeNil)
		So(assignedCommunity, ShouldEqual, 999)
		So(assigned.Status(), ShouldEqual, primitive.ERROR)
		So(assigned.Get(primitive.SignalsRegion)[0], ShouldEqual, 0)

		rejectedCommunity, err := rejected.Property(primitive.COMMUNITY)
		So(err, ShouldBeNil)
		So(rejectedCommunity, ShouldEqual, 0)
		So(rejected.Get(primitive.SignalsRegion)[0], ShouldEqual, 0)

		So(recruiter.Get(primitive.AffinityRegion)[0], ShouldEqual, 0b0111)
		So(recruiter.Get(primitive.SignalsRegion)[0], ShouldEqual, 0)
	})

	Convey("Given a candidate inside route budget but beyond Shannon saturation", t, func() {
		loadHypercubeGossipConfig(t)

		recruiter := primitive.Emit(primitive.WithFirmware(core.RECRUIT_COMMUNITY))
		defer recruiter.Close()
		setAffinityPrefix(recruiter, 1)

		accepted := primitive.Emit()
		defer accepted.Close()
		setAffinityPrefix(accepted, 119)

		saturated := primitive.Emit()
		defer saturated.Close()
		setAffinityPrefix(saturated, 120)

		HypercubeGossip(recruiter, []*primitive.Value{recruiter, accepted, saturated})

		acceptedCommunity, err := accepted.Property(primitive.COMMUNITY)
		So(err, ShouldBeNil)
		So(acceptedCommunity, ShouldEqual, recruiter.ID())

		saturatedCommunity, err := saturated.Property(primitive.COMMUNITY)
		So(err, ShouldBeNil)
		So(saturatedCommunity, ShouldEqual, 0)

		recruiterConfidence, err := recruiter.Property(primitive.CONFIDENCE)
		So(err, ShouldBeNil)
		So(recruiterConfidence, ShouldEqual, 119)
	})

	Convey("Given a matching program carrier", t, func() {
		loadHypercubeGossipConfig(t)

		carrier := primitive.Emit(
			primitive.WithFirmware(core.PROGRAM_CARRIER),
			primitive.WithProgramID(6),
		)
		defer carrier.Close()

		candidate := primitive.Emit()
		defer candidate.Close()
		candidate.SetProperty(primitive.PROGRAM_ID, 6)

		payload := core.Cfg.Programs[core.CAUSAL_HUB].Compiled()
		for idx, word := range payload {
			carrier.Set(primitive.AssetStartWord+idx, word)
		}

		HypercubeGossip(carrier, []*primitive.Value{carrier, candidate})

		got := candidate.Get(primitive.ProgramRegion)

		for idx, word := range payload {
			So(got[idx], ShouldEqual, word)
		}
		So(candidate.SchedulingNext(), ShouldEqual, candidate.ID())
		So(candidate.Status(), ShouldEqual, primitive.READY)
	})
}

func loadHypercubeGossipConfig(t *testing.T) {
	t.Helper()

	hypercubeGossipConfigOnce.Do(func() {
		_, file, _, ok := runtime.Caller(0)
		if !ok {
			hypercubeGossipConfigErr = errors.New("cannot resolve cpu test file")
			return
		}

		configPath := hypercubeGossipConfigPath(file)

		viper.SetConfigFile(configPath)
		hypercubeGossipConfigErr = viper.ReadInConfig()
		if hypercubeGossipConfigErr != nil {
			return
		}

		core.Cfg = core.NewConfig()
	})

	if hypercubeGossipConfigErr != nil {
		t.Fatalf("load hypercube gossip config: %v", hypercubeGossipConfigErr)
	}
}

func hypercubeGossipConfigPath(file string) string {
	if configured := os.Getenv(hypercubeGossipConfigEnv); configured != "" {
		return filepath.Clean(configured)
	}

	if filepath.IsAbs(DefaultConfigPath) {
		return filepath.Clean(DefaultConfigPath)
	}

	if _, err := os.Stat(DefaultConfigPath); err == nil {
		return filepath.Clean(DefaultConfigPath)
	}

	return filepath.Clean(filepath.Join(
		filepath.Dir(file),
		"..", "..", "..", "..",
		"cmd", "cfg", "config.yml",
	))
}

func setAffinityPrefix(value *primitive.Value, bitCount int) {
	if value == nil {
		return
	}

	for bit := 0; bit < bitCount && bit < primitive.AffinityBits; bit++ {
		value.Set(primitive.AffinityStartWord+(bit/64), (*value)[primitive.AffinityStartWord+(bit/64)]|uint64(1<<(bit%64)))
	}

	value.NormalizeAffinity()
}

func TestPopcntWords(t *testing.T) {
	Convey("Given a wide word slice", t, func() {
		words := make([]uint64, 31)
		var expected uint64

		for idx := range words {
			words[idx] = uint64(idx+1) * 0x9E3779B97F4A7C15
			expected += uint64(bits.OnesCount64(words[idx]))
		}

		Convey("It should return the same total as the scalar reference", func() {
			So(popcntWords(words), ShouldEqual, expected)
		})
	})
}

func BenchmarkPopcntWords(b *testing.B) {
	var words [64]uint64
	for idx := range words {
		words[idx] = uint64(idx+1) * 0x9E3779B97F4A7C15
	}

	b.Run("Scalar", func(bb *testing.B) {
		for iteration := 0; iteration < bb.N; iteration++ {
			_ = popcntWords(words[:])
		}
	})
}
