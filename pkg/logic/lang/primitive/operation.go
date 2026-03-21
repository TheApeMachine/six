package primitive

import (
	"fmt"

	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/numeric"
	config "github.com/theapemachine/six/pkg/system/core"
)

var discreteLogTable = buildDiscreteLogTable()
var calc = numeric.NewCalculus()

/*
MatchResult contains the evaluated metrics
between a query state and a stored candidate.
*/
type MatchResult struct {
	Residue       Value
	SharedBits    int
	PhaseQuotient numeric.Phase
	FitnessScore  int
}

/*
ScoreMatch computes the allocation-free match metrics between a query mask and
one candidate value. It returns shared structure, affine phase quotient, final
fitness score, and core residue popcount used as the energetic penalty term.
*/
func ScoreMatch(query Value, candidate Value) (int, numeric.Phase, int, int) {
	queryPhase := numeric.Phase(
		numeric.MersenneReduce64(query.ResidualCarry()),
	)

	candidatePhase := numeric.Phase(
		numeric.MersenneReduce64(candidate.ResidualCarry()),
	)

	var phaseQuotient numeric.Phase

	if queryPhase > 0 && candidatePhase > 0 {
		queryInv, err := calc.Inverse(queryPhase)

		if err == nil {
			phaseQuotient = calc.Multiply(candidatePhase, queryInv)
		}
	}

	sharedBits := query.Similarity(candidate)

	hole, _ := candidate.Hole(query)
	coreResidueBits := hole.CoreActiveCount()

	phaseCloseness := 0

	if phaseQuotient > 0 {
		phaseCloseness = int(numeric.FieldPrime) - int(discreteLog(phaseQuotient))
	}

	fitnessScore := sharedBits + phaseCloseness - coreResidueBits

	return sharedBits, phaseQuotient, fitnessScore, coreResidueBits
}

/*
EvaluateMatch computes the bitwise and algebraic differences
between a query (receiver) and a stored candidate. It evaluates
spatial bit-overlap alongside affine phase distance.
*/
func (query *Value) EvaluateMatch(candidate Value) MatchResult {
	residue, err := New()
	if err != nil {
		panic("EvaluateMatch: " + err.Error())
	}

	if err = candidate.HoleInto(*query, &residue); err != nil {
		panic("EvaluateMatch: " + err.Error())
	}

	sharedBits, phaseQuotient, fitnessScore, _ := ScoreMatch(*query, candidate)

	if phaseQuotient > 0 {
		// Heal the residue: the algebra dictates its new geometric phase state.
		residue.SetStatePhase(phaseQuotient)
	} else {
		// If there is no algebraic relation, strip the corrupted guard band.
		residue.SetResidualCarry(0)
	}

	return MatchResult{
		Residue:       residue,
		SharedBits:    sharedBits,
		PhaseQuotient: phaseQuotient,
		FitnessScore:  fitnessScore,
	}
}

/*
EvaluateMatchInto writes one query/candidate comparison into a caller-owned
MatchResult so hot loops can reuse residue storage across iterations.
*/
func (query *Value) EvaluateMatchInto(
	candidate Value,
	result *MatchResult,
) error {
	if result == nil {
		return fmt.Errorf("primitive: match result is nil")
	}

	if !result.Residue.IsValid() {
		return fmt.Errorf("primitive: match residue is not allocated")
	}

	if err := candidate.HoleInto(*query, &result.Residue); err != nil {
		return err
	}

	sharedBits, phaseQuotient, fitnessScore, _ := ScoreMatch(*query, candidate)

	if phaseQuotient > 0 {
		result.Residue.SetStatePhase(phaseQuotient)
	} else {
		result.Residue.SetResidualCarry(0)
	}

	result.SharedBits = sharedBits
	result.PhaseQuotient = phaseQuotient
	result.FitnessScore = fitnessScore

	return nil
}

/*
ApplyAffine computes the next phase using the embedded affine
operator (ax+b mod 8191). Returns the resulting phase and a
boolean indicating if the halt opcode was encountered.
*/
func (value *Value) ApplyAffine(
	incoming numeric.Phase,
) (numeric.Phase, bool) {
	outgoing := value.ApplyAffinePhase(incoming)
	opcode := Opcode(value.Opcode())

	return outgoing, opcode == OpcodeHalt
}

/*
TransitionMagnitude calculates the discontinuity between
the predecessor and the current value. Evaluates both spatial
bit-distance and affine phase-distance, returning the larger magnitude.
*/
func (value Value) TransitionMagnitude(
	predecessor Value,
) (numeric.Phase, error) {
	selfPhase := numeric.Phase(
		numeric.MersenneReduce64(value.ResidualCarry()),
	)

	predecessorPhase := numeric.Phase(
		numeric.MersenneReduce64(predecessor.ResidualCarry()),
	)

	delta, _ := value.XOR(predecessor)
	coreMagnitude := numeric.Phase(delta.CoreActiveCount())

	if selfPhase == 0 || predecessorPhase == 0 {
		return coreMagnitude, nil
	}

	predInv, err := calc.Inverse(predecessorPhase)
	if err != nil {
		return coreMagnitude, err
	}

	phaseQuotient := calc.Multiply(selfPhase, predInv)
	phaseMagnitude := discreteLog(phaseQuotient)

	if coreMagnitude > phaseMagnitude {
		return coreMagnitude, nil
	}

	return phaseMagnitude, nil
}

