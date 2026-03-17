package path

import (
	"math/bits"
	"sort"

	"github.com/theapemachine/six/pkg/numeric"
)

const (
	WavefrontRegisterRows  = 257
	WavefrontRegisterWords = 5
	WavefrontTrailLimit    = 8
)

type ExecutionCheckpointReason uint8

const (
	CheckpointSeed ExecutionCheckpointReason = iota + 1
	CheckpointStable
	CheckpointReset
	CheckpointAnchor
	CheckpointBridge
	CheckpointTerminal
)

type ExecutionCheckpoint struct {
	pos     uint32
	segment uint32
	phase   numeric.Phase
	energy  int
	pathLen int
	metaLen int
	reason  ExecutionCheckpointReason
}

type ExecutionRegisterRow [WavefrontRegisterWords]uint64

/*
ExecutionRegisters tracks observed phase residues and compact checkpoint
state while a graph path is being stabilized.
*/
type ExecutionRegisters struct {
	residues        [WavefrontRegisterRows]ExecutionRegisterRow
	alignments      [WavefrontRegisterRows]ExecutionRegisterRow
	checkpoints     [WavefrontRegisterRows]ExecutionRegisterRow
	trail           []ExecutionCheckpoint
	trailLimit      int
	lastResidue     uint16
	bestResidue     uint16
	worseningStreak uint16
}

func newExecutionRegisters() *ExecutionRegisters {
	return &ExecutionRegisters{
		trailLimit:  WavefrontTrailLimit,
		bestResidue: 0xFFFF,
	}
}

func (registers *ExecutionRegisters) clone() *ExecutionRegisters {
	if registers == nil {
		return nil
	}

	cloned := *registers
	if len(registers.trail) > 0 {
		cloned.trail = append([]ExecutionCheckpoint(nil), registers.trail...)
	}

	return &cloned
}

func RegisterPhaseIndex(phase numeric.Phase) int {
	return int(uint32(phase) % WavefrontRegisterRows)
}

func RegisterPositionIndex(pos uint32) int {
	return int(pos % WavefrontRegisterRows)
}

func RegisterSetBit(row *ExecutionRegisterRow, column int) {
	if row == nil {
		return
	}

	column %= WavefrontRegisterRows
	if column < 0 {
		column += WavefrontRegisterRows
	}

	word := column / 64
	bit := uint(column % 64)
	row[word] |= uint64(1) << bit
}

func RegisterHasBit(row ExecutionRegisterRow, column int) bool {
	column %= WavefrontRegisterRows
	if column < 0 {
		column += WavefrontRegisterRows
	}

	word := column / 64
	bit := uint(column % 64)
	return row[word]&(uint64(1)<<bit) != 0
}

func RegisterRowCount(row ExecutionRegisterRow) int {
	count := 0
	for _, word := range row {
		count += bits.OnesCount64(word)
	}

	return count
}

/*
ObserveTransition records one accepted phase transition and returns a penalty
when the transition worsens alignment relative to earlier observations.
*/
func (registers *ExecutionRegisters) ObserveTransition(
	sourcePhase numeric.Phase,
	expectedPhase numeric.Phase,
	observedPhase numeric.Phase,
) int {
	if registers == nil || observedPhase == 0 {
		return 0
	}

	if sourcePhase == 0 {
		sourcePhase = observedPhase
	}

	if expectedPhase == 0 {
		expectedPhase = observedPhase
	}

	sourceIndex := RegisterPhaseIndex(sourcePhase)
	expectedIndex := RegisterPhaseIndex(expectedPhase)
	observedIndex := RegisterPhaseIndex(observedPhase)
	residue := int(PhaseDistance(expectedPhase, observedPhase))
	penalty := 0

	if RegisterHasBit(registers.residues[sourceIndex], residue) {
		if residue > 0 {
			penalty--
		}
	} else if residue > 0 {
		penalty += residue / 4
	}
	RegisterSetBit(&registers.residues[sourceIndex], residue)

	alignmentCount := RegisterRowCount(registers.alignments[expectedIndex])
	if RegisterHasBit(registers.alignments[expectedIndex], observedIndex) {
		penalty--
	} else if alignmentCount > 0 {
		penalty += alignmentCount / 3
	}
	RegisterSetBit(&registers.alignments[expectedIndex], observedIndex)

	if registers.bestResidue == 0xFFFF || residue < int(registers.bestResidue) {
		registers.bestResidue = uint16(residue)
		registers.worseningStreak = 0
		penalty -= 2
	} else if residue > int(registers.lastResidue) {
		if registers.worseningStreak < 0xFFFF {
			registers.worseningStreak++
		}
		penalty += int(registers.worseningStreak)
	} else if residue < int(registers.lastResidue) {
		registers.worseningStreak = 0
		penalty--
	}

	registers.lastResidue = uint16(residue)
	if penalty < 0 {
		penalty = 0
	}

	return penalty
}

/*
BestResidue returns the lowest phase residue seen by the register set.
*/
func (registers *ExecutionRegisters) BestResidue() uint16 {
	if registers == nil {
		return 0xFFFF
	}

	return registers.bestResidue
}

