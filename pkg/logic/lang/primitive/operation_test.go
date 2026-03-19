package primitive

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
)

/*
TestOperationEvaluateMatch verifies quotient and residue behavior.
*/
func TestOperationEvaluateMatch(t *testing.T) {
	gc.Convey("Given query and candidate with compatible phases", t, func() {
		query, err := New()
		gc.So(err, gc.ShouldBeNil)
		query.Set(1)
		query.SetStatePhase(3)

		candidate, err := New()
		gc.So(err, gc.ShouldBeNil)
		candidate.Set(1)
		candidate.Set(9)
		candidate.SetStatePhase(9)

		result := query.EvaluateMatch(candidate)

		gc.So(result.SharedBits, gc.ShouldEqual, 1)
		gc.So(result.PhaseQuotient, gc.ShouldEqual, numeric.Phase(3))
		gc.So(result.Residue.ResidualCarry(), gc.ShouldEqual, uint64(result.PhaseQuotient))
	})

	gc.Convey("Given query and candidate with missing phase", t, func() {
		query, err := New()
		gc.So(err, gc.ShouldBeNil)
		query.Set(4)

		candidate, err := New()
		gc.So(err, gc.ShouldBeNil)
		candidate.Set(7)
		candidate.SetStatePhase(5)

		result := query.EvaluateMatch(candidate)
		gc.So(result.PhaseQuotient, gc.ShouldEqual, numeric.Phase(0))
		gc.So(result.Residue.ResidualCarry(), gc.ShouldEqual, uint64(0))
	})
}

/*
TestOperationApplyAffine verifies halt detection via opcode.
*/
func TestOperationApplyAffine(t *testing.T) {
	gc.Convey("Given a halt opcode value", t, func() {
		value, err := New()
		gc.So(err, gc.ShouldBeNil)
		value.SetAffine(2, 1)
		value.SetProgram(OpcodeHalt, 0, 0, true)

		next, halt := value.ApplyAffine(7)
		gc.So(halt, gc.ShouldBeTrue)
		gc.So(next, gc.ShouldEqual, numeric.Phase((2*7+1)%numeric.FermatPrime))
	})
}

/*
TestOperationTransitionMagnitude verifies larger of core and phase magnitudes.
*/
func TestOperationTransitionMagnitude(t *testing.T) {
	gc.Convey("Given predecessor and successor values", t, func() {
		predecessor, err := New()
		gc.So(err, gc.ShouldBeNil)
		predecessor.Set(2)
		predecessor.SetStatePhase(3)

		successor, err := New()
		gc.So(err, gc.ShouldBeNil)
		successor.Set(2)
		successor.Set(20)
		successor.SetStatePhase(9)

		magnitude, err := successor.TransitionMagnitude(predecessor)
		gc.So(err, gc.ShouldBeNil)
		gc.So(magnitude, gc.ShouldBeGreaterThanOrEqualTo, numeric.Phase(1))
	})
}

/*
TestOperationComputeOperator verifies derived affine operator metadata.
*/
func TestOperationComputeOperator(t *testing.T) {
	gc.Convey("Given predecessor and successor phase", t, func() {
		predecessor, err := New()
		gc.So(err, gc.ShouldBeNil)
		predecessor.SetStatePhase(3)

		value, err := New()
		gc.So(err, gc.ShouldBeNil)
		value.Set(6)
		value.Set(11)
		value.ComputeOperator(predecessor, 9)

		scale, translate := value.Affine()
		from, to, trajectory := value.Trajectory()

		gc.So(scale, gc.ShouldEqual, numeric.Phase(3))
		gc.So(translate, gc.ShouldEqual, numeric.Phase(0))
		gc.So(trajectory, gc.ShouldBeTrue)
		gc.So(from, gc.ShouldEqual, numeric.Phase(3))
		gc.So(to, gc.ShouldEqual, numeric.Phase(9))
		gc.So(value.HasGuard(), gc.ShouldBeTrue)
	})
}

/*
TestOperationExecuteTrace verifies normal completion, halt, and discontinuity gates.
*/
func TestOperationExecuteTrace(t *testing.T) {
	gc.Convey("Given a simple non-halting path", t, func() {
		first, err := New()
		gc.So(err, gc.ShouldBeNil)
		first.SetAffine(2, 0)
		first.SetStatePhase(2)

		second, err := New()
		gc.So(err, gc.ShouldBeNil)
		second.SetAffine(2, 1)
		second.SetStatePhase(5)

		trace, stop := ExecuteTrace([]Value{first, second}, 1, 0)
		gc.So(len(trace), gc.ShouldEqual, 2)
		gc.So(stop, gc.ShouldEqual, 2)
	})

	gc.Convey("Given a path with halt opcode", t, func() {
		first, err := New()
		gc.So(err, gc.ShouldBeNil)
		first.SetAffine(2, 0)

		haltNode, err := New()
		gc.So(err, gc.ShouldBeNil)
		haltNode.SetAffine(2, 1)
		haltNode.SetProgram(OpcodeHalt, 0, 0, true)

		trace, stop := ExecuteTrace([]Value{first, haltNode}, 1, 0)
		gc.So(len(trace), gc.ShouldEqual, 2)
		gc.So(stop, gc.ShouldEqual, 1)
	})

	gc.Convey("Given path with strict discontinuity limit", t, func() {
		first, err := New()
		gc.So(err, gc.ShouldBeNil)
		first.SetAffine(1, 0)
		first.SetStatePhase(1)
		first.Set(1)

		second, err := New()
		gc.So(err, gc.ShouldBeNil)
		second.SetAffine(1, 0)
		second.SetStatePhase(10)
		second.Set(200)

		trace, stop := ExecuteTrace([]Value{first, second}, 1, 0)
		gc.So(len(trace), gc.ShouldEqual, 2)
		gc.So(stop, gc.ShouldEqual, 2)

		trace, stop = ExecuteTrace([]Value{first, second}, 1, 1)
		gc.So(len(trace), gc.ShouldEqual, 1)
		gc.So(stop, gc.ShouldEqual, 1)
	})
}

