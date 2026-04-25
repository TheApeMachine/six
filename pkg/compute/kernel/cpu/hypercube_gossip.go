package cpu

import (
	"math/bits"
	"unsafe"

	pospop "github.com/theapemachine/six/pkg/compute/kernel/cpu/csa"
	"github.com/theapemachine/six/pkg/compute/program"
	"github.com/theapemachine/six/pkg/primitive"
)

// pendingWrite forms our Write-Ahead Log (WAL).
// This guarantees perfect lock-step execution without copying Megabytes of memory.
type pendingWrite struct {
	sourceIdx int
	valueIdx  int
	dstIdx    int
	mode      uint64
	payload   uint64
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
	var spawned []*primitive.Value
	spawnedBySource := make(map[int]*primitive.Value)
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
		var writes []pendingWrite
		anyExecuted := false

		// ---------------------------------------------------------
		// PHASE 1: EXECUTION (Strictly Read-Only)
		// ---------------------------------------------------------
		for i, v := range values {
			frame := (*[128]uint64)(unsafe.Pointer(v))
			execFrame := frame
			aFrame := frame
			bFrame := frame

			if ownerFrame != nil {
				execFrame = ownerFrame
				aFrame = ownerFrame
				bFrame = frame
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

			// Safety checks to prevent divide-by-zero panics
			if aSpan == 0 {
				aSpan = 1
			}
			if bSpan == 0 {
				bSpan = 1
			}
			if dstSpan == 0 {
				dstSpan = 1
			}

			// 1. Evaluate Predicate (Write Masking)
			writeMask := ^uint64(0)
			if predCond != 0 {
				if !program.PredicateAllows(bFrame, predStart, predCond) {
					writeMask = 0
				}
			}

			if writeMask == 0 && topology == program.TopologySelf {
				continue // Short-circuit if completely masked and local
			}

			// 3. Resolve Pointer Indirection
			curAStart, curBStart := aStart, bStart
			if aInd == 1 {
				curAStart = int(aFrame[curAStart] & 0x7F)
			}
			if bType == 1 {
				curBStart = int(bFrame[curBStart] & 0x7F)
			}
			var bImm uint64
			if bType == 2 {
				bImm = uint64(bStart) | (uint64(bSpan-1) << 7)
			}

			// 4. Compute Truth Table Masks
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

			// 5. Execute ALU lanes
			lanesToExec := dstSpan
			if mode != program.ModeTruth {
				lanesToExec = max(bSpan, aSpan)
			}

			truthRes := make([]uint64, lanesToExec)
			for lane := 0; lane < lanesToExec; lane++ {
				var a, b uint64

				aIdx := curAStart + (lane % aSpan)
				if aIdx < 128 {
					a = aFrame[aIdx]
				}

				if bType == 2 {
					b = bImm
				} else {
					bIdx := curBStart + (lane % bSpan)
					if bIdx < 128 {
						b = bFrame[bIdx]
					}
				}

				truthRes[lane] = (a & b & m0) | (a & ^b & m1) | (^a & b & m2) | (^a & ^b & m3)
			}

			// 6. Collapse Mode
			var finalRes []uint64
			switch mode {
			case program.ModeTruth:
				finalRes = truthRes
			case program.ModePopcnt:
				finalRes = []uint64{popcntWords(truthRes)}
			case program.ModeAnyZero:
				witness := uint64(0)
				for _, w := range truthRes {
					if w != ^uint64(0) {
						witness = 1
						break
					}
				}
				finalRes = []uint64{witness}
			case program.ModeAllOnes:
				witness := uint64(1)
				for _, w := range truthRes {
					if w != ^uint64(0) {
						witness = 0
						break
					}
				}
				finalRes = []uint64{witness}
			case program.ModeEmit:
				finalRes = truthRes
			}

			if writeMask == 0 {
				continue
			}

			// 7. Route the Signal (Physics / Topology)
			targetIdx := i
			if targetOwner && ownerIdx >= 0 {
				targetIdx = ownerIdx
			}
			if targetB {
				targetIdx = i
			}
			switch topology {
			case program.TopologyNext:
				// True Hypercube Diffusion
				if n > 1 {
					dim := pc % bits.Len(uint(n-1))
					targetIdx = i ^ (1 << dim)
					if targetIdx >= n {
						targetIdx = i // Bounce back if void
					}
				}
			case program.TopologySpawn:
				targetIdx = -1 // Spawn signal
			}

			// 8. Append to Write-Ahead Log
			for lane := 0; lane < dstSpan; lane++ {
				dstIdx := dstStart + lane
				if dstIdx < 128 {
					val := uint64(0)
					if len(finalRes) == 1 {
						val = finalRes[0]
					} else if lane < len(finalRes) {
						val = finalRes[lane]
					}

					writes = append(writes, pendingWrite{
						sourceIdx: i,
						valueIdx:  targetIdx,
						dstIdx:    dstIdx,
						mode:      mode,
						payload:   val,
					})
				}
			}
		}

		if !anyExecuted {
			break
		}

		// ---------------------------------------------------------
		// PHASE 2: COMMIT (Global State Sync)
		// ---------------------------------------------------------
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
					spawnedBySource[w.sourceIdx] = newVal
					spawned = append(spawned, newVal)
				}
				if newVal != nil {
					spawnFrame := (*[128]uint64)(unsafe.Pointer(newVal))
					spawnFrame[w.dstIdx] = w.payload
				}
			} else {
				// Apply physical write to memory
				frame := (*[128]uint64)(unsafe.Pointer(values[w.valueIdx]))
				frame[w.dstIdx] = w.payload
			}
		}
	}

	return spawned
}

/*
popcntWords routes scalar witness reductions through the active CSA popcount
backend. On AMD64 this is AVX2/SSE2, on ARM64 it is NEON, and other targets
fall back to the same carry-save algorithm in Go.
*/
func popcntWords(words []uint64) uint64 {
	var counts [64]int
	pospop.Count64(&counts, words)

	total := 0
	for _, count := range counts {
		total += count
	}

	return uint64(total)
}
