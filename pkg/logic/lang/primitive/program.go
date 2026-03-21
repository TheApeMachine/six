package primitive

import (
	"math/bits"

	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
	config "github.com/theapemachine/six/pkg/system/core"
)

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
ValuePrimeIndices returns the prime indices (0..NBasis-1) that are set in the value.
Used for debug output (which primes were assigned).
*/
func ValuePrimeIndices(value *Value) []int {
	var out []int

	for i := range config.ValueBlocks {
		block := value.Block(i)

		for block != 0 {
			bitIdx := bits.TrailingZeros64(block)
			primeIdx := i*64 + bitIdx

			if primeIdx < config.NBasis {
				out = append(out, primeIdx)
			}

			block &= block - 1
		}
	}

	return out
}

/*
CompileSequenceCells turns a stream of tokenizer keys into native program cells.

The address plane already carries lexical identity as (symbol << 32) | localDepth,
so the compiled Value deliberately avoids storing lexical seed bits. Instead each
cell becomes a tiny local operator containing:

  - the cumulative GF(8191) state after consuming the symbol,
  - the affine transition implied by the next symbol,
  - a trajectory snapshot from current state to next state,
  - a threaded-code program opcode (next / reset / jump / halt).

Boundary-local depth resets are interpreted as OpcodeReset. Ordinary forward
movement is OpcodeNext unless the depth jumps by more than one cell, in which
case OpcodeJump preserves the observed stride.
*/
func CompileSequenceCells(keys []uint64) []SequenceCell {
	state := errnie.NewState("logic/lang/primitive/program/compileSequenceCells")

	if len(keys) == 0 {
		return nil
	}

	coder := data.NewMortonCoder()
	calc := numeric.NewCalculus()
	phase := numeric.Phase(1)
	cells := make([]SequenceCell, 0, len(keys))

	for index, key := range keys {
		position, symbol := coder.Unpack(key)
		phase = calc.Multiply(phase, PhaseScaleForByte(symbol))

		value := NeutralValue()
		value.SetStatePhase(phase)

		meta := errnie.Guard(state, func() (Value, error) {
			return New()
		})
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

/*
PhaseScaleForByte returns the multiplicative GF(8191) transition induced by a
byte. It is the native phase operator behind the lexical update rule
state' = state * g^byte (mod numeric.FieldPrime) for field primitive g, but
factored out so stored values can carry that operator directly without
redundantly storing the byte seed.
*/
func PhaseScaleForByte(symbol byte) numeric.Phase {
	return bytePhaseScales[int(symbol)]
}

/*
SetLexicalTransition installs the native operator that would be induced by the
next byte under the original lexical phase rule. This preserves compatibility
with existing prompt/state logic while keeping the stored value lexical-free.

When the current state snapshot is already available in ResidualCarry, the value
also records a trajectory snapshot and a compact route hint in the shell so the
operator can steer traversal without replaying lexical identity from storage.
*/
func (value *Value) SetLexicalTransition(next byte) {
	value.SetAffine(PhaseScaleForByte(next), 0)
	value.SetRouteHint(RouteHintForSymbol(next))

	if carry := value.ResidualCarry(); carry > 0 {
		current := numeric.Phase(numeric.MersenneReduce64(carry))

		if current > 0 {
			value.SetTrajectory(current, value.ApplyAffinePhase(current))
		}
	}
}

/*
RouteHintForSymbol returns a compact routing class for a byte without storing
the byte itself in the persistent value. It is derived from the byte's sparse
lexical seed and only used as an execution hint inside the shell.
*/
func RouteHintForSymbol(symbol byte) uint8 {
	return byteRouteHints[int(symbol)]
}

/*
SeedObservable projects a lexical seed onto an otherwise native value so
prompts and human-facing outputs remain decodable without forcing storage to
carry the byte twice.
*/
func SeedObservable(symbol byte, value Value) Value {
	return ObservableValue(symbol, value)
}

/*
ObservableValue projects a stored native value back into the lexical plane for
measurement and human-facing decode. The key already names the byte; this helper
simply re-injects the byte's transient five-bit seed without disturbing the
stored phase or control shell.
*/
func ObservableValue(symbol byte, value Value) Value {
	state := errnie.NewState("logic/lang/primitive/program/observableValue")

	out := errnie.Guard(state, func() (Value, error) {
		return New()
	})
	out.CopyFrom(value)

	for _, off := range baseValueOffsets(symbol) {
		out.Set(off)
	}

	return out
}

/*
InferLexicalSeed returns the unique lexical seed projected onto an observable
value. Native lexical-free values return ok=false.
*/
func InferLexicalSeed(value Value) (byte, bool) {
	var symbol byte
	matches := 0

	for candidate := 0; candidate < 256; candidate++ {
		if !HasLexicalSeed(value, byte(candidate)) {
			continue
		}

		symbol = byte(candidate)
		matches++

		if matches > 1 {
			return 0, false
		}
	}

	return symbol, matches == 1
}

/*
HasLexicalSeed reports whether the value visibly contains the five-bit
lexical seed for the supplied byte. Query observables should usually return
true; canonical stored values should usually return false.
*/
func HasLexicalSeed(value Value, b byte) bool {
	offsets := baseValueOffsets(b)

	for _, offset := range offsets {
		if !value.Has(offset) {
			return false
		}
	}

	return true
}

/*
ShannonDensity returns the fraction of the config.CoreBits logical core bits that are active.
The Sequencer uses this to force a boundary before the value saturates.
Above ~0.40 (roughly 3276 of the 8191 core bits) the core field loses discriminative power. Shell bits do
not count toward this threshold because they live in the hardware jacket, not in
the field execution plane itself.
*/
func (value Value) ShannonDensity() float64 {
	return float64(value.CoreActiveCount()) / float64(config.CoreBits)
}
