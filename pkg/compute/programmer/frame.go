package programmer

import (
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
FrameContract identifies the lowered execution family the compiler selected for
one frame. Kernels should eventually dispatch on lowered contract shape rather
than infer semantics from one universal sweep path.
*/
type FrameContract uint8

const (
	ContractUnknown FrameContract = iota
	ContractExactBinary
	ContractSweepSignal
	ContractReduceBinary
	ContractGeometric
)

/*
Frame is one compiled chunk sized to fit a single Value program region.

Program holds the bits written into value.region.program. Scheduling metadata
(word 117) is applied by WriteFrames / Installer after program words are laid down.
*/
type Frame struct {
	Contract FrameContract
	Program  [64]uint64
}

/*
WriteIntoProgramRegion copies Program words into the Value program region
(value.region.program start and bit span from config).
*/
func (frame *Frame) WriteIntoProgramRegion(value *primitive.Value) {
	if frame == nil || value == nil {
		return
	}

	start := core.Cfg.Value.Region.Program.Start
	bits := core.Cfg.Value.Region.Program.Bits
	nWords := int((bits + 63) / 64)

	for wordIdx := 0; wordIdx < nWords && wordIdx < len(frame.Program); wordIdx++ {
		value.Set(start+wordIdx, frame.Program[wordIdx])
	}
}