/*
ComputeOperator derives and stores the GF(8191) multiplier
required to map the predecessor phase to the successor phase,
updating the local guard radius.
*/
func (value *Value) ComputeOperator(
	predecessor Value, successorPhase numeric.Phase,
) {
	state := errnie.NewState("primitive/operation/computeOperator")

	predecessorPhase := numeric.Phase(
		numeric.MersenneReduce64(predecessor.ResidualCarry()),
	)

	if predecessorPhase == 0 || successorPhase == 0 {
		return
	}

	predInv := errnie.Guard(state, func() (numeric.Phase, error) {
		return calc.Inverse(predecessorPhase)
	})

	multiplier := calc.Multiply(successorPhase, predInv)

	if multiplier == 0 {
		multiplier = 1
	}

	value.SetStatePhase(successorPhase)
	value.SetAffine(multiplier, 0)
	value.SetTrajectory(predecessorPhase, successorPhase)

	magnitude := errnie.Guard(state, func() (numeric.Phase, error) {
		return value.TransitionMagnitude(predecessor)
	})

	value.SetGuardRadius(uint8(magnitude % 256))
}

/*
ExecuteTrace processes a sequence of values by applying their affine
operators sequentially. Halts if the transition magnitude exceeds the
maximum allowed discontinuity, or if a halt opcode is read. Returns the
execution trace of phases and the index at which execution halted.
*/
func ExecuteTrace(
	path []Value, seedPhase numeric.Phase, maxDiscontinuity numeric.Phase,
) ([]numeric.Phase, int) {
	trace := make([]numeric.Phase, 0, len(path))
	currentPhase := seedPhase

	for idx, node := range path {
		nextPhase, halt := node.ApplyAffine(currentPhase)

		if halt {
			trace = append(trace, nextPhase)
			return trace, idx
		}

		if idx > 0 {
			magnitude, err := node.TransitionMagnitude(path[idx-1])
			if err != nil {
				continue
			}

			if maxDiscontinuity > 0 && magnitude > maxDiscontinuity {
				return trace, idx
			}
		}

		trace = append(trace, nextPhase)
		currentPhase = nextPhase
	}

	return trace, len(path)
}

/*
BuildQueryMask constructs a composite search state from known structural components.
Accumulates the physical bitwise OR mask and the composed inverse scalar phase.
*/
func BuildQueryMask(knownValues ...Value) Value {
	queryMask, err := New()
	if err != nil {
		panic("BuildQueryMask: " + err.Error())
	}

	if err := BuildQueryMaskInto(&queryMask, knownValues...); err != nil {
		panic("BuildQueryMaskInto: " + err.Error())
	}

	return queryMask
}

/*
BuildQueryMaskInto writes a composite search state into caller-owned storage.
*/
func BuildQueryMaskInto(destination *Value, knownValues ...Value) error {
	if destination == nil || !destination.IsValid() {
		return fmt.Errorf("primitive: query mask destination is invalid")
	}

	composedInversePhase := numeric.Phase(1)

	for i := range config.TotalBlocks {
		destination.setBlock(i, 0)
	}

	merged, err := New()
	if err != nil {
		return err
	}

	for _, known := range knownValues {
		if known.ActiveCount() == 0 {
			continue
		}

		if err := destination.ORInto(known, &merged); err != nil {
			continue
		}

		destination.CopyFrom(merged)

		phase := numeric.Phase(numeric.MersenneReduce64(known.ResidualCarry()))

		if phase == 0 {
			continue
		}

		inv, err := calc.Inverse(phase)
		if err != nil {
			continue
		}

		composedInversePhase = calc.Multiply(composedInversePhase, inv)
	}

	destination.SetAffine(composedInversePhase, 0)
	destination.SetStatePhase(composedInversePhase)

	return nil
}

/*
BatchEvaluate applies the query mask against a slice of candidate values.
Returns an array of match metrics for downstream sorting or filtering.
*/
func BatchEvaluate(queryMask Value, candidates []Value) []MatchResult {
	results := make([]MatchResult, 0, len(candidates))

	for idx := range candidates {
		result := queryMask.EvaluateMatch(candidates[idx])
		results = append(results, result)
	}

	return results
}

/*
BatchEvaluateInto applies the query mask into caller-owned MatchResult buffers.
The results slice must already contain one allocated Residue per candidate.
*/
func BatchEvaluateInto(
	queryMask Value,
	candidates []Value,
	results []MatchResult,
) ([]MatchResult, error) {
	if len(results) < len(candidates) {
		return nil, fmt.Errorf("primitive: results buffer too small")
	}

	for index := range candidates {
		if err := queryMask.EvaluateMatchInto(candidates[index], &results[index]); err != nil {
			return nil, err
		}
	}

	return results[:len(candidates)], nil
}

/*
buildDiscreteLogTable precomputes the discrete logarithm table for the
primitive root of GF(8191), reducing log lookups to O(1). The table maps
every non-zero field element to its discrete log base g (the primitive root).
*/
func buildDiscreteLogTable() []numeric.Phase {
	table := make([]numeric.Phase, numeric.FieldPrime)
	power := numeric.Phase(1)

	for k := numeric.Phase(0); k < numeric.Phase(numeric.FieldPrime)-1; k++ {
		table[power] = k

		power = numeric.Phase(
			numeric.MersenneReduce64(uint64(power) * uint64(numeric.FieldPrimitive)),
		)
	}

	return table
}

/*
discreteLog returns k such that g^k ≡ phase (mod 8191) via O(1) table lookup.
*/
func discreteLog(phase numeric.Phase) numeric.Phase {
	if phase == 0 || phase >= numeric.Phase(numeric.FieldPrime) {
		return 0
	}

	return discreteLogTable[phase]
}
