package cpu

import "math/bits"

/*
Predicate condition codes packed into instr[58:61]. The first six are
classic comparisons against a 1-word threshold into a canonical full-word
mask (^0 / 0). Multi-word sources compare their popcount witness; single-word
sources compare the raw scalar so property guards can use direct values. The
last two are reductions that store a value rather than a mask:

  - PredStorePopcnt: write popcount(A) as an integer scalar to dst[0].
    Lets `set X <- popcnt(Y)` collapse into one instruction without a
    separate accumulate-and-fold lowering.
  - PredAnyZero: write ^0 to dst[0] if any word in A is zero, else 0.
    Implements the legacy `any_zero(...)` primitive used by falsification
    / open-ended-generation programs.
*/
const (
	PredLT          = 0
	PredLE          = 1
	PredGT          = 2
	PredGE          = 3
	PredEQ          = 4
	PredNE          = 5
	PredStorePopcnt = 6
	PredAnyZero     = 7
)

const (
	OpReduceArgMinNonZero = 0x1
	OpReduceModeEq        = 0x2
)

const (
	PredicateBitShift  = 57
	PredicateCondShift = 58
	// SrcAFromBShift selects the popped B frame as the SrcA pointer. It
	// makes "operate entirely on B" (e.g. write B.x <- xor(B.x, B.y))
	// expressible without reverse-engineering the operand routing.
	SrcAFromBShift = 61
	// StageBitShift marks an instruction as a stage(B) directive: instead
	// of running the truth-table body, the kernel records the bound B
	// for the host to push into staging[ownerFrame[ReferenceWord]].
	StageBitShift = 62
	// PopEndBitShift marks the last instruction of a pop(B) body. After
	// executing such an instruction the kernel advances the lane cursor
	// and, if more Bs remain, rewinds pc to the body start so the body
	// runs once per lane element in a single sweep.
	PopEndBitShift = 63
)

