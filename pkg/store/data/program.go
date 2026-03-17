package data

import "github.com/theapemachine/six/pkg/numeric"

/*
SequenceCell is the native value-plane compilation product for one radix cell.
The Morton key names the lexical address (symbol + boundary-local depth); the
Value stores the local program that was observed at that address.
*/
type SequenceCell struct {
	Position   uint32
	Symbol     byte
	NextSymbol byte
	Value      Value
	Meta       Value
}

/*
CompileSequenceCells turns a stream of tokenizer keys into native program cells.

The address plane already carries lexical identity as (symbol << 32) | localDepth,
so the compiled Value deliberately avoids storing lexical seed bits. Instead each
cell becomes a tiny local operator containing:

  - the cumulative GF(257) state after consuming the symbol,
  - the affine transition implied by the next symbol,
  - a trajectory snapshot from current state to next state,
  - a threaded-code program opcode (next / reset / jump / halt).

Boundary-local depth resets are interpreted as OpcodeReset. Ordinary forward
movement is OpcodeNext unless the depth jumps by more than one cell, in which
case OpcodeJump preserves the observed stride.
*/
func CompileSequenceCells(keys []uint64) []SequenceCell {
	if len(keys) == 0 {
		return nil
	}

	coder := NewMortonCoder()
	calc := numeric.NewCalculus()
	phase := numeric.Phase(1)
	cells := make([]SequenceCell, 0, len(keys))

	for index, key := range keys {
		position, symbol := coder.Unpack(key)
		phase = calc.Multiply(phase, PhaseScaleForByte(symbol))

		value := NeutralValue()
		value.SetStatePhase(phase)

		meta := MustNewValue()
		meta.SetStatePhase(phase)

		nextSymbol := byte(0)
		nextPos := uint32(0)
		hasNext := index+1 < len(keys)
		if hasNext {
			nextPos, nextSymbol = coder.Unpack(keys[index+1])
			value.SetLexicalTransition(nextSymbol)
			meta.SetAffine(PhaseScaleForByte(nextSymbol), 0)
			meta.SetRouteHint(RouteHintForSymbol(nextSymbol))

			if from, to, ok := value.Trajectory(); ok {
				meta.SetTrajectory(from, to)
			}
		}

		opcode, jump, branches, terminal := programForStep(position, nextPos, hasNext)
		value.SetProgram(opcode, jump, branches, terminal)
		meta.SetProgram(opcode, jump, branches, terminal)

		cells = append(cells, SequenceCell{
			Position:   position,
			Symbol:     symbol,
			NextSymbol: nextSymbol,
			Value:      value,
			Meta:       meta,
		})
	}

	return cells
}

/*
CompileObservableSequenceValues projects compiled native cells
back into the lexical plane for query-time transport. This keeps
prompt handling human-facing and decodable while storage stays
native and lexical-free.
*/
func CompileObservableSequenceValues(keys []uint64) []Value {
	cells := CompileSequenceCells(keys)
	values := make([]Value, 0, len(cells))

	for _, cell := range cells {
		values = append(
			values,
			SeedObservable(cell.Symbol, cell.Value),
		)
	}

	return values
}

/*
programForStep chooses the next Opcode and parameters for advancing the
program based on the current and next positions. It centralizes the logic for
determining whether the execution should halt, reset, continue to the next
cell, or jump with a specific stride.
*/
func programForStep(
	currentPos, nextPos uint32, hasNext bool,
) (Opcode, uint32, uint8, bool) {
	if !hasNext {
		return OpcodeHalt, 0, 0, true
	}

	if nextPos == 0 {
		return OpcodeReset, 0, 1, false
	}

	if nextPos <= currentPos {
		jump := currentPos - nextPos
		return OpcodeJump, jump, 0, false
	}

	jump := nextPos - currentPos

	if jump == 1 {
		return OpcodeNext, 1, 0, false
	}

	return OpcodeJump, jump, 0, false
}
