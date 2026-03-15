package lsm

import (
	"github.com/theapemachine/six/pkg/numeric"
)

type visitMark struct {
	key     uint64
	segment uint32
}

func (wf *Wavefront) advanceTarget(head *WavefrontHead) (uint32, uint32, bool) {
	if head == nil {
		return 0, 0, false
	}
	if len(head.path) == 0 {
		return 0, head.segment, false
	}

	last := head.path[len(head.path)-1]
	return advanceProgramCursor(head.pos, head.segment, last)
}

func (wf *Wavefront) predictNextPhase(head *WavefrontHead, nextSymbol byte) numeric.Phase {
	if head == nil {
		return wf.advancePromptPhase(1, nextSymbol)
	}
	if len(head.path) > 0 {
		return predictNextPhaseFromValue(wf.calc, head.path[len(head.path)-1], head.phase, nextSymbol)
	}

	return wf.advancePromptPhase(head.phase, nextSymbol)
}

func visitFor(key uint64, segment uint32) visitMark {
	return visitMark{key: key, segment: segment}
}