/*
executeKernelGo is the canonical Go implementation of the device ALU.
It supports the truth-table mode, the predicate mode (compare / store /
any-zero), and the SrcA-from-B routing bit. Every asm fast path defers
to this function for any frame that uses a feature beyond plain
truth-table ops.
*/
func (backend *Backend) executeKernelGo(
	ownerFrame *[128]uint64,
	ownerIdx uint64,
	community []*[128]uint64,
	communitySize uint64,
	dimCount uint64,
) []uint64 {
	bQueueIdx := uint64(0)
	currentBIdx := uint64(0)
	var currentB *[128]uint64
	var stagedIdx []uint64
	popBodyStart := uint64(0)
	popActive := false

	for pc := uint64(0); pc < ProgramWords; pc++ {
		instr := ownerFrame[ProgramStartWord+pc]
		if instr == 0 {
			continue
		}

		opcode := instr & 0xF
		aStart := (instr >> 4) & 0x7F
		aSpan := ((instr >> 11) & 0x7F) + 1
		bStart := (instr >> 18) & 0x7F
		bSpan := ((instr >> 25) & 0x7F) + 1
		dstStart := (instr >> 32) & 0x7F
		dstSpan := ((instr >> 39) & 0x7F) + 1
		maskStart := (instr >> 46) & 0x7F

		targetB := (instr >> 53) & 1
		emit := (instr >> 54) & 1
		topology := (instr >> 55) & 3
		predicate := (instr >> PredicateBitShift) & 1
		predCond := (instr >> PredicateCondShift) & 7
		srcAFromB := (instr >> SrcAFromBShift) & 1
		stageBit := (instr >> StageBitShift) & 1
		popEnd := (instr >> PopEndBitShift) & 1

		if predicate == 1 {
			switch opcode {
			case OpReduceArgMinNonZero:
				reduceArgMinNonZero(ownerFrame, community, communitySize, aStart, bStart, dstStart, maskStart)
				continue
			case OpReduceModeEq:
				reduceModeEq(ownerFrame, community, communitySize, aStart, bStart, dstStart, maskStart)
				continue
			}
		}

		if topology == 1 && bQueueIdx < communitySize {
			currentB = community[bQueueIdx]
			currentBIdx = bQueueIdx
			bQueueIdx++
			popBodyStart = pc + 1
			popActive = true
		}

		if stageBit == 1 {
			if currentB != nil {
				stagedIdx = append(stagedIdx, currentBIdx)
			}

			// stage(B) is a side-effect-only instruction; skip the
			// truth-table body but still honor the pop-loop rewind so
			// `pop(B) { ...; stage(B) }` drains the entire lane.
			if popEnd == 1 && popActive {
				if bQueueIdx < communitySize {
					currentB = community[bQueueIdx]
					currentBIdx = bQueueIdx
					bQueueIdx++
					pc = popBodyStart - 1
					continue
				}

				popActive = false
			}

			continue
		}

		// Predicate path uses a single ptrB; gossip bodies sweep all dims
		// later in the truth-table block. Pick a representative peer here
		// (dim 0) so predicate semantics still resolve sensibly.
		ptrB := currentB
		if topology == 2 && dimCount > 0 {
			peerIdx := ownerIdx ^ 1
			if peerIdx < communitySize {
				ptrB = community[peerIdx]
			}
		}
		if ptrB == nil {
			ptrB = ownerFrame
		}

		ptrA := ownerFrame
		if srcAFromB == 1 {
			ptrA = ptrB
		}

		ptrDst := ownerFrame
		if targetB == 1 {
			ptrDst = ptrB
		}

		if predicate == 1 {
			guard := ownerFrame[maskStart]
			var pop uint64
			for lane := uint64(0); lane < aSpan; lane++ {
				pop += uint64(bits.OnesCount64(ptrA[(aStart+lane)&127]))
			}

			switch predCond {
			case PredStorePopcnt:
				dstIdx := dstStart & 127
				prevDst := ptrDst[dstIdx]
				ptrDst[dstIdx] = (pop & guard) | (prevDst & ^guard)
			case PredAnyZero:
				// Tracks whether any single word in the A range is zero.
				zeroSeen := false
				for lane := uint64(0); lane < aSpan; lane++ {
					if ptrA[(aStart+lane)&127] == 0 {
						zeroSeen = true
						break
					}
				}
				var result uint64
				if zeroSeen {
					result = ^uint64(0)
				}
				dstIdx := dstStart & 127
				prevDst := ptrDst[dstIdx]
				ptrDst[dstIdx] = (result & guard) | (prevDst & ^guard)
			default:
				threshold := ownerFrame[bStart&127]
				witness := pop
				if aSpan == 1 {
					witness = ptrA[aStart&127]
				}

				var hit bool
				switch predCond {
				case PredLT:
					hit = witness < threshold
				case PredLE:
					hit = witness <= threshold
				case PredGT:
					hit = witness > threshold
				case PredGE:
					hit = witness >= threshold
				case PredEQ:
					hit = witness == threshold
				case PredNE:
					hit = witness != threshold
				}

				var maskValue uint64
				if hit {
					maskValue = ^uint64(0)
				}
				ptrDst[dstStart&127] = maskValue & guard
			}

			if popEnd == 1 && popActive {
				if bQueueIdx < communitySize {
					currentB = community[bQueueIdx]
					currentBIdx = bQueueIdx
					bQueueIdx++
					pc = popBodyStart - 1
					continue
				}

				popActive = false
			}

			continue
		}

		mask := ownerFrame[maskStart]

		m0 := -(opcode & 1)
		m1 := -((opcode >> 1) & 1)
		m2 := -((opcode >> 2) & 1)
		m3 := -((opcode >> 3) & 1)

		// Under hypercube topology one instruction sweeps the staged
		// community from the owner's perspective. Two semantics depend
		// on target:
		//   target=A: fold the truth-table op across every peer into
		//             the owner's dst (or → union, and → intersect,
		//             xor → chain). One program slot, |community|
		//             effective ops.
		//   target=B: write the single (A,peer) result back to every
		//             peer's dst. This is the broadcast write that
		//             stamps a per-peer marker (e.g. set
		//             B.properties.community <- A.id) across the
		//             whole gossip neighborhood in one instruction.
		hypercube := topology == 2 && communitySize > 0

		if hypercube && targetB == 1 {
			for k := uint64(0); k < communitySize; k++ {
				if k == ownerIdx {
					continue
				}

				peer := community[k]
				for lane := uint64(0); lane < dstSpan; lane++ {
					wordA := ptrA[(aStart+(lane%aSpan))&127]
					wordB := peer[(bStart+(lane%bSpan))&127]

					res := (wordA & wordB & m0) |
						(wordA & ^wordB & m1) |
						(^wordA & wordB & m2) |
						(^wordA & ^wordB & m3)

					dstIdx := (dstStart + lane) & 127
					prevDst := peer[dstIdx]
					peer[dstIdx] = (res & mask) | (prevDst & ^mask)
				}
			}
		} else {
			peers := uint64(1)
			if hypercube {
				peers = communitySize
			}

			for lane := uint64(0); lane < dstSpan; lane++ {
				startA := ptrA[(aStart+(lane%aSpan))&127]
				dstIdx := (dstStart + lane) & 127
				prevDst := ptrDst[dstIdx]

				acc := startA
				any := false
				for k := uint64(0); k < peers; k++ {
					peer := ptrB
					if hypercube {
						if k == ownerIdx {
							continue
						}
						peer = community[k]
					}

					wordB := peer[(bStart+(lane%bSpan))&127]

					acc = (acc & wordB & m0) |
						(acc & ^wordB & m1) |
						(^acc & wordB & m2) |
						(^acc & ^wordB & m3)
					any = true
				}

				if !any {
					acc = startA
				}

				ptrDst[dstIdx] = (acc & mask) | (prevDst & ^mask)
			}
		}

		if emit == 1 && mask != 0 {
			ownerFrame[SpawnRegisterWord] += 1
		}

		// At the end of a pop(B) body, advance the lane cursor and rewind
		// to the body start if more Bs remain. The body executes once per
		// staged B in a single sweep — programs stay linear, the kernel
		// runs the loop. When the lane is drained, fall through and the
		// outer pc loop continues past the pop block.
		if popEnd == 1 && popActive {
			if bQueueIdx < communitySize {
				currentB = community[bQueueIdx]
				currentBIdx = bQueueIdx
				bQueueIdx++
				pc = popBodyStart - 1
				continue
			}

			popActive = false
		}
	}

	return stagedIdx
}

