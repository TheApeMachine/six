package cpu

import (
	"math/bits"
	"sort"
)

/*
Predicate condition codes are packed into instr[58:61] when the predicate
bit is set. The first six are classic comparisons against a 1-word threshold
into a canonical full-word mask (^0 / 0). Multi-word sources compare their
popcount witness; single-word sources compare the raw scalar so property
guards can use direct values. The last two are reductions that store a value
rather than a mask:

  - PredStorePopcnt: write popcount(A) as an integer scalar to dst[0].
    Lets `set X <- popcnt(Y)` collapse into one instruction without a
    separate accumulate-and-fold lowering.
  - PredAnyZero: write ^0 to dst[0] if any word in A is zero, else 0.
    Implements the legacy `any_zero(...)` primitive used by falsification
    / open-ended-generation programs.

When the predicate bit is clear, the same three bits are SrcB byte-rotation
metadata for truth-table instructions. That keeps alignment in the operand
read path instead of creating a semantic ALU operation.
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
	OpReduceZipfSelect    = 0x3
)

const (
	zipfIDWord        = 122
	zipfEpochWord     = 58
	zipfCommunityWord = 64
	zipfSurprisalWord = 68
	zipfWeightScale   = 1 << 48
)

const (
	// TargetShift selects the truth-table destination bucket.
	TargetShift = 53
	// TargetBBitShift is retained for tests and older hand-packed words
	// whose target field is the low bit of the two-bit target bucket.
	TargetBBitShift = TargetShift
	// TopologyShift holds the topology code (two bits): local, queue, hypercube, hypercube-per-peer.
	TopologyShift      = 55
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

const (
	TargetA = 0
	TargetB = 1
	TargetC = 2
)

const (
	TopoLocal            = 0
	TopoPopQueue         = 1
	TopoHypercube        = 2
	TopoHypercubePerPeer = 3
)

/*
evaluatePredicateCompare decides whether an ALU witness satisfies the packed
instruction threshold word for the active predicate. predCond names the
comparison operator taken from the three condition bits at predicate time;
witness is the measured lane value and threshold is the decoded comparand
word. Returns true when the relation holds and false when it does not (or
when predCond does not name a recognised comparison).

PredLT, PredLE, PredGT, PredGE impose the usual unsigned integer ordering;
PredEQ and PredNE test bitwise equality. Any other predCond value yields
false so predicate bodies stay defensive against corrupt encodings.
*/
func evaluatePredicateCompare(predCond uint64, witness, threshold uint64) bool {
	switch predCond {
	case PredLT:
		return witness < threshold
	case PredLE:
		return witness <= threshold
	case PredGT:
		return witness > threshold
	case PredGE:
		return witness >= threshold
	case PredEQ:
		return witness == threshold
	case PredNE:
		return witness != threshold
	default:
		return false
	}
}

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
) ([]uint64, [128]uint64, bool) {
	bQueueIdx := uint64(0)
	currentBIdx := uint64(0)
	var currentB *[128]uint64
	var stagedIdx []uint64
	var childFrame [128]uint64
	childActive := false
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

		target := (instr >> TargetShift) & 3
		topology := (instr >> 55) & 3
		predicate := (instr >> PredicateBitShift) & 1
		predCond := (instr >> PredicateCondShift) & 7
		bRotate := predCond
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
			case OpReduceZipfSelect:
				reduceZipfSelect(ownerFrame, community, communitySize, aStart, bStart, dstStart, maskStart)
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
			if topology == TopoHypercubePerPeer && communitySize > 0 {
				// Per-peer stage: every peer whose per-peer mask word is
				// non-zero gets queued for staging. The mask was written
				// by a preceding TopoHypercubePerPeer predicate in the
				// same gossip block.
				for peerIdx := uint64(0); peerIdx < communitySize; peerIdx++ {
					if peerIdx == ownerIdx {
						continue
					}

					peer := community[peerIdx]
					if peer == nil || peer[maskStart&127] == 0 {
						continue
					}

					stagedIdx = append(stagedIdx, peerIdx)
				}
			} else if currentB != nil {
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
		if target == TargetB {
			ptrDst = ptrB
		}
		if target == TargetC {
			ptrDst = &childFrame
		}

		if predicate == 1 && topology == TopoHypercubePerPeer && communitySize > 0 {
			// Per-peer predicate: evaluate the comparison once for every
			// peer using that peer's view of the source operand, and
			// write the resulting all-ones / all-zeros mask into the
			// peer's own dst slot (the per-peer scratch word the parser
			// retargeted the IfNode mask to). Body instructions in the
			// same gossip block read their per-peer mask from that slot
			// via maskStart, so the broadcast write is naturally gated.
			threshold := ownerFrame[bStart&127]

			for peerIdx := uint64(0); peerIdx < communitySize; peerIdx++ {
				if peerIdx == ownerIdx {
					continue
				}

				peer := community[peerIdx]
				if peer == nil {
					continue
				}

				witnessSrc := ownerFrame
				if srcAFromB == 1 {
					witnessSrc = peer
				}

				var perPop uint64
				for lane := uint64(0); lane < aSpan; lane++ {
					perPop += uint64(bits.OnesCount64(witnessSrc[(aStart+lane)&127]))
				}

				witness := perPop
				if aSpan == 1 {
					witness = witnessSrc[aStart&127]
				}

				hit := evaluatePredicateCompare(predCond, witness, threshold)

				var maskValue uint64
				if hit {
					maskValue = ^uint64(0)
				}

				peer[dstStart&127] = maskValue
			}

			continue
		}

		if predicate == 1 {
			guard := ownerFrame[maskStart]
			if target == TargetC && guard != 0 {
				childActive = true
			}

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

				hit := evaluatePredicateCompare(predCond, witness, threshold)

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
		hypercube := (topology == TopoHypercube || topology == TopoHypercubePerPeer) && communitySize > 0
		perPeerMask := topology == TopoHypercubePerPeer

		if hypercube && target == TargetB {
			for k := uint64(0); k < communitySize; k++ {
				if k == ownerIdx {
					continue
				}

				peer := community[k]
				if peer == nil {
					continue
				}

				// Per-peer mode reads its mask from the peer frame, so a
				// preceding TopoHypercubePerPeer predicate can gate the
				// broadcast write per peer instead of all-or-nothing.
				peerMask := mask
				if perPeerMask {
					peerMask = peer[maskStart&127]
				}

				for lane := uint64(0); lane < dstSpan; lane++ {
					wordA := ptrA[(aStart+(lane%aSpan))&127]
					wordB := rotatedWord(peer, bStart, bSpan, lane, bRotate)

					res := (wordA & wordB & m0) |
						(wordA & ^wordB & m1) |
						(^wordA & wordB & m2) |
						(^wordA & ^wordB & m3)

					dstIdx := (dstStart + lane) & 127
					prevDst := peer[dstIdx]
					peer[dstIdx] = (res & peerMask) | (prevDst & ^peerMask)
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

					wordB := rotatedWord(peer, bStart, bSpan, lane, bRotate)

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

		if target == TargetC && mask != 0 {
			childActive = true
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

	return stagedIdx, childFrame, childActive
}

func rotatedWord(frame *[128]uint64, start, span, lane, rotate uint64) uint64 {
	idx := lane % span
	word := frame[(start+idx)&127]

	if rotate == 0 {
		return word
	}

	shift := rotate * 8
	next := frame[(start+((idx+1)%span))&127]

	return (word >> shift) | (next << (64 - shift))
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

/*
zipfCandidateRank is one non-zero Zipf candidate after community filtering.
index is the peer offset in the gossip community slice, utility is the
word used for lexicographic ranking, and value is the candidate payload
(program id or other non-zero lane value) chosen when this entry wins.
*/
type zipfCandidateRank struct {
	index   uint64
	utility uint64
	value   uint64
}

/*
sortedZipfCandidates gathers Zipf-select inputs from community[0:communitySize].
Nil peers are ignored; zero valueStart lane words are ignored because they
do not consume rank mass. Remaining peers are sorted by utility descending
with stable ascending index order on ties so hardware and Go agree on the
ranking ladder.
*/
func sortedZipfCandidates(
	community []*[128]uint64,
	communitySize uint64,
	valueStart uint64,
	utilityStart uint64,
) []zipfCandidateRank {
	var candidates []zipfCandidateRank

	for idx := uint64(0); idx < communitySize; idx++ {
		peer := community[idx]
		if peer == nil {
			continue
		}

		value := peer[valueStart&127]
		if value == 0 {
			continue
		}

		candidates = append(candidates, zipfCandidateRank{
			index:   idx,
			utility: peer[utilityStart&127],
			value:   value,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].utility != candidates[j].utility {
			return candidates[i].utility > candidates[j].utility
		}

		return candidates[i].index < candidates[j].index
	})

	return candidates
}

func reduceZipfSelect(
	ownerFrame *[128]uint64,
	community []*[128]uint64,
	communitySize uint64,
	valueStart uint64,
	utilityStart uint64,
	dstStart uint64,
	temperatureStart uint64,
) {
	if ownerFrame == nil || communitySize == 0 {
		return
	}

	candidates := sortedZipfCandidates(community, communitySize, valueStart, utilityStart)
	if len(candidates) == 0 {
		return
	}

	bestValue := candidates[0].value
	bestUtility := candidates[0].utility
	count := len(candidates)

	temperature := ownerFrame[temperatureStart&127]
	if temperature == 0 || count == 1 {
		ownerFrame[dstStart&127] = bestValue

		return
	}

	power := zipfPower(temperature)
	total := uint64(0)

	for rank := 1; rank <= count; rank++ {
		total += zipfWeight(uint64(rank), power)
	}

	if total == 0 {
		ownerFrame[dstStart&127] = bestValue

		return
	}

	seed := zipfSeed(ownerFrame, uint64(count), bestUtility)
	ticket := seed % total
	running := uint64(0)

	for rank := 1; rank <= count; rank++ {
		running += zipfWeight(uint64(rank), power)
		if ticket >= running {
			continue
		}

		ownerFrame[dstStart&127] = candidates[rank-1].value

		return
	}

	ownerFrame[dstStart&127] = bestValue
}

func zipfPower(temperature uint64) uint64 {
	switch {
	case temperature >= 1024:
		return 0
	case temperature >= 512:
		return 1
	case temperature >= 256:
		return 2
	case temperature >= 128:
		return 3
	default:
		return 4
	}
}

func zipfWeight(rank uint64, power uint64) uint64 {
	if rank == 0 {
		return 0
	}

	if power == 0 {
		return 1
	}

	weight := uint64(zipfWeightScale)
	for idx := uint64(0); idx < power; idx++ {
		weight /= rank
		if weight == 0 {
			return 1
		}
	}

	return weight
}

func zipfSeed(ownerFrame *[128]uint64, count uint64, bestUtility uint64) uint64 {
	seed := ownerFrame[zipfIDWord] ^
		bits.RotateLeft64(ownerFrame[zipfEpochWord], 17) ^
		bits.RotateLeft64(ownerFrame[zipfCommunityWord], 31) ^
		bits.RotateLeft64(ownerFrame[zipfSurprisalWord], 7) ^
		bits.RotateLeft64(bestUtility, 43) ^
		count

	return zipfMix(seed)
}

func zipfMix(seed uint64) uint64 {
	seed += 0x9e3779b97f4a7c15
	seed = (seed ^ (seed >> 30)) * 0xbf58476d1ce4e5b9
	seed = (seed ^ (seed >> 27)) * 0x94d049bb133111eb

	return seed ^ (seed >> 31)
}
