package kernel

import (
	"github.com/theapemachine/six/pkg/compute/program"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/primitive"
)

type Strategy uint

const (
	StrategyExact Strategy = iota
	StrategyRotate
)

/*
Optimizer converts a Value to a compute Frame, which is an
efficient alignment of the data and opcodes to maximize the
hardware sympathy of the target backend.
*/
type Optimizer struct {
	A        [][]uint64
	B        [][]uint64
	DST      [][]uint64
	OP       []uint64
	MODE     []uint64
	IMM      []uint64
	RETURN   [][]uint64
	strategy Strategy
	value    *primitive.Value
}

/*
NewOptimizer creates a new Optimizer.
*/
func NewOptimizer(
	value *primitive.Value, strategy Strategy,
) *Optimizer {
	return &Optimizer{
		A:        make([][]uint64, 16),
		B:        make([][]uint64, 16),
		DST:      make([][]uint64, 16),
		OP:       make([]uint64, 16),
		MODE:     make([]uint64, 16),
		IMM:      make([]uint64, 16),
		RETURN:   make([][]uint64, 16),
		value:    value,
		strategy: strategy,
	}
}

/*
rotateBits rotates a slice of uint64 words by the given number of bits to the left,
treating the entire slice as a single contiguous little-endian bitstring.
*/
func rotateBits(words []uint64, shiftBits int) []uint64 {
	n := len(words)
	if n == 0 {
		return nil
	}

	out := make([]uint64, n)
	wordShift := (shiftBits / 64) % n
	bitShift := uint(shiftBits % 64)

	for i := range n {
		src1Idx := (i - wordShift + n) % n
		src2Idx := (i - wordShift - 1 + n) % n

		if bitShift == 0 {
			out[i] = words[src1Idx]
		} else {
			out[i] = (words[src1Idx] << bitShift) | (words[src2Idx] >> (64 - bitShift))
		}
	}
	return out
}

/*
Frame the program according to the strategy.
*/
func (optimizer *Optimizer) Frame() *Optimizer {
	if optimizer.value == nil {
		return optimizer
	}

	progRegion := optimizer.value.Get(primitive.ProgramRegion)
	fullFrame := (*[128]uint64)(optimizer.value)[:]

	for i := 0; i < len(progRegion) && i < 16; i++ {
		instr := progRegion[i]

		if instr == 0 {
			break // End of program
		}

		// Decode the instruction to get the addresses
		aStart, aSpan, bStart, bSpan, dstStart, dstSpan, op, mode, imm := program.DecodeInstruction(instr)
		optimizer.RETURN[i] = make([]uint64, dstSpan)

		optimizer.OP[i] = op
		optimizer.MODE[i] = mode
		optimizer.IMM[i] = imm

		// Extract the actual data slices from the Value frame
		optimizer.A[i] = fullFrame[aStart : aStart+aSpan]
		optimizer.DST[i] = fullFrame[dstStart : dstStart+dstSpan]

		bSlice := fullFrame[bStart : bStart+bSpan]

		switch optimizer.strategy {
		case StrategyExact:
			// Exact strategy: just 1 rotation (the original)
			optimizer.B[i] = bSlice
		case StrategyRotate:
			// Generate 16 rotations of B (shifted by 8 bits each step)
			optimizer.B[i] = rotateBits(bSlice, 8)
		}
	}

	return optimizer
}

/*
Value returns the value associated with the optimizer.
It also resolves the RETURN slice back to the Value frame.
*/
func (optimizer *Optimizer) Value() *primitive.Value {
	if optimizer.value == nil {
		return nil
	}

	switch optimizer.strategy {
	case StrategyExact:
		return optimizer.value
	case StrategyRotate:
		signalsStart := core.Cfg.Value.Region.Signals.Start

		var maxLen int

		for _, ret := range optimizer.RETURN {
			if len(ret) == 0 {
				continue
			}

			_, length := geometry.ScanOneRun(ret)

			if length > maxLen {
				maxLen = length
			}
		}

		optimizer.value.Set(signalsStart, uint64(maxLen))
		return optimizer.value
	}

	return optimizer.value
}