/*
TestOperationBuildQueryMask verifies OR accumulation and inverse phase composition.
*/
func TestOperationBuildQueryMask(t *testing.T) {
	gc.Convey("Given known values with phases", t, func() {
		first, err := New()
		gc.So(err, gc.ShouldBeNil)
		first.Set(1)
		first.SetStatePhase(3)

		second, err := New()
		gc.So(err, gc.ShouldBeNil)
		second.Set(20)
		second.SetStatePhase(9)

		mask := BuildQueryMask(first, second)
		calc := numeric.NewCalculus()
		firstInverse, inverseErr := calc.Inverse(3)
		gc.So(inverseErr, gc.ShouldBeNil)
		secondInverse, inverseErr := calc.Inverse(9)
		gc.So(inverseErr, gc.ShouldBeNil)
		expectedPhase := calc.Multiply(firstInverse, secondInverse)

		gc.So(primitiveHasBit(mask, 1), gc.ShouldBeTrue)
		gc.So(primitiveHasBit(mask, 20), gc.ShouldBeTrue)
		gc.So(mask.ResidualCarry(), gc.ShouldEqual, uint64(expectedPhase))
	})
}

/*
TestOperationBatchEvaluate verifies one output per candidate.
*/
func TestOperationBatchEvaluate(t *testing.T) {
	gc.Convey("Given a query and candidate batch", t, func() {
		query := NeutralValue()
		query.SetStatePhase(3)

		candidateA := NeutralValue()
		candidateA.SetStatePhase(9)

		candidateB := NeutralValue()
		candidateB.SetStatePhase(27)

		results := BatchEvaluate(query, []Value{candidateA, candidateB})
		gc.So(len(results), gc.ShouldEqual, 2)
		gc.So(results[0].PhaseQuotient, gc.ShouldEqual, numeric.Phase(3))
		gc.So(results[1].PhaseQuotient, gc.ShouldEqual, numeric.Phase(9))
	})
}

/*
TestOperationDiscreteLog verifies table boundary behavior.
*/
func TestOperationDiscreteLog(t *testing.T) {
	gc.Convey("Given phase table lookups", t, func() {
		gc.So(discreteLog(0), gc.ShouldEqual, numeric.Phase(0))
		gc.So(discreteLog(numeric.Phase(numeric.FermatPrime)), gc.ShouldEqual, numeric.Phase(0))
		gc.So(discreteLog(1), gc.ShouldEqual, numeric.Phase(0))
		gc.So(discreteLog(3), gc.ShouldEqual, numeric.Phase(1))
	})
}

/*
BenchmarkOperationEvaluateMatch measures matching throughput.
*/
func BenchmarkOperationEvaluateMatch(b *testing.B) {
	query := NeutralValue()
	query.Set(1)
	query.Set(10)
	query.SetStatePhase(3)

	candidate := NeutralValue()
	candidate.Set(1)
	candidate.Set(9)
	candidate.SetStatePhase(9)

	b.ResetTimer()

	for b.Loop() {
		_ = query.EvaluateMatch(candidate)
	}
}

/*
BenchmarkOperationBuildQueryMask measures query-mask construction throughput.
*/
func BenchmarkOperationBuildQueryMask(b *testing.B) {
	known := make([]Value, 16)
	for index := range known {
		value := NeutralValue()
		value.Set(index % 257)
		value.SetStatePhase(numeric.Phase((index % 256) + 1))
		known[index] = value
	}

	b.ResetTimer()

	for b.Loop() {
		_ = BuildQueryMask(known...)
	}
}

/*
BenchmarkOperationExecuteTrace measures trace execution throughput.
*/
func BenchmarkOperationExecuteTrace(b *testing.B) {
	path := make([]Value, 12)

	for index := range path {
		node := NeutralValue()
		node.SetAffine(numeric.Phase((index%7)+1), numeric.Phase(index%5))
		node.SetStatePhase(numeric.Phase((index % 128) + 1))
		path[index] = node
	}

	b.ResetTimer()

	for b.Loop() {
		_, _ = ExecuteTrace(path, 1, 0)
	}
}
