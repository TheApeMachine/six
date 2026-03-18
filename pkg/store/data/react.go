package data

import (
	"github.com/theapemachine/six/pkg/numeric"
)

/*
Reaction is the product of two Values meeting. It captures the
algebraic relationship between the reactants without any external
control flow deciding what to do with it.
*/
type Reaction struct {
	Product   Value
	Resonance int
	Distance  numeric.Phase
}

/*
React computes the algebraic product of two Values meeting.
Resonance is the shared structure (popcount of AND).
Product is the residue after cancellation (XOR).
Distance is the GF(257) affine distance between their phases.
No code decides what to do with this. The algebra speaks.
*/
func (value *Value) React(other Value) Reaction {
	calc := numeric.NewCalculus()

	selfPhase := numeric.Phase(value.ResidualCarry() % uint64(numeric.FermatPrime))
	otherPhase := numeric.Phase(other.ResidualCarry() % uint64(numeric.FermatPrime))

	var distance numeric.Phase

	if selfPhase > 0 && otherPhase > 0 {
		selfInv, err := calc.Inverse(selfPhase)

		if err == nil {
			distance = calc.Multiply(otherPhase, selfInv)
		}
	}

	return Reaction{
		Product:   value.XOR(other),
		Resonance: value.Similarity(other),
		Distance:  distance,
	}
}

/*
Catalyze applies the Value's own stored affine operator to an incoming phase.
The Value acts as a tiny CPU: input goes in, the affine transforms it, output
comes out. Halt is determined by the Value's own opcode, not by external code.
*/
func (value *Value) Catalyze(incoming numeric.Phase) (numeric.Phase, bool) {
	outgoing := value.ApplyAffinePhase(incoming)
	opcode := Opcode(value.Opcode())

	return outgoing, opcode == OpcodeHalt
}

/*
BoundaryStrength computes the magnitude of the phase discontinuity between
this Value and its predecessor's phase. The strength is the discrete log
of the affine distance in GF(257): how many primitive root steps separate
the two phases.

Smooth continuation → small strength.
Sentence boundary → large strength.
The Value discovers its own boundary. No code tells it.
*/
func (value *Value) BoundaryStrength(predecessorPhase numeric.Phase) numeric.Phase {
	calc := numeric.NewCalculus()

	selfPhase := numeric.Phase(value.ResidualCarry() % uint64(numeric.FermatPrime))

	if selfPhase == 0 || predecessorPhase == 0 {
		return 0
	}

	predInv, err := calc.Inverse(predecessorPhase)

	if err != nil {
		return 0
	}

	distance := calc.Multiply(selfPhase, predInv)

	return discreteLog(distance)
}

/*
Derive programs a Value from its context. The Value examines its
neighbors and computes its own affine operator — the rotation that
maps predecessorPhase to successorPhase THROUGH this Value.

This is tool synthesis at the Value level. The Value doesn't need
CompileSequenceCells to tell it what operator to carry. It derives
the operator from the data it sits between.
*/
func (value *Value) Derive(predecessorPhase, successorPhase numeric.Phase) {
	calc := numeric.NewCalculus()

	if predecessorPhase == 0 || successorPhase == 0 {
		return
	}

	predInv, err := calc.Inverse(predecessorPhase)

	if err != nil {
		return
	}

	tool := calc.Multiply(successorPhase, predInv)

	if tool == 0 {
		tool = 1
	}

	value.SetAffine(tool, 0)
	value.SetTrajectory(predecessorPhase, successorPhase)

	strength := value.BoundaryStrength(predecessorPhase)
	value.SetGuardRadius(uint8(strength % 256))
}

/*
Propagate executes a chain of Values by passing an incoming phase through
each Value's affine operator in sequence. Execution halts when a Value's
boundary strength exceeds maxBoundary, or when a Value carries OpcodeHalt.

The chain IS the program. The phases ARE the execution trace.
Returns the collected phases (the output) and the halt index.
*/
func Propagate(chain []Value, seed numeric.Phase, maxBoundary numeric.Phase) ([]numeric.Phase, int) {
	trace := make([]numeric.Phase, 0, len(chain))
	phase := seed

	for idx, cell := range chain {
		outgoing, halt := cell.Catalyze(phase)

		if halt {
			trace = append(trace, outgoing)
			return trace, idx
		}

		if idx > 0 {
			strength := cell.BoundaryStrength(phase)

			if maxBoundary > 0 && strength > maxBoundary {
				return trace, idx
			}
		}

		trace = append(trace, outgoing)
		phase = outgoing
	}

	return trace, len(chain)
}

/*
Reagent synthesizes a query Value from known components using algebraic
cancellation. Given the phases to cancel, it produces a single Value whose
affine operator IS the inverse of the known structure.

Drop the reagent into a vat of Values. The only things that survive
the reaction are the unknowns.
*/
func Reagent(knownPhases ...numeric.Phase) Value {
	calc := numeric.NewCalculus()

	composedInverse := numeric.Phase(1)

	for _, phase := range knownPhases {
		if phase == 0 {
			continue
		}

		inv, err := calc.Inverse(phase)

		if err != nil {
			continue
		}

		composedInverse = calc.Multiply(composedInverse, inv)
	}

	reagent := NeutralValue()
	reagent.SetAffine(composedInverse, 0)
	reagent.SetStatePhase(composedInverse)

	return reagent
}

/*
Precipitate drops a reagent into a vat of Values and returns the
reaction products sorted by resonance (highest first). This IS
the query mechanism. No search. No iteration over candidates.
The algebra does the work.

Each Value in the vat reacts with the reagent. High resonance means
the reagent cancelled the Value's known structure, leaving behind
only the unknown — the answer.
*/
func Precipitate(reagent Value, vat []Value) []Reaction {
	reactions := make([]Reaction, 0, len(vat))

	for idx := range vat {
		reaction := reagent.React(vat[idx])
		reactions = append(reactions, reaction)
	}

	return reactions
}

/*
discreteLog computes the discrete logarithm of phase in base 3 (the
primitive root of GF(257)). Returns k such that 3^k ≡ phase (mod 257).
Brute force over 256 elements — this is a tiny field.
*/
func discreteLog(phase numeric.Phase) numeric.Phase {
	if phase == 0 {
		return 0
	}

	power := numeric.Phase(1)

	for k := numeric.Phase(0); k < numeric.Phase(numeric.FermatPrime); k++ {
		if power == phase {
			return k
		}

		power = numeric.Phase((uint32(power) * numeric.FermatPrimitive) % numeric.FermatPrime)
	}

	return 0
}
