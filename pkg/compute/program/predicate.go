package program

import (
	"fmt"
	"math/bits"
	"sync"
)

const valueWordCount = 128

const (
	PredKindPopcntLTE uint64 = 1
	PredKindPopcntLT  uint64 = 2
	PredKindHammingLT uint64 = 4
	// Hamming distance < Threshold on (Start,Span), and scalar guard on AndWord (B operand frame).
	PredKindHammingLTAndScalarEq0 uint64 = 8
	PredKindHammingLTAndScalarNE0 uint64 = 9
)

type PredicateDeviceSpec struct {
	Kind      uint64
	Start     uint64
	Span      uint64
	Threshold uint64
	AndWord   uint64
}

var (
	predicateTableMu sync.RWMutex
	predicateTable   [128]PredicateDeviceSpec

	firmwarePredSlotMu   sync.Mutex
	firmwareNextPredSlot = 1
)

/*
ResetPredicateSession clears the predicate spec table and restarts monotonic
pred slot allocation from 1. Call once before compiling each batch of
programs (config precompile, go generate) so later programs do not overwrite
table slots that earlier programs' instructions still reference.
*/
func ResetPredicateSession() {
	firmwarePredSlotMu.Lock()
	predicateTableMu.Lock()

	firmwareNextPredSlot = 1
	for idx := range predicateTable {
		predicateTable[idx] = PredicateDeviceSpec{}
	}

	predicateTableMu.Unlock()
	firmwarePredSlotMu.Unlock()
}

func beginFirmwarePredCompile() int {
	firmwarePredSlotMu.Lock()
	defer firmwarePredSlotMu.Unlock()

	return firmwareNextPredSlot
}

func finishFirmwarePredCompile(endExclusive int) {
	firmwarePredSlotMu.Lock()
	if endExclusive > firmwareNextPredSlot {
		firmwareNextPredSlot = endExclusive
	}
	firmwarePredSlotMu.Unlock()
}

func PredicateDeviceSpecs() []PredicateDeviceSpec {
	predicateTableMu.RLock()
	defer predicateTableMu.RUnlock()
	out := make([]PredicateDeviceSpec, len(predicateTable))
	copy(out[:], predicateTable[:])
	return out
}

func SetPredicateSpecSlot(slot int, spec PredicateDeviceSpec) error {
	if slot < 0 || slot >= len(predicateTable) {
		return fmt.Errorf("invalid predicate slot: %d", slot)
	}
	predicateTableMu.Lock()
	predicateTable[slot] = spec
	predicateTableMu.Unlock()
	return nil
}

func hammingWords(frameA, frameB *[valueWordCount]uint64, start, span int) uint64 {
	var dist uint64
	for lane := 0; lane < span && start+lane < valueWordCount; lane++ {
		idx := start + lane
		dist += uint64(bits.OnesCount64(frameA[idx] ^ frameB[idx]))
	}
	return dist
}

func popcntWordsFrame(frame *[valueWordCount]uint64, start, span int) uint64 {
	var count uint64
	for lane := 0; lane < span && start+lane < valueWordCount; lane++ {
		count += uint64(bits.OnesCount64(frame[start+lane]))
	}
	return count
}

/*
PredicateAllows mirrors ast_predicate_allows in backend.cu: frame is the
predicate frame for popcnt/scalar pred_cond 1–2; compound scalar uses frameB.
frameA/frameB are ctx.a/ctx.b.
*/
func PredicateAllows(
	frame, frameA, frameB *[valueWordCount]uint64,
	predStart uint64, predCond uint64,
) bool {
	if predCond == 0 {
		return true
	}
	if frame == nil {
		return false
	}
	if predStart >= valueWordCount {
		return false
	}

	slot := int(predStart)

	if predCond == 1 {
		return frame[slot] != 0
	}
	if predCond == 2 {
		return frame[slot] == 0
	}

	predicateTableMu.RLock()
	spec := predicateTable[slot]
	predicateTableMu.RUnlock()

	if spec.Kind == PredKindHammingLT {
		if frameA == nil || frameB == nil {
			return false
		}
		dist := hammingWords(frameA, frameB, int(spec.Start), int(spec.Span))
		return dist < spec.Threshold
	}
	if spec.Kind == PredKindHammingLTAndScalarEq0 || spec.Kind == PredKindHammingLTAndScalarNE0 {
		if frameA == nil || frameB == nil {
			return false
		}
		dist := hammingWords(frameA, frameB, int(spec.Start), int(spec.Span))
		if dist >= spec.Threshold {
			return false
		}
		idx := int(spec.AndWord)
		if idx < 0 || idx >= valueWordCount {
			return false
		}
		// Scalar guard follows the B operand (e.g. when B.properties…), not the executing lane.
		if spec.Kind == PredKindHammingLTAndScalarEq0 {
			return frameB[idx] == 0
		}
		return frameB[idx] != 0
	}
	if spec.Kind != PredKindPopcntLTE && spec.Kind != PredKindPopcntLT {
		// predStart indexes the device spec table for predCond==3; it is not a frame word.
		// Treat missing or unknown kinds as deny (never read frame[slot] as a legacy scalar test).
		return false
	}

	count := popcntWordsFrame(frame, int(spec.Start), int(spec.Span))
	if spec.Kind == PredKindPopcntLT {
		return count < spec.Threshold
	}
	return count <= spec.Threshold
}
