package pysix

import (
	"github.com/theapemachine/six/pkg/compute/stepwise"
)

/*
Run executes a compiled program on frame using stepwise.RunScalar. The frame
must be zero-initialized; prologue instructions set slotZero and slotOnes.
*/
func Run(frame *[stepwise.FrameWords]uint64, prog []uint64) error {

	return stepwise.RunScalar(frame, prog)
}
