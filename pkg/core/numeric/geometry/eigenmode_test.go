package geometry

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
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

func TestDetectPhaseMode(t *testing.T) {
	t.Parallel()

	Convey("An empty field has no dominant lane", t, func() {
		vector := NewField(Mod257)

		mode := DetectPhaseMode(vector)

		So(mode.Index, ShouldEqual, -1)
	})

	Convey("The strongest occupied lane wins", t, func() {
		vector := NewField(Mod257)

		vector.Fields[17] = &Field{modulus: Mod257, amplitude: 900}
		vector.Fields[18] = &Field{modulus: Mod257, amplitude: 100}

		mode := DetectPhaseMode(vector)

		So(mode.Index, ShouldEqual, 17)
		So(mode.Amplitude, ShouldEqual, 900)
		So(mode.Concentration, ShouldAlmostEqual, 0.9, 1e-9)
	})

	Convey("DetectPhaseMode aliases match DetectPhaseMode for each modulus", t, func() {
		f8191 := NewField(Mod8191)

		f8191.Fields[42] = &Field{modulus: Mod8191, amplitude: 100}

		So(DetectPhaseMode8191(f8191), ShouldResemble, DetectPhaseMode(f8191))

		f65537 := NewField(Mod65537)

		f65537.Fields[5] = &Field{modulus: Mod65537, amplitude: 50}
		f65537.Fields[6] = &Field{modulus: Mod65537, amplitude: 20}

		So(DetectPhaseMode65537(f65537), ShouldResemble, DetectPhaseMode(f65537))
	})
}

func TestPhaseAlignment(t *testing.T) {
	t.Parallel()

	Convey("PhaseAlignment uses the field ring width", t, func() {
		field := NewField(Mod257)

		left := PhaseMode{Index: 7}
		right := PhaseMode{Index: 7}

		So(PhaseAlignment(left, right, field), ShouldEqual, 1)

		left.Index = 0
		right.Index = 128

		So(PhaseAlignment(left, right, field), ShouldEqual, 0)
	})
}

func TestPhaseModeFromDominant(t *testing.T) {
	t.Parallel()

	Convey("phaseModeFromDominant maps PhaseMode fields", t, func() {
		d := PhaseMode{
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

func BenchmarkDetectPhaseMode(b *testing.B) {
	vector := NewField(Mod257)

	vector.Fields[13] = &Field{modulus: Mod257, amplitude: 50}
	vector.Fields[14] = &Field{modulus: Mod257, amplitude: 100}

	var mode PhaseMode

	b.ResetTimer()

	for b.Loop() {
		mode = DetectPhaseMode(vector)
	}

	_ = mode
}
