package lsm

import (
	"math/bits"
	"sort"

	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

const (
	execRegisterRows       = 257
	execRegisterWords      = 5
	execRegisterTrailLimit = 8
)

type executionRegisterRow [execRegisterWords]uint64

type executionCheckpointReason uint8

const (
	checkpointReasonSeed executionCheckpointReason = iota + 1
	checkpointReasonCarry
	checkpointReasonStable
	checkpointReasonReset
	checkpointReasonAnchor
	checkpointReasonSkip
	checkpointReasonBridge
	checkpointReasonBacktrack
	checkpointReasonFrustration
	checkpointReasonFrustrationFork
	checkpointReasonTerminal
)

type executionCheckpoint struct {
	pos          uint32
	segment      uint32
	phase        numeric.Phase
	alignedPhase numeric.Phase
	queryPhase   numeric.Phase
	promptIdx    int
	energy       int
	pathLen      int
	metaLen      int
	age          uint16
	stalls       uint8
	visited      map[visitMark]bool
	reason       executionCheckpointReason
}

type executionRegisters struct {
	residues        [execRegisterRows]executionRegisterRow
	alignments      [execRegisterRows]executionRegisterRow
	checkpoints     [execRegisterRows]executionRegisterRow
	trail           []executionCheckpoint
	trailLimit      int
	lastResidue     uint8
	bestResidue     uint8
	worseningStreak uint8
}

func newExecutionRegisters() *executionRegisters {
	return &executionRegisters{
		trailLimit:  execRegisterTrailLimit,
		bestResidue: 0xFF,
	}
}

func cloneExecutionRegisters(registers *executionRegisters) *executionRegisters {
	if registers == nil {
		return nil
	}

	cloned := *registers
	if len(registers.trail) > 0 {
		cloned.trail = make([]executionCheckpoint, len(registers.trail))
		for i, checkpoint := range registers.trail {
			cloned.trail[i] = checkpoint
			cloned.trail[i].visited = cloneVisitedMap(checkpoint.visited)
		}
	}

	return &cloned
}

func registerPhaseIndex(phase numeric.Phase) int {
	return int(uint32(phase) % execRegisterRows)
}

func registerPositionIndex(pos uint32) int {
	return int(pos % execRegisterRows)
}

func registerSetBit(row *executionRegisterRow, column int) {
	if row == nil {
		return
	}

	column %= execRegisterRows
	if column < 0 {
		column += execRegisterRows
	}

	word := column / 64
	bit := uint(column % 64)
	row[word] |= uint64(1) << bit
}

func registerHasBit(row executionRegisterRow, column int) bool {
	column %= execRegisterRows
	if column < 0 {
		column += execRegisterRows
	}

	word := column / 64
	bit := uint(column % 64)
	return row[word]&(uint64(1)<<bit) != 0
}

func registerRowCount(row executionRegisterRow) int {
	count := 0
	for _, word := range row {
		count += bits.OnesCount64(word)
	}
	return count
}

func (registers *executionRegisters) ObserveTransition(
	sourcePhase, queryPhase, observedPhase numeric.Phase,
) int {
	if registers == nil || observedPhase == 0 {
		return 0
	}

	if sourcePhase == 0 {
		sourcePhase = observedPhase
	}
	if queryPhase == 0 {
		queryPhase = observedPhase
	}

	sourceIdx := registerPhaseIndex(sourcePhase)
	queryIdx := registerPhaseIndex(queryPhase)
	observedIdx := registerPhaseIndex(observedPhase)
	residue := int(phaseDistanceMod257(queryPhase, observedPhase))

	penalty := 0

	if registerHasBit(registers.residues[sourceIdx], residue) {
		if residue > 0 {
			penalty--
		}
	} else if residue > 0 {
		penalty += residue / 4
	}
	registerSetBit(&registers.residues[sourceIdx], residue)

	alignmentCount := registerRowCount(registers.alignments[queryIdx])
	if registerHasBit(registers.alignments[queryIdx], observedIdx) {
		penalty--
	} else if alignmentCount > 0 {
		penalty += alignmentCount / 3
	}
	registerSetBit(&registers.alignments[queryIdx], observedIdx)

	if registers.bestResidue == 0xFF || residue < int(registers.bestResidue) {
		registers.bestResidue = uint8(residue)
		registers.worseningStreak = 0
		penalty -= 2
	} else if residue > int(registers.lastResidue) {
		if registers.worseningStreak < 0xFF {
			registers.worseningStreak++
		}
		penalty += int(registers.worseningStreak)
	} else if residue < int(registers.lastResidue) {
		registers.worseningStreak = 0
		penalty--
	} else if residue == 0 {
		penalty--
	}

	registers.lastResidue = uint8(residue)
	return penalty
}

func (registers *executionRegisters) LastResidue() uint8 {
	if registers == nil {
		return 0xFF
	}
	return registers.lastResidue
}

func (registers *executionRegisters) BestResidue() uint8 {
	if registers == nil {
		return 0xFF
	}
	return registers.bestResidue
}

func (registers *executionRegisters) WorseningStreak() uint8 {
	if registers == nil {
		return 0
	}
	return registers.worseningStreak
}

func (registers *executionRegisters) TagCheckpoint(segment, pos uint32) {
	if registers == nil {
		return
	}

	row := registerPositionIndex(segment)
	col := registerPositionIndex(pos)
	registerSetBit(&registers.checkpoints[row], col)
}

func (registers *executionRegisters) HasCheckpointTag(segment, pos uint32) bool {
	if registers == nil {
		return false
	}

	row := registerPositionIndex(segment)
	col := registerPositionIndex(pos)
	return registerHasBit(registers.checkpoints[row], col)
}

func (registers *executionRegisters) RecordCheckpoint(head *WavefrontHead, reason executionCheckpointReason) {
	if registers == nil || head == nil {
		return
	}

	registers.TagCheckpoint(head.segment, head.pos)

	checkpoint := executionCheckpoint{
		pos:          head.pos,
		segment:      head.segment,
		phase:        head.phase,
		alignedPhase: head.alignedPhase,
		queryPhase:   head.queryPhase,
		promptIdx:    head.promptIdx,
		energy:       head.energy,
		pathLen:      len(head.path),
		metaLen:      len(head.metaPath),
		age:          head.age,
		stalls:       head.stalls,
		visited:      cloneVisitedMap(head.visited),
		reason:       reason,
	}

	if n := len(registers.trail); n > 0 {
		last := registers.trail[n-1]
		if last.pos == checkpoint.pos &&
			last.segment == checkpoint.segment &&
			last.promptIdx == checkpoint.promptIdx &&
			last.pathLen == checkpoint.pathLen &&
			last.reason == checkpoint.reason {
			return
		}
	}

	registers.trail = append(registers.trail, checkpoint)
	if registers.trailLimit > 0 && len(registers.trail) > registers.trailLimit {
		trimmed := make([]executionCheckpoint, registers.trailLimit)
		copy(trimmed, registers.trail[len(registers.trail)-registers.trailLimit:])
		registers.trail = trimmed
	}
}

func checkpointPriority(reason executionCheckpointReason) int {
	switch reason {
	case checkpointReasonStable, checkpointReasonAnchor, checkpointReasonReset, checkpointReasonTerminal:
		return 5
	case checkpointReasonCarry, checkpointReasonSeed:
		return 4
	case checkpointReasonBridge, checkpointReasonSkip, checkpointReasonBacktrack:
		return 3
	case checkpointReasonFrustrationFork:
		return 2
	case checkpointReasonFrustration:
		return 1
	default:
		return 2
	}
}

func (registers *executionRegisters) GarbageCollect(head *WavefrontHead, limit, window int) {
	if registers == nil || len(registers.trail) == 0 {
		return
	}
	if limit <= 0 {
		limit = registers.trailLimit
	}
	if limit <= 0 {
		limit = execRegisterTrailLimit
	}

	maxPath := 0
	maxMeta := 0
	if head != nil {
		maxPath = len(head.path)
		maxMeta = len(head.metaPath)
	}

	type checkpointKey struct {
		pos       uint32
		segment   uint32
		promptIdx int
		pathLen   int
	}
	type scoredCheckpoint struct {
		checkpoint executionCheckpoint
		index      int
		score      int
	}

	bestByKey := make(map[checkpointKey]scoredCheckpoint, len(registers.trail))
	for idx, checkpoint := range registers.trail {
		if maxPath > 0 && (checkpoint.pathLen <= 0 || checkpoint.pathLen > maxPath || checkpoint.metaLen > maxMeta) {
			continue
		}

		distance := 0
		if maxPath > 0 {
			distance = maxPath - checkpoint.pathLen
			if distance < 0 {
				distance = 0
			}
		}

		priority := checkpointPriority(checkpoint.reason)
		if window > 0 && distance > window && priority < 4 {
			continue
		}

		score := priority*1024 - distance*16 - int(checkpoint.stalls)*8
		if maxPath > 0 {
			score -= (head.energy - checkpoint.energy) / 8
		}

		key := checkpointKey{
			pos:       checkpoint.pos,
			segment:   checkpoint.segment,
			promptIdx: checkpoint.promptIdx,
			pathLen:   checkpoint.pathLen,
		}

		existing, exists := bestByKey[key]
		if !exists || score > existing.score || (score == existing.score && idx > existing.index) {
			bestByKey[key] = scoredCheckpoint{checkpoint: checkpoint, index: idx, score: score}
		}
	}

	if len(bestByKey) == 0 {
		registers.trail = nil
		for i := range registers.checkpoints {
			registers.checkpoints[i] = executionRegisterRow{}
		}
		return
	}

	kept := make([]scoredCheckpoint, 0, len(bestByKey))
	for _, checkpoint := range bestByKey {
		kept = append(kept, checkpoint)
	}

	sort.Slice(kept, func(i, j int) bool {
		if kept[i].score != kept[j].score {
			return kept[i].score > kept[j].score
		}
		return kept[i].index > kept[j].index
	})

	if len(kept) > limit {
		kept = kept[:limit]
	}

	sort.Slice(kept, func(i, j int) bool {
		return kept[i].index < kept[j].index
	})

	registers.trail = make([]executionCheckpoint, len(kept))
	for i, checkpoint := range kept {
		registers.trail[i] = checkpoint.checkpoint
	}

	for i := range registers.checkpoints {
		registers.checkpoints[i] = executionRegisterRow{}
	}
	for _, checkpoint := range registers.trail {
		registers.TagCheckpoint(checkpoint.segment, checkpoint.pos)
	}
}

func (registers *executionRegisters) LatestCheckpointBefore(
	head *WavefrontHead,
) (executionCheckpoint, int, bool) {
	if registers == nil || head == nil || len(registers.trail) == 0 {
		return executionCheckpoint{}, -1, false
	}

	for idx := len(registers.trail) - 1; idx >= 0; idx-- {
		checkpoint := registers.trail[idx]
		if checkpoint.reason == checkpointReasonBacktrack || checkpoint.reason == checkpointReasonSkip || checkpoint.reason == checkpointReasonFrustration || checkpoint.reason == checkpointReasonFrustrationFork {
			continue
		}
		if checkpoint.pathLen <= 0 || checkpoint.pathLen > len(head.path) || checkpoint.metaLen > len(head.metaPath) {
			continue
		}
		if checkpoint.pathLen == len(head.path) &&
			checkpoint.pos == head.pos &&
			checkpoint.segment == head.segment &&
			checkpoint.promptIdx == head.promptIdx {
			continue
		}

		return checkpoint, idx, true
	}

	return executionCheckpoint{}, -1, false
}

func (registers *executionRegisters) RecentCheckpointsBefore(
	head *WavefrontHead,
	limit int,
) []executionCheckpoint {
	if registers == nil || head == nil || limit <= 0 || len(registers.trail) == 0 {
		return nil
	}

	out := make([]executionCheckpoint, 0, limit)
	for idx := len(registers.trail) - 1; idx >= 0 && len(out) < limit; idx-- {
		checkpoint := registers.trail[idx]
		if checkpoint.reason == checkpointReasonBacktrack || checkpoint.reason == checkpointReasonSkip || checkpoint.reason == checkpointReasonFrustration || checkpoint.reason == checkpointReasonFrustrationFork {
			continue
		}
		if checkpoint.pathLen <= 0 || checkpoint.pathLen > len(head.path) || checkpoint.metaLen > len(head.metaPath) {
			continue
		}
		if checkpoint.pathLen == len(head.path) &&
			checkpoint.pos == head.pos &&
			checkpoint.segment == head.segment &&
			checkpoint.promptIdx == head.promptIdx {
			continue
		}

		out = append(out, checkpoint)
	}

	return out
}

func rewindHeadFromCheckpoint(head *WavefrontHead, checkpoint executionCheckpoint) *WavefrontHead {
	if head == nil {
		return nil
	}
	if checkpoint.pathLen <= 0 || checkpoint.pathLen > len(head.path) || checkpoint.metaLen > len(head.metaPath) {
		return nil
	}

	return &WavefrontHead{
		phase:        checkpoint.phase,
		alignedPhase: checkpoint.alignedPhase,
		queryPhase:   checkpoint.queryPhase,
		pos:          checkpoint.pos,
		segment:      checkpoint.segment,
		promptIdx:    checkpoint.promptIdx,
		energy:       checkpoint.energy,
		path:         cloneChordSlice(head.path[:checkpoint.pathLen]),
		metaPath:     cloneChordSlice(head.metaPath[:checkpoint.metaLen]),
		visited:      cloneVisitedMap(checkpoint.visited),
		age:          checkpoint.age,
		stalls:       0,
		fuzzyErrs:    head.fuzzyErrs,
	}
}

func (wf *Wavefront) initializeHeadRegisters(head *WavefrontHead, reason executionCheckpointReason) {
	if head == nil {
		return
	}
	if head.registers == nil {
		head.registers = wf.newHeadRegisters()
	}

	queryPhase := head.queryPhase
	if queryPhase == 0 {
		if head.alignedPhase != 0 {
			queryPhase = head.alignedPhase
		} else {
			queryPhase = head.phase
		}
	}

	head.energy += head.registers.ObserveTransition(1, queryPhase, head.phase)
	head.registers.RecordCheckpoint(head, reason)
}

func (wf *Wavefront) applyTransitionRegisters(
	parent *WavefrontHead,
	child *WavefrontHead,
	value data.Chord,
	queryPhase numeric.Phase,
	anchored bool,
) int {
	if child == nil {
		return 0
	}
	if child.registers == nil {
		if parent != nil {
			child.registers = cloneExecutionRegisters(parent.registers)
		}
		if child.registers == nil {
			child.registers = wf.newHeadRegisters()
		}
	}

	sourcePhase := numeric.Phase(1)
	if parent != nil && parent.phase != 0 {
		sourcePhase = parent.phase
	}

	if queryPhase == 0 {
		switch {
		case child.queryPhase != 0:
			queryPhase = child.queryPhase
		case parent != nil && parent.queryPhase != 0:
			queryPhase = parent.queryPhase
		case child.alignedPhase != 0:
			queryPhase = child.alignedPhase
		default:
			queryPhase = child.phase
		}
	}

	penalty := child.registers.ObserveTransition(sourcePhase, queryPhase, child.phase)

	if queryPhase != 0 && phaseDistanceMod257(queryPhase, child.phase) == 0 {
		child.registers.RecordCheckpoint(child, checkpointReasonStable)
	}
	if anchored {
		child.registers.RecordCheckpoint(child, checkpointReasonAnchor)
	}
	if value.Terminal() || data.Opcode(value.Opcode()) == data.OpcodeHalt {
		child.registers.RecordCheckpoint(child, checkpointReasonTerminal)
	}
	if data.Opcode(value.Opcode()) == data.OpcodeReset {
		child.registers.RecordCheckpoint(child, checkpointReasonReset)
	}

	return penalty
}

func (wf *Wavefront) backtrackPromptHead(head *WavefrontHead, expectedByte byte) *WavefrontHead {
	if head == nil || head.registers == nil || head.fuzzyErrs >= wf.maxFuzzy {
		return nil
	}

	checkpoint, trailIdx, ok := head.registers.LatestCheckpointBefore(head)
	if !ok {
		return nil
	}

	rewound := rewindHeadFromCheckpoint(head, checkpoint)
	if rewound == nil {
		return nil
	}

	rewound.fuzzyErrs = head.fuzzyErrs + 1
	rewound.age = head.age + 1
	rewound.stalls = 0
	rewound.energy += wf.promptEditPenalty(expectedByte) + 8
	rewound.registers = cloneExecutionRegisters(head.registers)
	if rewound.registers == nil {
		rewound.registers = wf.newHeadRegisters()
	}
	if trailIdx >= 0 && trailIdx+1 < len(rewound.registers.trail) {
		rewound.registers.trail = append([]executionCheckpoint(nil), rewound.registers.trail[:trailIdx+1]...)
	}
	rewound.registers.RecordCheckpoint(rewound, checkpointReasonBacktrack)

	return rewound
}
