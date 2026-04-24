package cpu

import (
	"math/bits"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/program"
	"github.com/theapemachine/six/pkg/primitive"
)

// ExecuteCommunity groups the community by resident program, then applies
// each unique program across its cohort in lockstep.
func ExecuteCommunity(community []*primitive.Value) []*primitive.Value {
	n := len(community)
	if n == 0 {
		return nil
	}

	// 1. Group Values into cohorts by their resident program (16 words).
	// This ensures Values truly execute their own resident firmware.
	cohorts := make(map[[16]uint64][]int)
	for i, v := range community {
		frame := (*[128]uint64)(unsafe.Pointer(v))
		var prog [16]uint64
		copy(prog[:], frame[16:32])
		cohorts[prog] = append(cohorts[prog], i)
	}

	// 2. Allocate the Tick Post-State buffer.
	post := make([][128]uint64, n)
	for i := 0; i < n; i++ {
		post[i] = *(*[128]uint64)(unsafe.Pointer(community[i]))
	}

	var spawned []*primitive.Value

	for prog, indices := range cohorts {
		cohortSize := len(indices)
		
		for pc := 0; pc < 16; pc++ {
			instr := prog[pc]
			if instr == 0 {
				break
			}

			aStart, aSpan, bStart, bSpan, dstStart, dstSpan, opcode, mode, topology, predStart, predCond, aInd, bType := program.DecodeInstruction(instr)

			m0 := uint64(0)
			if opcode&1 != 0 {
				m0 = ^uint64(0)
			}
			m1 := uint64(0)
			if opcode&2 != 0 {
				m1 = ^uint64(0)
			}
			m2 := uint64(0)
			if opcode&4 != 0 {
				m2 = ^uint64(0)
			}
			m3 := uint64(0)
			if opcode&8 != 0 {
				m3 = ^uint64(0)
			}

			var bImm uint64
			if bType == 2 {
				bImm = uint64(bStart) | (uint64(bSpan-1) << 7)
			}

			var foldRes [][]uint64
			if topology == program.TopologyFold {
				foldRes = make([][]uint64, cohortSize)
			}

			writeMasks := make([]uint64, cohortSize)
			finalResAll := make([][]uint64, cohortSize)

			for idx, globalI := range indices {
				frame := (*[128]uint64)(unsafe.Pointer(community[globalI]))

				writeMask := ^uint64(0)
				if predCond != 0 {
					pval := frame[predStart]
					if predCond == 1 && pval == 0 {
						writeMask = 0
					} else if predCond == 2 && pval != 0 {
						writeMask = 0
					}
				}
				writeMasks[idx] = writeMask

				if writeMask == 0 && topology == program.TopologySelf {
					continue
				}

				curAStart := aStart
				if aInd == 1 {
					curAStart = int(frame[curAStart] & 0x7F)
				}
				curBStart := bStart
				if bType == 1 {
					curBStart = int(frame[curBStart] & 0x7F)
				}

				var truthRes []uint64
				var lanesToExec int

				if mode == program.ModeTruth {
					lanesToExec = dstSpan
				} else {
					lanesToExec = aSpan
					if bSpan > lanesToExec {
						lanesToExec = bSpan
					}
				}

				truthRes = make([]uint64, lanesToExec)

				for lane := 0; lane < lanesToExec; lane++ {
					var a, b uint64

					aIdx := curAStart + (lane % aSpan)
					if aIdx < 128 {
						a = frame[aIdx]
					}

					if bType == 2 {
						b = bImm
					} else {
						bIdx := curBStart + (lane % bSpan)
						if bIdx < 128 {
							b = frame[bIdx]
						}
					}

					truthRes[lane] = (a & b & m0) | (a & ^b & m1) | (^a & b & m2) | (^a & ^b & m3)
				}

				var finalRes []uint64

				switch mode {
				case program.ModeTruth:
					finalRes = truthRes
				case program.ModePopcnt:
					total := 0
					for _, w := range truthRes {
						total += bits.OnesCount64(w)
					}
					finalRes = []uint64{uint64(total)}
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
				}

				finalResAll[idx] = finalRes

				if topology == program.TopologyFold {
					foldRes[idx] = finalRes
				}
			}

			var globalFoldRes []uint64
			if topology == program.TopologyFold && cohortSize > 0 {
				globalFoldRes = make([]uint64, len(foldRes[0]))
				copy(globalFoldRes, foldRes[0])
				for idx := 1; idx < cohortSize; idx++ {
					for j := 0; j < len(globalFoldRes) && j < len(foldRes[idx]); j++ {
						a := globalFoldRes[j]
						b := foldRes[idx][j]
						globalFoldRes[j] = (a & b & m0) | (a & ^b & m1) | (^a & b & m2) | (^a & ^b & m3)
					}
				}
			}

			var instrSpawned []*primitive.Value

			for idx, globalI := range indices {
				writeMask := writeMasks[idx]
				if writeMask == 0 {
					continue
				}

				targetI := globalI
				switch topology {
				case program.TopologyNext:
					targetI = indices[(idx+1)%cohortSize]
				case program.TopologyFold:
					targetI = globalI
				case program.TopologySpawn:
					targetI = -1 // Indicates a spawned value
				}

				finalRes := finalResAll[idx]
				if topology == program.TopologyFold {
					finalRes = globalFoldRes
				}

				var spawnFrame *[128]uint64
				if targetI == -1 {
					newVal := primitive.AllocValue()
					if newVal == nil {
						continue // Arena full
					}
					newVal.StampID()
					spawnFrame = (*[128]uint64)(unsafe.Pointer(newVal))
					// Copy TTL (offset 59) and Community (offset 64)
					// Based on updated offsets: TTL is 59, COMMUNITY is 64
					parentFrame := (*[128]uint64)(unsafe.Pointer(community[globalI]))
					spawnFrame[59] = parentFrame[59]
					spawnFrame[64] = parentFrame[64]
					instrSpawned = append(instrSpawned, newVal)
				}

				for lane := 0; lane < dstSpan; lane++ {
					dstIdx := dstStart + lane
					if dstIdx < 128 {
						val := uint64(0)
						if len(finalRes) == 1 {
							val = finalRes[0]
						} else if lane < len(finalRes) {
							val = finalRes[lane]
						}

						if targetI == -1 {
							spawnFrame[dstIdx] = val
						} else {
							post[targetI][dstIdx] = val
						}
					}
				}
			}
			
			if len(instrSpawned) > 0 {
				spawned = append(spawned, instrSpawned...)
			}
		}
	}

	// 6. Global Sync
	for i := 0; i < n; i++ {
		live := (*[128]uint64)(unsafe.Pointer(community[i]))
		copy(live[:], post[i][:])
	}

	return spawned
}
