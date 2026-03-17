package path

import (
	"fmt"

	"github.com/theapemachine/six/pkg/logic/synthesis/goal"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

/*
Wavefront stabilizes prefetched values inside the graph substrate.
It keeps the address plane passive by operating only on materialized s,
applying phase checks, anchor correction, and optional bridge synthesis.
*/
type Wavefront struct {
	calc                  *numeric.Calculus
	anchorStride          uint32
	anchorTolerance       uint32
	checkpointTrailLimit  int
	checkpointWindow      int
	frustrationAttempts   int
	frustrationCandidates int
	fe                    *goal.FrustrationEngineServer
}

type WavefrontOpts func(*Wavefront)

/*
NewWavefront instantiates a graph-local wavefront controller for prefetched
value s.
*/
func NewWavefront(opts ...WavefrontOpts) *Wavefront {
	wavefront := &Wavefront{
		calc:                  numeric.NewCalculus(),
		checkpointTrailLimit:  WavefrontTrailLimit,
		checkpointWindow:      4,
		frustrationAttempts:   int(numeric.FermatPrime),
		frustrationCandidates: int(numeric.FermatPrimitive),
	}

	for _, opt := range opts {
		opt(wavefront)
	}

	return wavefront
}

/*
CanStabilize reports whether at least one supplied sequence carries the operator
state required for wavefront-style stabilization.
*/
func (wavefront *Wavefront) CanStabilize(sequences [][]data.Value) bool {
	for _, sequence := range sequences {
		if wavefront.canStabilizeSequence(sequence) {
			return true
		}
	}

	return false
}

/*
Stabilize validates and repairs prefetched sequences before they are folded by the
graph substrate.
*/
func (wavefront *Wavefront) Stabilize(
	sequences [][]data.Value,
	metaSequences [][]data.Value,
) ([][]data.Value, [][]data.Value, error) {
	if len(sequences) != len(metaSequences) {
		return nil, nil, fmt.Errorf(
			"%w: %d sequences != %d meta sequences",
			ErrWavefrontMismatchedCount,
			len(sequences),
			len(metaSequences),
		)
	}

	results := make([]WavefrontResult, 0, len(sequences))

	for index := range sequences {
		sequence := sequences[index]
		metaSequence := metaSequences[index]

		if len(sequence) != len(metaSequence) {
			return nil, nil, fmt.Errorf(
				"%w:  %d has %d values != %d meta values",
				ErrWavefrontMismatchedSequenceLength,
				index,
				len(sequence),
				len(metaSequence),
			)
		}

		if !wavefront.canStabilizeSequence(sequence) {
			results = append(results, wavefront.cloneResult(sequence, metaSequence))
			continue
		}

		result, err := wavefront.stabilizeSequence(index, sequence, metaSequence)
		if err != nil {
			return nil, nil, err
		}

		results = append(results, result)
	}

	stables := make([][]data.Value, len(results))
	stableMetas := make([][]data.Value, len(results))

	for index, result := range results {
		stables[index] = result.path
		stableMetas[index] = result.meta
	}

	return stables, stableMetas, nil
}

func (wavefront *Wavefront) canStabilizeSequence(sequence []data.Value) bool {
	if len(sequence) < 2 || wavefront.phaseForValue(sequence[0]) == 0 {
		return false
	}

	hasPhase := false
	hasOperator := false

	for _, value := range sequence {
		if wavefront.phaseForValue(value) != 0 {
			hasPhase = true
		}

		if wavefront.valueCarriesOperator(value) {
			hasOperator = true
		}

		if hasPhase && hasOperator {
			return true
		}
	}

	return false
}

func (wavefront *Wavefront) cloneResult(
	sequence []data.Value,
	metaSequence []data.Value,
) WavefrontResult {
	phase := numeric.Phase(0)
	if len(sequence) > 0 {
		phase = wavefront.phaseForValue(sequence[len(sequence)-1])
	}

	return WavefrontResult{
		path:  cloneValueSlice(sequence),
		meta:  cloneValueSlice(metaSequence),
		phase: phase,
		depth: uint32(len(sequence)),
	}
}

func (wavefront *Wavefront) stabilizeSequence(
	sequenceIndex int,
	sequence []data.Value,
	metaSequence []data.Value,
) (WavefrontResult, error) {
	head, err := wavefront.seedHead(sequenceIndex, sequence, metaSequence)
	if err != nil {
		return WavefrontResult{}, err
	}

	for valueIndex := 1; valueIndex < len(sequence); valueIndex++ {
		currentValue := head.path[len(head.path)-1]
		nextValue := sequence[valueIndex]
		nextMeta := metaSequence[valueIndex]

		nextPos, nextSegment, ok := wavefront.advanceCursor(head.pos, head.segment, currentValue)
		if !ok {
			return WavefrontResult{}, fmt.Errorf(
				"%w:  %d halted before value %d",
				ErrWavefrontUnexpectedTerminal,
				sequenceIndex,
				valueIndex,
			)
		}

		if !wavefront.valueCarriesOperator(currentValue) {
			head.pos = nextPos
			head.segment = nextSegment
			head.path = append(head.path, nextValue)
			head.meta = append(head.meta, nextMeta)

			if observedPhase := wavefront.phaseForValue(nextValue); observedPhase != 0 {
				head.phase = observedPhase
			}

			head.recordOpcodeCheckpoint(currentValue)
			continue
		}

		observedPhase := wavefront.phaseForValue(nextValue)
		if observedPhase == 0 {
			head.pos = nextPos
			head.segment = nextSegment
			head.path = append(head.path, nextValue)
			head.meta = append(head.meta, nextMeta)
			head.recordOpcodeCheckpoint(currentValue)
			continue
		}

		expectedPhase := wavefront.predictNextPhase(currentValue, head.phase)
		resolvedPhase, transitionPenalty, anchored, ok := wavefront.resolveTransition(
			nextPos,
			expectedPhase,
			currentValue,
			nextValue,
			observedPhase,
		)

		if !ok {
			bridgedHead, bridgeOK, bridgeErr := wavefront.bridgeDiscontinuity(
				sequenceIndex,
				valueIndex,
				head,
				nextPos,
				nextSegment,
				nextValue,
				nextMeta,
				observedPhase,
			)
			if bridgeErr != nil {
				return WavefrontResult{}, bridgeErr
			}
			if bridgeOK {
				head = bridgedHead
				continue
			}

			return WavefrontResult{}, fmt.Errorf(
				"%w:  %d value %d rejected expected phase %d observed phase %d",
				ErrWavefrontTransitionRejected,
				sequenceIndex,
				valueIndex,
				expectedPhase,
				observedPhase,
			)
		}

		stableValue := nextValue
		if resolvedPhase != observedPhase {
			stableValue.SetStatePhase(resolvedPhase)
		}

		head.energy += transitionPenalty
		head.energy += head.registers.ObserveTransition(head.phase, expectedPhase, resolvedPhase)
		head.phase = resolvedPhase
		head.pos = nextPos
		head.segment = nextSegment
		head.path = append(head.path, stableValue)
		head.meta = append(head.meta, nextMeta)

		head.registers.RecordCheckpoint(head, CheckpointStable)
		if anchored {
			head.registers.RecordCheckpoint(head, CheckpointAnchor)
		}

		head.recordOpcodeCheckpoint(stableValue)
	}

	head.registers.GarbageCollect(head, wavefront.checkpointTrailLimit, wavefront.checkpointWindow)

	return WavefrontResult{
		path:   head.path,
		meta:   head.meta,
		phase:  head.phase,
		energy: head.energy,
		depth:  head.pos,
	}, nil
}

func (wavefront *Wavefront) seedHead(
	sequenceIndex int,
	sequence []data.Value,
	metaSequence []data.Value,
) (*WavefrontHead, error) {
	if len(sequence) == 0 || len(metaSequence) == 0 {
		return &WavefrontHead{
			path:      cloneValueSlice(sequence),
			meta:      cloneValueSlice(metaSequence),
			registers: newExecutionRegisters(),
		}, nil
	}

	phase := wavefront.phaseForValue(sequence[0])
	if phase == 0 {
		return nil, fmt.Errorf(
			"%w:  %d first value carries no phase",
			ErrWavefrontMissingPhase,
			sequenceIndex,
		)
	}

	head := &WavefrontHead{
		phase:     phase,
		path:      []data.Value{sequence[0]},
		meta:      []data.Value{metaSequence[0]},
		registers: newExecutionRegisters(),
	}

	head.registers.trailLimit = wavefront.checkpointTrailLimit
	head.energy += head.registers.ObserveTransition(phase, phase, phase)
	head.registers.RecordCheckpoint(head, CheckpointSeed)
	head.recordOpcodeCheckpoint(sequence[0])

	return head, nil
}

func (wavefront *Wavefront) advanceCursor(
	pos uint32,
	segment uint32,
	value data.Value,
) (uint32, uint32, bool) {
	opcode, jump, _, terminal := value.Program()
	if terminal || opcode == data.OpcodeHalt {
		return pos, segment, false
	}

	if jump == 0 {
		jump = 1
	}

	switch opcode {
	case data.OpcodeReset:
		return 0, segment + 1, true
	case data.OpcodeJump:
		return pos + jump, segment, true
	default:
		return pos + jump, segment, true
	}
}

func (wavefront *Wavefront) predictNextPhase(
	value data.Value,
	currentPhase numeric.Phase,
) numeric.Phase {
	if from, to, ok := value.Trajectory(); ok && from == currentPhase {
		return to
	}

	if value.HasAffine() {
		return value.ApplyAffinePhase(currentPhase)
	}

	return currentPhase
}

func (wavefront *Wavefront) resolveTransition(
	pos uint32,
	expectedPhase numeric.Phase,
	currentValue data.Value,
	nextValue data.Value,
	observedPhase numeric.Phase,
) (numeric.Phase, int, bool, bool) {
	resolvedPhase := expectedPhase
	penalty := wavefront.routePenalty(currentValue, nextValue)
	anchored := false

	if snappedPhase, anchorPenalty, ok := wavefront.anchorCorrect(pos, expectedPhase, nextValue); ok {
		resolvedPhase = snappedPhase
		penalty += anchorPenalty
		anchored = true
	}

	drift := wavefront.phaseDistance(resolvedPhase, observedPhase)

	if currentValue.HasGuard() {
		if drift > uint32(currentValue.GuardRadius()) {
			return 0, 0, anchored, false
		}

		penalty += int(drift)
		return observedPhase, penalty, anchored, true
	}

	if drift != 0 {
		return 0, 0, anchored, false
	}

	return resolvedPhase, penalty, anchored, true
}

func (wavefront *Wavefront) bridgeDiscontinuity(
	sequenceIndex int,
	valueIndex int,
	head *WavefrontHead,
	nextPos uint32,
	nextSegment uint32,
	targetValue data.Value,
	targetMeta data.Value,
	targetPhase numeric.Phase,
) (*WavefrontHead, bool, error) {
	if head == nil || wavefront.fe == nil {
		return nil, false, nil
	}

	sourceValue := head.path[len(head.path)-1]
	if sourceValue.ActiveCount() == 0 || targetValue.ActiveCount() == 0 {
		return nil, false, nil
	}

	s, err := wavefront.fe.ResolveCandidates(
		sourceValue,
		targetValue,
		wavefront.frustrationAttempts,
		wavefront.frustrationCandidates,
	)
	if err != nil {
		return nil, false, fmt.Errorf(
			"%w:  %d value %d bridge resolve failed: %v",
			ErrWavefrontBridgeRejected,
			sequenceIndex,
			valueIndex,
			err,
		)
	}

	if len(s) == 0 {
		return nil, false, nil
	}

	bestHead := (*WavefrontHead)(nil)
	bestDepth := 0

	for _, opcode := range s {
		candidate := head.clone()
		bridgeValues, bridgeMeta, _, bridgePenalty := wavefront.synthesizeBridge(candidate.phase, opcode)
		if len(bridgeValues) == 0 {
			continue
		}

		for bridgeIndex := range bridgeValues {
			candidate.path = append(candidate.path, bridgeValues[bridgeIndex])
			candidate.meta = append(candidate.meta, bridgeMeta[bridgeIndex])
			candidate.energy += bridgePenalty
			candidate.phase = wavefront.phaseForValue(bridgeValues[bridgeIndex])
			candidate.registers.RecordCheckpoint(candidate, CheckpointBridge)
		}

		candidate.path = append(candidate.path, targetValue)
		candidate.meta = append(candidate.meta, targetMeta)
		candidate.phase = targetPhase
		candidate.pos = nextPos
		candidate.segment = nextSegment
		candidate.energy += candidate.registers.ObserveTransition(head.phase, targetPhase, targetPhase)
		candidate.registers.RecordCheckpoint(candidate, CheckpointStable)
		candidate.recordOpcodeCheckpoint(targetValue)

		if bestHead == nil ||
			candidate.energy < bestHead.energy ||
			(candidate.energy == bestHead.energy && len(bridgeValues) < bestDepth) {
			bestHead = candidate
			bestDepth = len(bridgeValues)
		}
	}

	if bestHead == nil {
		return nil, false, fmt.Errorf(
			"%w:  %d value %d produced no viable bridge",
			ErrWavefrontBridgeRejected,
			sequenceIndex,
			valueIndex,
		)
	}

	return bestHead, true, nil
}

func (wavefront *Wavefront) synthesizeBridge(
	startPhase numeric.Phase,
	opcodes []*macro.MacroOpcode,
) ([]data.Value, []data.Value, numeric.Phase, int) {
	phase := startPhase
	values := make([]data.Value, 0, len(opcodes))
	metaValues := make([]data.Value, 0, len(opcodes))
	penalty := 0

	for index, opcode := range opcodes {
		if opcode == nil {
			continue
		}

		previousPhase := phase
		phase = opcode.ApplyPhase(phase)

		value := data.NeutralValue()
		value.SetStatePhase(phase)
		value.SetAffine(opcode.Scale, opcode.Translate)
		value.SetTrajectory(previousPhase, phase)
		value.SetGuardRadius(2)
		value.SetMutable(true)

		program := data.OpcodeNext
		if index+1 < len(opcodes) {
			program = data.OpcodeBranch
		}
		value.SetProgram(program, 1, uint8(len(opcodes)-index-1), false)

		meta := data.MustNewValue()
		meta.SetProgram(program, 1, uint8(len(opcodes)-index-1), false)

		values = append(values, value)
		metaValues = append(metaValues, meta)

		penalty += 4
		if !opcode.Hardened {
			penalty += 2
		}
	}

	return values, metaValues, phase, penalty
}

func (wavefront *Wavefront) valueCarriesOperator(value data.Value) bool {
	if value.HasTrajectory() || value.HasGuard() || data.Opcode(value.Opcode()) != 0 {
		return true
	}

	if !value.HasAffine() {
		return false
	}

	scale, translate := value.Affine()
	return scale != 1 || translate != 0
}

func (wavefront *Wavefront) phaseForValue(value data.Value) numeric.Phase {
	carry := value.ResidualCarry()
	if carry == 0 {
		return 0
	}

	return numeric.Phase(carry % uint64(numeric.FermatPrime))
}

func (wavefront *Wavefront) phaseDistance(
	left numeric.Phase,
	right numeric.Phase,
) uint32 {
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

func (wavefront *Wavefront) anchorCorrect(
	pos uint32,
	expectedPhase numeric.Phase,
	value data.Value,
) (numeric.Phase, int, bool) {
	if wavefront.anchorStride == 0 || pos == 0 || pos%wavefront.anchorStride != 0 {
		return 0, 0, false
	}

	anchorPhase := wavefront.phaseForValue(value)
	if anchorPhase == 0 {
		return 0, 0, false
	}

	drift := wavefront.phaseDistance(expectedPhase, anchorPhase)
	if drift == 0 || drift > wavefront.anchorTolerance {
		return 0, 0, false
	}

	return anchorPhase, int(drift), true
}

func (wavefront *Wavefront) routePenalty(
	currentValue data.Value,
	nextValue data.Value,
) int {
	if !currentValue.HasRouteHint() || !nextValue.HasRouteHint() {
		return 0
	}

	if currentValue.RouteHint() == nextValue.RouteHint() {
		return 0
	}

	return 1
}

/*
WavefrontWithAnchors configures periodic phase correction for prefetched
graph sequences.
*/
func WavefrontWithAnchors(stride, tolerance uint32) WavefrontOpts {
	return func(wavefront *Wavefront) {
		wavefront.anchorStride = stride
		wavefront.anchorTolerance = tolerance
	}
}

/*
WavefrontWithFrustrationEngine installs the bridge synthesizer used when a
prefetched sequence contains a discontinuity.
*/
func WavefrontWithFrustrationEngine(
	fe *goal.FrustrationEngineServer,
	attempts int,
	candidates int,
) WavefrontOpts {
	return func(wavefront *Wavefront) {
		wavefront.fe = fe
		if attempts > 0 {
			wavefront.frustrationAttempts = attempts
		}
		if candidates > 0 {
			wavefront.frustrationCandidates = candidates
		}
	}
}

/*
WavefrontWithCheckpointWindow tunes checkpoint retention for long graph
sequences.
*/
func WavefrontWithCheckpointWindow(limit, window int) WavefrontOpts {
	return func(wavefront *Wavefront) {
		if limit > 0 {
			wavefront.checkpointTrailLimit = limit
		}
		if window >= 0 {
			wavefront.checkpointWindow = window
		}
	}
}

/*
WavefrontError identifies a stabilization failure in the substrate-local
wavefront controller.
*/
type WavefrontError string

const (
	ErrWavefrontMismatchedCount          WavefrontError = "_wavefront_mismatched__count"
	ErrWavefrontMismatchedSequenceLength WavefrontError = "_wavefront_mismatched_sequence_length"
	ErrWavefrontMissingPhase             WavefrontError = "_wavefront_missing_phase"
	ErrWavefrontTransitionRejected       WavefrontError = "_wavefront_transition_rejected"
	ErrWavefrontBridgeRejected           WavefrontError = "_wavefront_bridge_rejected"
	ErrWavefrontUnexpectedTerminal       WavefrontError = "_wavefront_unexpected_terminal"
)

/*
Error implements the error interface for WavefrontError.
*/
func (err WavefrontError) Error() string {
	return string(err)
}

type WavefrontHead struct {
	phase     numeric.Phase
	pos       uint32
	segment   uint32
	energy    int
	path      []data.Value
	meta      []data.Value
	registers *ExecutionRegisters
}

func (head *WavefrontHead) clone() *WavefrontHead {
	if head == nil {
		return nil
	}

	return &WavefrontHead{
		phase:     head.phase,
		pos:       head.pos,
		segment:   head.segment,
		energy:    head.energy,
		path:      cloneValueSlice(head.path),
		meta:      cloneValueSlice(head.meta),
		registers: head.registers.clone(),
	}
}

func (head *WavefrontHead) recordOpcodeCheckpoint(value data.Value) {
	if head == nil || head.registers == nil {
		return
	}

	opcode, _, _, terminal := value.Program()

	if terminal || opcode == data.OpcodeHalt {
		head.registers.RecordCheckpoint(head, CheckpointTerminal)
	}

	if opcode == data.OpcodeReset {
		head.registers.RecordCheckpoint(head, CheckpointReset)
	}
}

type WavefrontResult struct {
	path   []data.Value
	meta   []data.Value
	phase  numeric.Phase
	energy int
	depth  uint32
}

func cloneValueSlice(values []data.Value) []data.Value {
	if len(values) == 0 {
		return nil
	}

	return append([]data.Value(nil), values...)
}
