package stepwise

import (
	"errors"

	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
)

/*
RunHomogeneousBatch runs the same program across many frames. Operands must be
loaded from each frame’s A slice only (leftFromB and rightFromB must be false);
pairwise batches should run RunEmbeddedPair per job from compute.Backend.
*/
func RunHomogeneousBatch(contexts []*[FrameWords]uint64, program []uint64) error {

	if len(contexts) == 0 {
		return nil
	}

	batch := len(contexts)
	bufA := make([]uint64, batch)
	bufD := make([]uint64, batch)

	for step := range program {
		op, idxA, idxB, idxDst, leftFromB, rightFromB, decodeErr := DecodeStep(program[step])
		if decodeErr != nil {
			return decodeErr
		}

		if leftFromB || rightFromB {
			return errors.New(
				"stepwise.RunHomogeneousBatch: partner frame flags not supported",
			)
		}

		if op > 0xF {
			return errors.New(
				"stepwise.RunHomogeneousBatch: opcode > 0xF not SIMD-batched here",
			)
		}

		for lane := 0; lane < batch; lane++ {
			bufA[lane] = contexts[lane][idxA]
			bufD[lane] = contexts[lane][idxB]
		}

		cpu.ExecWordBlock(bufD, bufA, op)

		for lane := 0; lane < batch; lane++ {
			contexts[lane][idxDst] = bufD[lane]
		}
	}

	return nil
}