/*
WorseningStreak returns the count of consecutive transitions that increased
residue drift.
*/
func (registers *ExecutionRegisters) WorseningStreak() uint16 {
	if registers == nil {
		return 0
	}

	return registers.worseningStreak
}

/*
TagCheckpoint marks a logical position inside the transient checkpoint plane.
*/
func (registers *ExecutionRegisters) TagCheckpoint(segment uint32, pos uint32) {
	if registers == nil {
		return
	}

	row := RegisterPositionIndex(segment)
	column := RegisterPositionIndex(pos)
	RegisterSetBit(&registers.checkpoints[row], column)
}

/*
HasCheckpointTag reports whether a checkpoint tag exists for the supplied
segment and position.
*/
func (registers *ExecutionRegisters) HasCheckpointTag(segment uint32, pos uint32) bool {
	if registers == nil {
		return false
	}

	row := RegisterPositionIndex(segment)
	column := RegisterPositionIndex(pos)
	return RegisterHasBit(registers.checkpoints[row], column)
}

/*
RecordCheckpoint appends a checkpoint snapshot for the current stabilization
head.
*/
func (registers *ExecutionRegisters) RecordCheckpoint(
	head *WavefrontHead,
	reason ExecutionCheckpointReason,
) {
	if registers == nil || head == nil {
		return
	}

	registers.TagCheckpoint(head.segment, head.pos)

	checkpoint := ExecutionCheckpoint{
		pos:     head.pos,
		segment: head.segment,
		phase:   head.phase,
		energy:  head.energy,
		pathLen: len(head.path),
		metaLen: len(head.meta),
		reason:  reason,
	}

	if count := len(registers.trail); count > 0 {
		last := registers.trail[count-1]
		if last.pos == checkpoint.pos &&
			last.segment == checkpoint.segment &&
			last.pathLen == checkpoint.pathLen &&
			last.reason == checkpoint.reason {
			return
		}
	}

	registers.trail = append(registers.trail, checkpoint)
	if registers.trailLimit > 0 && len(registers.trail) > registers.trailLimit {
		registers.trail = append([]ExecutionCheckpoint(nil), registers.trail[len(registers.trail)-registers.trailLimit:]...)
	}
}

/*
GarbageCollect keeps the most relevant checkpoints and drops stale residue from
the trail.
*/
func (registers *ExecutionRegisters) GarbageCollect(
	head *WavefrontHead,
	limit int,
	window int,
) {
	if registers == nil || len(registers.trail) == 0 {
		return
	}

	if limit <= 0 {
		limit = registers.trailLimit
	}

	if limit <= 0 {
		limit = WavefrontTrailLimit
	}

	type checkpointKey struct {
		pos     uint32
		segment uint32
		pathLen int
	}

	type scoredCheckpoint struct {
		checkpoint ExecutionCheckpoint
		index      int
		score      int
	}

	maxPath := 0
	maxMeta := 0
	if head != nil {
		maxPath = len(head.path)
		maxMeta = len(head.meta)
	}

	bestByKey := make(map[checkpointKey]scoredCheckpoint, len(registers.trail))

	for index, checkpoint := range registers.trail {
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

		score := priority*1024 - distance*16
		if head != nil {
			score -= (head.energy - checkpoint.energy) / 8
		}

		key := checkpointKey{
			pos:     checkpoint.pos,
			segment: checkpoint.segment,
			pathLen: checkpoint.pathLen,
		}

		existing, ok := bestByKey[key]
		if !ok || score > existing.score || (score == existing.score && index > existing.index) {
			bestByKey[key] = scoredCheckpoint{
				checkpoint: checkpoint,
				index:      index,
				score:      score,
			}
		}
	}

	if len(bestByKey) == 0 {
		registers.trail = nil
		for index := range registers.checkpoints {
			registers.checkpoints[index] = ExecutionRegisterRow{}
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

	registers.trail = make([]ExecutionCheckpoint, len(kept))
	for index, checkpoint := range kept {
		registers.trail[index] = checkpoint.checkpoint
	}

	for index := range registers.checkpoints {
		registers.checkpoints[index] = ExecutionRegisterRow{}
	}

	for _, checkpoint := range registers.trail {
		registers.TagCheckpoint(checkpoint.segment, checkpoint.pos)
	}
}

func checkpointPriority(reason ExecutionCheckpointReason) int {
	switch reason {
	case CheckpointStable, CheckpointAnchor, CheckpointReset, CheckpointTerminal:
		return 5
	case CheckpointBridge, CheckpointSeed:
		return 4
	default:
		return 2
	}
}

func PhaseDistance(left numeric.Phase, right numeric.Phase) uint32 {
	if left == right {
		return 0
	}

	leftValue := uint32(left) % numeric.FermatPrime
	rightValue := uint32(right) % numeric.FermatPrime

	forward := (leftValue + numeric.FermatPrime - rightValue) % numeric.FermatPrime
	backward := (rightValue + numeric.FermatPrime - leftValue) % numeric.FermatPrime

	if forward < backward {
		return forward
	}

	return backward
}
