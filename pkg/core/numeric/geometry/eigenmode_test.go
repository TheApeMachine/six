package geometry

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core/numeric/gf"
)

func TestEigenmodeMembers(t *testing.T) {
	t.Parallel()

	Convey("Members returns an independent copy of the mode list", t, func() {
		mode := &Eigenmode{
			members: []uint64{1, 2, 3},
			energy:  4.5,
		}

		copySlice := mode.Members()

		copySlice[0] = 99

		So(mode.members[0], ShouldEqual, 1)
		So(len(copySlice), ShouldEqual, 3)
	})
}

func TestEigenmodeEnergy(t *testing.T) {
	t.Parallel()

	Convey("Energy exposes the aggregate score", t, func() {
		mode := &Eigenmode{energy: 12.25}

		So(mode.Energy(), ShouldEqual, 12.25)
	})
}

func TestDetectModes(t *testing.T) {
	t.Parallel()

	Convey("Given no participants", t, func() {
		modes, dominant := DetectModes(nil, 0.5, func(a, b uint64) float64 { return 0 })

		So(len(modes), ShouldEqual, 0)
		So(dominant, ShouldEqual, -1)
	})

	Convey("Given participants with pairwise coupling at threshold", t, func() {
		participants := []ModeParticipant{
			{Origin: 10, Energy: 1},
			{Origin: 20, Energy: 2},
			{Origin: 30, Energy: 4},
		}

		couple := func(a, b uint64) float64 {
			if a == 10 && b == 20 || a == 20 && b == 10 {
				return 1
			}

			return 0
		}

		modes, dominant := DetectModes(participants, 1.0, couple)

		So(len(modes), ShouldEqual, 2)
		So(dominant, ShouldEqual, 1)
		So(modes[1].Energy(), ShouldEqual, 4)
		So(len(modes[0].members), ShouldEqual, 2)
	})
}

func TestDetectPhaseMode257(t *testing.T) {
	t.Parallel()

	Convey("An empty phase vector has no dominant lane", t, func() {
		var vector gf.Vector257

		mode := DetectPhaseMode257(vector)

		So(mode.Index, ShouldEqual, -1)
	})

	Convey("The strongest occupied lane wins", t, func() {
		var vector gf.Vector257

		vector[17] = 900
		vector[18] = 100

		mode := DetectPhaseMode257(vector)

		So(mode.Index, ShouldEqual, 17)
		So(mode.Amplitude, ShouldEqual, 900)
		So(mode.Concentration, ShouldAlmostEqual, 0.9, 1e-9)
	})
}

func TestDetectPhaseMode8191(t *testing.T) {
	t.Parallel()

	Convey("DetectPhaseMode8191 mirrors Dominant on Vector8191", t, func() {
		var vector gf.Vector8191

		vector[42] = 50

		mode := DetectPhaseMode8191(vector)

		So(mode.Index, ShouldEqual, 42)
		So(mode.Amplitude, ShouldEqual, 50)
		So(mode.Concentration, ShouldEqual, 1)
	})
}

func TestDetectPhaseMode65537(t *testing.T) {
	t.Parallel()

	Convey("DetectPhaseMode65537 mirrors Dominant on Vector65537", t, func() {
		var vector gf.Vector65537

		vector[5] = 1000
		vector[6] = 500

		mode := DetectPhaseMode65537(vector)

		So(mode.Index, ShouldEqual, 5)
		So(mode.Concentration, ShouldAlmostEqual, 1000.0/1500.0, 1e-9)
	})
}

func TestPhaseAlignment(t *testing.T) {
	t.Parallel()

	Convey("PhaseAlignment defers to gf.Alignment for lane agreement", t, func() {
		left := PhaseMode{Index: 7}
		right := PhaseMode{Index: 7}

		So(PhaseAlignment(left, right), ShouldEqual, 1)

		left.Index = 0
		right.Index = 128

		So(PhaseAlignment(left, right), ShouldEqual, 0)
	})
}

func TestPhaseModeFromDominant(t *testing.T) {
	t.Parallel()

	Convey("phaseModeFromDominant maps DominantPhase fields", t, func() {
		d := gf.DominantPhase{
			Index:         3,
			Amplitude:     200,
			Concentration: 0.42,
		}

		mode := phaseModeFromDominant(d)

		So(mode.Index, ShouldEqual, 3)
		So(mode.Amplitude, ShouldEqual, 200)
		So(mode.Concentration, ShouldEqual, 0.42)
	})
}

func BenchmarkDetectModes(b *testing.B) {
	participants := make([]ModeParticipant, 64)

	for idx := range participants {
		participants[idx] = ModeParticipant{Origin: uint64(idx + 1), Energy: float64(idx%7 + 1)}
	}

	couple := func(a, b uint64) float64 {
		if (a+b)%3 == 0 {
			return 1
		}

		return 0
	}

	var modes []Eigenmode

	var dominant int

	b.ResetTimer()

	for b.Loop() {
		modes, dominant = DetectModes(participants, 0.7, couple)
	}

	_ = modes
	_ = dominant
}

func BenchmarkDetectPhaseMode257(b *testing.B) {
	var vector gf.Vector257

	vector[13] = 400
	vector[14] = 100

	var mode PhaseMode

	b.ResetTimer()

	for b.Loop() {
		mode = DetectPhaseMode257(vector)
	}

	_ = mode
}