func reduceArgMinNonZero(
	ownerFrame *[128]uint64,
	community []*[128]uint64,
	communitySize uint64,
	valueStart uint64,
	keyStart uint64,
	dstStart uint64,
	guardStart uint64,
) {
	if ownerFrame == nil || communitySize == 0 || ownerFrame[guardStart&127] == 0 {
		return
	}

	bestValue := uint64(0)
	bestKey := ^uint64(0)

	for idx := uint64(0); idx < communitySize; idx++ {
		peer := community[idx]
		if peer == nil {
			continue
		}

		value := peer[valueStart&127]
		if value == 0 {
			continue
		}

		key := peer[keyStart&127]
		if key >= bestKey {
			continue
		}

		bestKey = key
		bestValue = value
	}

	if bestValue == 0 {
		return
	}

	ownerFrame[dstStart&127] = bestValue
}

func reduceModeEq(
	ownerFrame *[128]uint64,
	community []*[128]uint64,
	communitySize uint64,
	valueStart uint64,
	keyStart uint64,
	dstStart uint64,
	matchStart uint64,
) {
	if ownerFrame == nil || communitySize == 0 {
		return
	}

	match := ownerFrame[matchStart&127]
	if match == 0 {
		return
	}

	var counts [256]uint64
	var overflow map[uint64]uint64
	bestValue := uint64(0)
	bestCount := uint64(0)

	for idx := uint64(0); idx < communitySize; idx++ {
		peer := community[idx]
		if peer == nil || peer[keyStart&127] != match {
			continue
		}

		value := peer[valueStart&127]
		if value == 0 {
			continue
		}

		var count uint64
		if value < uint64(len(counts)) {
			counts[value]++
			count = counts[value]
		} else {
			if overflow == nil {
				overflow = make(map[uint64]uint64)
			}

			overflow[value]++
			count = overflow[value]
		}

		if count <= bestCount {
			continue
		}

		bestCount = count
		bestValue = value
	}

	if bestValue == 0 {
		return
	}

	ownerFrame[dstStart&127] = bestValue
}
