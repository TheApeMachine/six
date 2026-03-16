package lsm

import (
	"sort"

	"github.com/theapemachine/six/pkg/numeric"
)

func (wf *Wavefront) newHeadRegisters() *executionRegisters {
	registers := newExecutionRegisters()
	if wf != nil && wf.checkpointTrailLimit > 0 {
		registers.trailLimit = wf.checkpointTrailLimit
	}
	return registers
}

type wavefrontStateKey struct {
	phase        numeric.Phase
	alignedPhase numeric.Phase
	queryPhase   numeric.Phase
	pos          uint32
	segment      uint32
	promptIdx    int
}

type wavefrontFrontierKey struct {
	pos       uint32
	segment   uint32
	promptIdx int
}

func (wf *Wavefront) headStateKey(head *WavefrontHead) wavefrontStateKey {
	if head == nil {
		return wavefrontStateKey{}
	}
	return wavefrontStateKey{
		phase:        head.phase,
		alignedPhase: head.alignedPhase,
		queryPhase:   head.queryPhase,
		pos:          head.pos,
		segment:      head.segment,
		promptIdx:    head.promptIdx,
	}
}

func (wf *Wavefront) headFrontierKey(head *WavefrontHead) wavefrontFrontierKey {
	if head == nil {
		return wavefrontFrontierKey{}
	}
	return wavefrontFrontierKey{pos: head.pos, segment: head.segment, promptIdx: head.promptIdx}
}

func (wf *Wavefront) hygieneScore(head *WavefrontHead) int {
	if head == nil {
		return 1 << 30
	}

	score := head.energy + int(head.stalls)*6 + int(head.frustration)*5
	score += (len(head.path) - len(head.metaPath)) * 2
	if head.registers != nil {
		score += int(head.registers.WorseningStreak()) * 3
		bestResidue := head.registers.BestResidue()
		if bestResidue != 0xFF {
			score += int(bestResidue) / 2
		}
	}
	return score
}

func (wf *Wavefront) betterHead(left, right *WavefrontHead, promptMode bool) bool {
	if right == nil {
		return true
	}
	if left == nil {
		return false
	}

	if promptMode && left.promptIdx != right.promptIdx {
		return left.promptIdx > right.promptIdx
	}
	if left.fuzzyErrs != right.fuzzyErrs {
		return left.fuzzyErrs < right.fuzzyErrs
	}
	leftScore := wf.hygieneScore(left)
	rightScore := wf.hygieneScore(right)
	if leftScore != rightScore {
		return leftScore < rightScore
	}
	if left.stalls != right.stalls {
		return left.stalls < right.stalls
	}
	if left.frustration != right.frustration {
		return left.frustration < right.frustration
	}
	if left.segment != right.segment {
		return left.segment < right.segment
	}
	if left.pos != right.pos {
		return left.pos < right.pos
	}
	if left.age != right.age {
		return left.age < right.age
	}
	return len(left.path) <= len(right.path)
}

func (wf *Wavefront) shouldExpireHead(head *WavefrontHead) bool {
	if head == nil {
		return true
	}
	if len(head.path) == 0 {
		return true
	}
	if wf.headGraceStalls > 0 && int(head.stalls) > wf.headGraceStalls {
		return true
	}
	if head.registers != nil && wf.headGraceStalls > 0 && int(head.registers.WorseningStreak()) > wf.headGraceStalls+2 && head.frustration > 0 {
		return true
	}
	return false
}

func (wf *Wavefront) compactHeads(heads []*WavefrontHead, promptMode bool) []*WavefrontHead {
	if len(heads) == 0 {
		return nil
	}

	fallback := make([]*WavefrontHead, 0, len(heads))
	stateBest := make(map[wavefrontStateKey]*WavefrontHead, len(heads))
	for _, head := range heads {
		if head == nil {
			continue
		}
		fallback = append(fallback, head)
		if head.registers != nil {
			head.registers.GarbageCollect(head, wf.checkpointTrailLimit, wf.checkpointWindow)
		}
		if wf.shouldExpireHead(head) {
			continue
		}
		key := wf.headStateKey(head)
		if existing, ok := stateBest[key]; !ok || wf.betterHead(head, existing, promptMode) {
			stateBest[key] = head
		}
	}

	compacted := make([]*WavefrontHead, 0, len(stateBest))
	for _, head := range stateBest {
		compacted = append(compacted, head)
	}

	if len(compacted) == 0 {
		compacted = append(compacted, fallback...)
	}

	sort.Slice(compacted, func(i, j int) bool {
		return wf.betterHead(compacted[i], compacted[j], promptMode)
	})

	if wf.headFrontierFanout > 0 && len(compacted) > 1 {
		filtered := make([]*WavefrontHead, 0, len(compacted))
		frontierCounts := make(map[wavefrontFrontierKey]int)
		for _, head := range compacted {
			key := wf.headFrontierKey(head)
			if frontierCounts[key] >= wf.headFrontierFanout {
				continue
			}
			filtered = append(filtered, head)
			frontierCounts[key]++
		}
		if len(filtered) > 0 {
			compacted = filtered
		}
	}

	return compacted
}
