package lsm

import (
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
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
	if last.Terminal() || last.Opcode() == uint64(data.OpcodeHalt) {
		return 0, head.segment, false
	}

	if data.Opcode(last.Opcode()) == data.OpcodeReset {
		return 0, head.segment + 1, true
	}

	if jump := last.Jump(); jump > 0 {
		return head.pos + jump, head.segment, true
	}

	return head.pos + 1, head.segment, true
}

func (wf *Wavefront) predictNextPhase(head *WavefrontHead, nextSymbol byte) numeric.Phase {
	if head == nil {
		return wf.advancePromptPhase(1, nextSymbol)
	}
	if len(head.path) > 0 {
		last := head.path[len(head.path)-1]
		if last.HasAffine() {
			if next := last.ApplyAffinePhase(head.phase); next != 0 {
				return next
			}
		}
	}

	return wf.advancePromptPhase(head.phase, nextSymbol)
}

func visitFor(key uint64, segment uint32) visitMark {
	return visitMark{key: key, segment: segment}
}
