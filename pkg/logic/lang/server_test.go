package lang

import (
	"context"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/numeric"
)

/*
seedValue returns one deterministic lexical seed converted to primitive.Value.
*/
func seedValue(symbol byte) primitive.Value {
	return primitive.Value(primitive.BaseValue(symbol))
}

/*
valueWithPhaseAndBits builds one primitive value with optional core bits and phase.
*/
func valueWithPhaseAndBits(phase numeric.Phase, bits ...int) primitive.Value {
	value, err := primitive.New()
	if err != nil {
		panic(err)
	}

	for _, bit := range bits {
		value.Set(bit)
	}

	if phase > 0 {
		value.SetStatePhase(phase)
	}

	return value
}

/*
TestProgramServerExecuteStable verifies Execute returns a stable outcome when one
candidate residue exactly matches the target.
*/
func TestProgramServerExecuteStable(t *testing.T) {
	gc.Convey("Given a candidate whose recovered residue equals target", t, func() {
		server := NewProgramServer(
			ProgramServerWithContext(context.Background()),
			ProgramServerWithMaxSteps(1),
		)

		start := valueWithPhaseAndBits(3, 5)
		target := valueWithPhaseAndBits(9, 17)
		err := server.Seed(start, target)
		gc.So(err, gc.ShouldBeNil)

		candidates := []primitive.Value{
			valueWithPhaseAndBits(3, 5, 17),
		}

		outcome, execErr := server.Execute(candidates)
		gc.So(execErr, gc.ShouldBeNil)
		gc.So(outcome, gc.ShouldNotBeNil)
		gc.So(outcome.WinnerIndex, gc.ShouldEqual, 0)
		gc.So(outcome.PostResidue, gc.ShouldEqual, 0)
		gc.So(outcome.Steps, gc.ShouldEqual, 1)

		residue, residueErr := outcome.RecoveredState.XOR(target)
		gc.So(residueErr, gc.ShouldBeNil)
		gc.So(residue.ActiveCount(), gc.ShouldEqual, 0)
	})
}

/*
TestProgramServerExecuteValidation verifies Execute rejects missing boundaries and candidates.
*/
func TestProgramServerExecuteValidation(t *testing.T) {
	gc.Convey("Given empty start and target", t, func() {
		server := NewProgramServer(
			ProgramServerWithContext(context.Background()),
		)

		outcome, execErr := server.Execute([]primitive.Value{primitive.NeutralValue()})
		gc.So(execErr, gc.ShouldNotBeNil)
		gc.So(execErr.Error(), gc.ShouldContainSubstring, string(ProgramErrorTypeStartAndTargetEmpty))
		gc.So(outcome, gc.ShouldBeNil)
	})

	gc.Convey("Given no candidates", t, func() {
		server := NewProgramServer(
			ProgramServerWithContext(context.Background()),
		)

		err := server.Seed(seedValue('A'), valueWithPhaseAndBits(0, 17))
		gc.So(err, gc.ShouldBeNil)

		outcome, execErr := server.Execute(nil)
		gc.So(execErr, gc.ShouldNotBeNil)
		gc.So(execErr.Error(), gc.ShouldContainSubstring, string(ProgramErrorTypeCandidatePoolEmpty))
		gc.So(outcome, gc.ShouldBeNil)
	})
}

/*
TestProgramServerExecuteProgramStalled verifies Execute fails when no candidate
has a usable phase quotient.
*/
func TestProgramServerExecuteProgramStalled(t *testing.T) {
	gc.Convey("Given candidates with zero phase quotient", t, func() {
		server := NewProgramServer(
			ProgramServerWithContext(context.Background()),
			ProgramServerWithMaxSteps(1),
		)

		err := server.Seed(seedValue('A'), valueWithPhaseAndBits(7, 17))
		gc.So(err, gc.ShouldBeNil)

		candidates := []primitive.Value{
			valueWithPhaseAndBits(0, 9),
		}

		outcome, execErr := server.Execute(candidates)
		gc.So(execErr, gc.ShouldNotBeNil)
		gc.So(execErr.Error(), gc.ShouldContainSubstring, string(ProgramErrorTypeProgramStalled))
		gc.So(outcome, gc.ShouldBeNil)
	})
}

/*
TestProgramServerExecuteExhausted verifies Execute reports exhaustion after one
advancing step when maxSteps is reached.
*/
func TestProgramServerExecuteExhausted(t *testing.T) {
	gc.Convey("Given a one-step candidate pool that advances without converging", t, func() {
		server := NewProgramServer(
			ProgramServerWithContext(context.Background()),
			ProgramServerWithMaxSteps(1),
		)
		server.start = seedValue('A')

		target, err := primitive.New()
		gc.So(err, gc.ShouldBeNil)
		target.SetStatePhase(5)
		target.Set(17)
		server.target = target

		candidates := []primitive.Value{
			func() primitive.Value {
				candidate, candidateErr := primitive.New()
				if candidateErr != nil {
					panic(candidateErr)
				}
				candidate.SetStatePhase(5)
				return candidate
			}(),
		}

		outcome, execErr := server.Execute(candidates)
		gc.So(execErr, gc.ShouldNotBeNil)
		gc.So(execErr.Error(), gc.ShouldContainSubstring, string(ProgramErrorTypeProgramExhausted))
		gc.So(outcome, gc.ShouldBeNil)
	})
}

/*
BenchmarkProgramServerExecuteExhausted measures one-step exhausted execution.
*/
func BenchmarkProgramServerExecuteExhausted(b *testing.B) {
	server := NewProgramServer(
		ProgramServerWithContext(context.Background()),
		ProgramServerWithMaxSteps(1),
	)
	server.start = seedValue('A')

	target, err := primitive.New()
	if err != nil {
		b.Fatalf("target allocation failed: %v", err)
	}
	target.SetStatePhase(5)
	target.Set(17)
	server.target = target

	candidates := []primitive.Value{
		func() primitive.Value {
			candidate, candidateErr := primitive.New()
			if candidateErr != nil {
				b.Fatalf("candidate allocation failed: %v", candidateErr)
			}
			candidate.SetStatePhase(5)
			return candidate
		}(),
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		outcome, err := server.Execute(candidates)
		if err == nil {
			b.Fatalf("expected exhausted error")
		}

		if outcome != nil {
			b.Fatalf("unexpected outcome: %+v", outcome)
		}
	}
}
