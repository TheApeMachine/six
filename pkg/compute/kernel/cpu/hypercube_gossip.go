package cpu

import (
	"math/bits"
	"unsafe"

	pospop "github.com/theapemachine/six/pkg/compute/kernel/cpu/csa"
	"github.com/theapemachine/six/pkg/compute/program"
	"github.com/theapemachine/six/pkg/primitive"
)

const csaPopcntThreshold = 15

// pendingWrite forms our Write-Ahead Log (WAL).
// This guarantees perfect lock-step execution without copying Megabytes of memory.
type pendingWrite struct {
	sourceIdx int
	valueIdx  int
	dstIdx    int
	payload   uint64
}

type foldKey struct {
	opcode uint64
	dstIdx int
}

type foldWrite struct {
	pendingWrite
	opcode uint64
}

type foldAggregate struct {
	key     foldKey
	payload uint64
}

/*
HypercubeGossip diffuses the values across the community using a hypercube.
This unifies the execution, and the networking across values, effectively
allowing data exchange as a first-class citizen in the programming model.
*/
func HypercubeGossip(
	value *primitive.Value, values []*primitive.Value,
) []*primitive.Value {
	n := len(values)
	if n == 0 {
		return nil
	}

	var spawned []*primitive.Value
	spawnedBySource := make([]*primitive.Value, n)
	writes := make([]pendingWrite, 0, n*primitive.SignalsWords)
	foldWrites := make([]foldWrite, 0, n*primitive.SignalsWords)
	foldGroups := make(map[foldKey]int, primitive.SignalsWords)
	foldAggregates := make([]foldAggregate, 0, primitive.SignalsWords)
	ownerIdx := -1
	var ownerFrame *[128]uint64

	if value != nil {
		ownerFrame = (*[128]uint64)(unsafe.Pointer(value))
		for idx, candidate := range values {
			if candidate == value {
				ownerIdx = idx
				break
			}
		}
	}

	// PC Loop: strictly 16 clock cycles.
	for pc := 0; pc < primitive.ProgramWords; pc++ {
		writes = writes[:0]
		foldWrites = foldWrites[:0]
		anyExecuted := false

		for i, v := range values {
			if v == nil {
				continue
			}

			frame := (*[128]uint64)(unsafe.Pointer(v))
			nextFrame := (*[128]uint64)(nil)
			if i+1 < n && values[i+1] != nil {
				nextFrame = (*[128]uint64)(unsafe.Pointer(values[i+1]))
			}

			execFrame := frame
			aFrame := frame
			bFrame := frame

			if ownerFrame != nil {
				execFrame = ownerFrame
				aFrame = ownerFrame

				if instrUsesBSource(execFrame[16+pc]) {
					aFrame = frame
				}
			}

			// Direct read. Values run whatever program they hold concurrently.
			instr := execFrame[16+pc]
			if instr == 0 {
				continue
			}
			anyExecuted = true

			aStart, aSpan, bStart, bSpan, dstStart, dstSpan, opcode, mode, topology, predStart, predCond, aInd, bType := program.DecodeInstruction(instr)
			targetB := instr&program.InstrFlagTargetB != 0
			targetOwner := instr&program.InstrFlagTargetOwner != 0
			if ownerFrame != nil && instr&program.InstrFlagAFromB != 0 {
				aFrame = frame
			}
			if ownerFrame != nil && instr&program.InstrFlagBFromA != 0 {
				bFrame = ownerFrame
			}
			if bType == program.InstrBTypeNext {
				bFrame = nextFrame
			}

			if aSpan == 0 {
				aSpan = 1
			}
			if bSpan == 0 {
				bSpan = 1
			}
			if dstSpan == 0 {
				dstSpan = 1
			}

			writeMask := ^uint64(0)
			if predCond != 0 {
				if bFrame == nil || !program.PredicateAllows(bFrame, predStart, predCond) {
					writeMask = 0
				}
			}

			if writeMask == 0 && topology == program.TopologySelf {
				continue // Short-circuit if completely masked and local
			}

			targetIdx := routeTarget(i, n, pc, topology, targetOwner, targetB, ownerIdx)
			if mode == program.ModeGeometric || program.IsGeometricOpcode(opcode) {
				var tmp primitive.Value
				stageGeometricOperands(&tmp, aFrame, bFrame, aStart, aSpan, bStart, bSpan)
				if GeometricFrame(unsafe.Pointer(&tmp), opcode) && writeMask != 0 {
					tmpFrame := (*[128]uint64)(unsafe.Pointer(&tmp))
					for lane := 0; lane < min(dstSpan, primitive.SignalsWords); lane++ {
						writes = append(writes, pendingWrite{
							sourceIdx: i,
							valueIdx:  targetIdx,
							dstIdx:    dstStart + lane,
							payload:   tmpFrame[primitive.SignalsStartWord+lane],
						})
					}
				}

				continue
			}

			curAStart, curBStart := aStart, bStart
			if aInd == 1 {
				curAStart = int(aFrame[curAStart] & 0x7F)
			}
			if bType == program.InstrBTypeIndirect && bFrame != nil {
				curBStart = int(bFrame[curBStart] & 0x7F)
			}
			var bImm uint64
			if bType == program.InstrBTypeImmediate {
				bImm = uint64(bStart) | (uint64(bSpan-1) << 7)
			}

			lanesToExec := dstSpan
			if mode != program.ModeTruth && mode != program.ModeEmit {
				lanesToExec = max(bSpan, aSpan)
			}

			var truthRes [64]uint64
			for lane := 0; lane < lanesToExec; lane++ {
				var a, b uint64

				aIdx := curAStart + (lane % aSpan)
				if aIdx < 128 {
					a = aFrame[aIdx]
				}

				if bType == program.InstrBTypeImmediate {
					b = bImm
				} else {
					bIdx := curBStart + (lane % bSpan)
					if bFrame != nil && bIdx < 128 {
						b = bFrame[bIdx]
					}
				}

				truthRes[lane] = truthWord(opcode, a, b)
			}

			var finalRes [64]uint64
			finalLen := lanesToExec
			switch mode {
			case program.ModeTruth:
				copy(finalRes[:], truthRes[:lanesToExec])
			case program.ModePopcnt:
				finalRes[0] = popcntWords(truthRes[:lanesToExec])
				finalLen = 1
			case program.ModeAnyZero:
				witness := uint64(0)
				for _, w := range truthRes[:lanesToExec] {
					if w != ^uint64(0) {
						witness = 1
						break
					}
				}
				finalRes[0] = witness
				finalLen = 1
			case program.ModeAllOnes:
				witness := uint64(1)
				for _, w := range truthRes[:lanesToExec] {
					if w != ^uint64(0) {
						witness = 0
						break
					}
				}
				finalRes[0] = witness
				finalLen = 1
			case program.ModeEmit:
				copy(finalRes[:], truthRes[:lanesToExec])
			}

			if writeMask == 0 {
				continue
			}

			for lane := 0; lane < dstSpan; lane++ {
				dstIdx := dstStart + lane
				if dstIdx < 128 {
					val := uint64(0)
					if finalLen == 1 {
						val = finalRes[0]
					} else if lane < finalLen {
						val = finalRes[lane]
					}

					write := pendingWrite{
						sourceIdx: i,
						valueIdx:  targetIdx,
						dstIdx:    dstIdx,
						payload:   val,
					}

					if topology == program.TopologyFold {
						foldWrites = append(foldWrites, foldWrite{
							pendingWrite: write,
							opcode:       opcode,
						})
						continue
					}

					writes = append(writes, write)
				}
			}
		}

		if !anyExecuted {
			break
		}

		if len(foldWrites) > 0 {
			writes = appendFoldWrites(writes, foldWrites, foldGroups, &foldAggregates)
		}

		for _, w := range writes {
			if w.valueIdx == -1 {
				newVal := spawnedBySource[w.sourceIdx]
				if newVal == nil {
					newVal = primitive.AllocValue()
					if newVal == nil {
						continue
					}
					if w.sourceIdx >= 0 && w.sourceIdx < len(values) {
						sourceFrame := (*[128]uint64)(unsafe.Pointer(values[w.sourceIdx]))
						spawnFrame := (*[128]uint64)(unsafe.Pointer(newVal))
						copy(spawnFrame[:], sourceFrame[:])
					}
					newVal.StampID()
					newVal.ClearProgram()
					newVal.SetSchedulingNext(0)
					newVal.SetStatus(primitive.PENDING)
					spawnedBySource[w.sourceIdx] = newVal
					spawned = append(spawned, newVal)
				}
				if newVal != nil {
					spawnFrame := (*[128]uint64)(unsafe.Pointer(newVal))
					spawnFrame[w.dstIdx] = w.payload
				}
			} else {
				if w.valueIdx < 0 || w.valueIdx >= len(values) || values[w.valueIdx] == nil {
					continue
				}
				frame := (*[128]uint64)(unsafe.Pointer(values[w.valueIdx]))
				frame[w.dstIdx] = w.payload
			}
		}
	}

	return spawned
}

