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

func (wf *Wavefront) resolveTransition(
	head *WavefrontHead,
	nextPos uint32,
	nextSymbol byte,
	stateValue data.Value,
	expected numeric.Phase,
) (numeric.Phase, int, bool, bool) {
	storedPhase, ok := extractStatePhase(stateValue, nextSymbol)
	if !ok {
		return 0, 0, false, false
	}

	resolved := expected
	penalty := 0
	anchored := false

	if snapped, anchorPenalty, ok := wf.anchorCorrect(nextPos, expected, stateValue); ok {
		resolved = snapped
		penalty += anchorPenalty
		anchored = true
	} else if wf.anchorViolates(nextPos, expected, stateValue) {
		return 0, 0, false, false
	}

	if head != nil && len(head.path) > 0 {
		prev := head.path[len(head.path)-1]
		penalty += operatorRoutePenalty(prev, nextSymbol)

		accepted, guardPenalty, ok := operatorPhaseAcceptance(prev, resolved, storedPhase)
		if !ok {
			return 0, 0, anchored, false
		}

		resolved = accepted
		penalty += guardPenalty
		return resolved, penalty, anchored, true
	}

	if storedPhase != resolved {
		return 0, 0, anchored, false
	}

	return resolved, penalty, anchored, true
}

func visitFor(key uint64, segment uint32) visitMark {
	return visitMark{key: key, segment: segment}
}
