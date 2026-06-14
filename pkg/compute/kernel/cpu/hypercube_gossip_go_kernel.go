package cpu

import (
	"math/bits"
	"unsafe"
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
  - PredScalar: run a generic scalar operation named by the opcode nibble.
    This sublane carries shifts and rotates without assigning a named
    reducer meaning to the instruction.

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
	PredScalar      = 7
)

const (
	ScalarShiftLeft   = 1
	ScalarShiftRight  = 2
	ScalarRotateLeft  = 3
	ScalarRotateRight = 4
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
	// SrcAFromBShift selects the mapped B frame as the SrcA pointer. It
	// makes "operate entirely on B" (e.g. write B.x <- xor(B.x, B.y))
	// expressible without reverse-engineering the operand routing.
	SrcAFromBShift = 61
	// StageBitShift and PopEndBitShift are reserved in strict mode. The
	// Go kernel ignores both so the sweep is exactly pc=0..15 once.
	StageBitShift  = 62
	PopEndBitShift = 63
)

/*
Target constants decode the 2-bit target selector in packed ALU words.
TargetA writes to the resident owner frame, TargetB writes to the mapped peer
frame, and TargetC writes to the emitted child frame staged by the Go kernel.
TargetMask isolates the selector after shifting by TargetShift.
*/
const (
	TargetMask = 3
	TargetA    = 0
	TargetB    = 1
	TargetC    = 2
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
) ([]uint64, [][][128]uint64) {
	var stagedIdx []uint64
	var childFrame [128]uint64
	var childGroup [][128]uint64
	var childGroups [][][128]uint64
	childActive := false
	flushChild := func() {
		if !childActive {
			return
		}

		childGroup = append(childGroup, childFrame)
		childFrame = [128]uint64{}
		childActive = false
	}
	flushGroup := func() {
		if len(childGroup) == 0 {
			return
		}

		childGroups = append(childGroups, childGroup)
		childGroup = nil
	}

	for pc := uint64(0); pc < ProgramWords; pc++ {
		instr := ownerFrame[ProgramStartWord+pc]
		if instr == 0 {
			continue
		}

		if backend.geometricSlot(instr) {
			GeometricFrame(unsafe.Pointer(ownerFrame), instr)
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
		emitEnd := stageBit == 1 && target == TargetC

		if stageBit == 1 && !emitEnd {
			continue
		}

		// Predicate and scalar paths use a single ptrB unless the instruction
		// explicitly sweeps in the hypercube block below. Pick a representative
		// peer here so scalar local reads have a stable B-side source.
		ptrB := ownerFrame
		if (topology == TopoHypercube || topology == TopoHypercubePerPeer) && dimCount > 0 {
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

		if predicate == 1 && predCond == PredScalar {
			mask := ownerFrame[maskStart&127]
			hypercube := (topology == TopoHypercube || topology == TopoHypercubePerPeer) && communitySize > 0
			perPeerMask := topology == TopoHypercubePerPeer

			if hypercube && target == TargetB {
				for peerIdx := uint64(0); peerIdx < communitySize; peerIdx++ {
					if peerIdx == ownerIdx {
						continue
					}

					peer := community[peerIdx]
					if peer == nil {
						continue
					}

					peerMask := mask
					if perPeerMask {
						peerMask = peer[maskStart&127]
					}

					for lane := uint64(0); lane < dstSpan; lane++ {
						srcFrame := ownerFrame
						if srcAFromB == 1 {
							srcFrame = peer
						}

						value := srcFrame[(aStart+(lane%aSpan))&127]
						amount := peer[bStart&127]
						result := scalarWord(opcode, value, amount)
						dstIdx := (dstStart + lane) & 127
						prevDst := peer[dstIdx]
						peer[dstIdx] = (result & peerMask) | (prevDst & ^peerMask)
					}
				}
			} else {
				for lane := uint64(0); lane < dstSpan; lane++ {
					value := ptrA[(aStart+(lane%aSpan))&127]
					amount := ptrB[(bStart+(lane%bSpan))&127]
					result := scalarWord(opcode, value, amount)
					dstIdx := (dstStart + lane) & 127
					prevDst := ptrDst[dstIdx]
					ptrDst[dstIdx] = (result & mask) | (prevDst & ^mask)
				}
			}

			if target == TargetC && mask != 0 {
				childActive = true
			}

			if emitEnd {
				flushChild()
			}

			continue
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

				guard := ^uint64(0)
				if maskStart != 72 {
					guard = peer[maskStart&127]
				}
				if guard == 0 {
					peer[dstStart&127] = 0
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

				if predCond == PredStorePopcnt {
					dstFrame := ownerFrame
					if target == TargetB {
						dstFrame = peer
					}
					if target == TargetC {
						dstFrame = &childFrame
						childActive = true
					}

					dstIdx := dstStart & 127
					prevDst := dstFrame[dstIdx]
					dstFrame[dstIdx] = (perPop & guard) | (prevDst & ^guard)
					continue
				}

				hit := evaluatePredicateCompare(predCond, witness, threshold)

				var maskValue uint64
				if hit {
					maskValue = ^uint64(0)
				}

				peer[dstStart&127] = maskValue & guard
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

			if emitEnd {
				flushChild()
			}

			continue
		}

		mask := ownerFrame[maskStart&127]

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
				if peerMask == 0 {
					continue
				}

				for lane := uint64(0); lane < dstSpan; lane++ {
					wordA := ptrA[(aStart+(lane%aSpan))&127]
					if srcAFromB == 1 {
						wordA = peer[(aStart+(lane%aSpan))&127]
					}
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
					if peer == nil {
						continue
					}
					if perPeerMask && peer[maskStart&127] == 0 {
						continue
					}

					wordA := acc
					if srcAFromB == 1 {
						wordA = peer[(aStart+(lane%aSpan))&127]
					}
					wordB := rotatedWord(peer, bStart, bSpan, lane, bRotate)

					acc = (wordA & wordB & m0) |
						(wordA & ^wordB & m1) |
						(^wordA & wordB & m2) |
						(^wordA & ^wordB & m3)
					any = true
				}

				if !any && !hypercube {
					acc = startA
				}
				if !any && hypercube {
					continue
				}

				writeMask := mask
				if perPeerMask {
					writeMask = ^uint64(0)
				}

				ptrDst[dstIdx] = (acc & writeMask) | (prevDst & ^writeMask)
			}
		}

		if target == TargetC && mask != 0 {
			childActive = true
		}

		if emitEnd {
			flushChild()
		}
	}

	flushChild()
	flushGroup()

	return stagedIdx, childGroups
}

/*
geometricSlot detects reserved geometric sweep words that bypass the 4-bit
truth-table decoder. Instructions 0x10, 0x20, and 0x30 align with
compiler.OpGeometricCompose, OpGeometricSandwich, and OpGeometricReverse; they
occupy an entire program slot as opaque 64-bit payloads. programAsmCompatible
uses this predicate to disable the asm fast path until geometric lowering
matches executeKernelGo.
*/
func (backend *Backend) geometricSlot(instr uint64) bool {
	_ = backend

	switch instr {
	case 0x10, 0x20, 0x30:
		return true
	default:
		return false
	}
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

func scalarWord(opcode, value, amount uint64) uint64 {
	shift := uint(amount & 63)

	switch opcode {
	case ScalarShiftLeft:
		return value << shift
	case ScalarShiftRight:
		return value >> shift
	case ScalarRotateLeft:
		return bits.RotateLeft64(value, int(shift))
	case ScalarRotateRight:
		return bits.RotateLeft64(value, -int(shift))
	default:
		return 0
	}
}