func stageGeometricOperands(
	tmp *primitive.Value,
	aFrame, bFrame *[128]uint64,
	aStart, aSpan, bStart, bSpan int,
) {
	tmpFrame := (*[128]uint64)(unsafe.Pointer(tmp))

	for lane := 0; lane < min(aSpan, primitive.ContextWords); lane++ {
		srcIdx := aStart + lane
		if srcIdx < 128 {
			tmpFrame[primitive.ContextStartWord+lane] = aFrame[srcIdx]
		}
	}

	if bFrame == nil {
		return
	}

	for lane := 0; lane < min(bSpan, primitive.GradientWords); lane++ {
		srcIdx := bStart + lane
		if srcIdx < 128 {
			tmpFrame[primitive.GradientStartWord+lane] = bFrame[srcIdx]
		}
	}
}

func appendFoldWrites(
	writes []pendingWrite,
	foldWrites []foldWrite,
	foldGroups map[foldKey]int,
	foldAggregates *[]foldAggregate,
) []pendingWrite {
	clear(foldGroups)
	aggregates := (*foldAggregates)[:0]

	for _, write := range foldWrites {
		key := foldKey{
			opcode: write.opcode,
			dstIdx: write.dstIdx,
		}
		groupIdx, ok := foldGroups[key]
		if !ok {
			foldGroups[key] = len(aggregates)
			aggregates = append(aggregates, foldAggregate{
				key:     key,
				payload: write.payload,
			})
			continue
		}

		aggregates[groupIdx].payload = truthWord(key.opcode, aggregates[groupIdx].payload, write.payload)
	}

	for _, write := range foldWrites {
		key := foldKey{
			opcode: write.opcode,
			dstIdx: write.dstIdx,
		}
		groupIdx, ok := foldGroups[key]
		if !ok {
			continue
		}

		write.payload = aggregates[groupIdx].payload
		writes = append(writes, write.pendingWrite)
	}

	*foldAggregates = aggregates

	return writes
}

func routeTarget(
	sourceIdx, valueCount, pc int,
	topology uint64,
	targetOwner bool,
	targetB bool,
	ownerIdx int,
) int {
	targetIdx := sourceIdx
	if targetOwner && ownerIdx >= 0 {
		targetIdx = ownerIdx
	}
	if targetB {
		targetIdx = sourceIdx
	}

	switch topology {
	case program.TopologyNext:
		if valueCount > 1 {
			dim := pc % bits.Len(uint(valueCount-1))
			targetIdx = sourceIdx ^ (1 << dim)
			if targetIdx >= valueCount {
				targetIdx = sourceIdx
			}
		}
	case program.TopologySpawn:
		targetIdx = -1
	}

	return targetIdx
}

func truthWord(opcode, a, b uint64) uint64 {
	m0, m1, m2, m3 := uint64(0), uint64(0), uint64(0), uint64(0)
	if opcode&1 != 0 {
		m0 = ^uint64(0)
	}
	if opcode&2 != 0 {
		m1 = ^uint64(0)
	}
	if opcode&4 != 0 {
		m2 = ^uint64(0)
	}
	if opcode&8 != 0 {
		m3 = ^uint64(0)
	}

	return (a & b & m0) | (a & ^b & m1) | (^a & b & m2) | (^a & ^b & m3)
}

/*
popcntWords is the scalar witness path used by resident AST reductions.
Small program spans are faster as direct word popcounts than positional CSA.
*/
func popcntWords(words []uint64) uint64 {
	if len(words) >= csaPopcntThreshold {
		total := 0
		var counts [64]int

		pospop.Count64(&counts, words)
		for _, count := range counts {
			total += count
		}

		return uint64(total)
	}

	total := 0
	for _, word := range words {
		total += bits.OnesCount64(word)
	}

	return uint64(total)
}

func instrUsesBSource(instr uint64) bool {
	return instr&program.InstrFlagAFromB != 0
}
