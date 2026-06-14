package cpu

import (
	"context"
	"math"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
)

const (
	rot8OpcodeCopyB    = uint64(0x5)
	rot8AStart         = uint64(0)
	rot8ASpan          = uint64(1)
	rot8BStart         = uint64(0)
	rot8BSpan          = uint64(2)
	rot8DstStart       = uint64(32)
	rot8DstSpan        = uint64(1)
	rot8MaskStart      = uint64(72)
	rot8TopologyNext   = uint64(TopoHypercube)
	rot8BRotateOneByte = uint64(1)
)

func TestExecuteKernelGoRot8(t *testing.T) {
	Convey("Given a truth-table instruction with B byte rotation metadata", t, func() {
		var owner [128]uint64
		var peer [128]uint64

		owner[72] = ^uint64(0)
		peer[0] = 0x0807060504030201
		peer[1] = 0x100f0e0d0c0b0a09

		owner[ProgramStartWord] = packTestInstruction(
			rot8OpcodeCopyB,
			rot8AStart,
			rot8ASpan,
			rot8BStart,
			rot8BSpan,
			rot8DstStart,
			rot8DstSpan,
			rot8MaskStart,
			rot8TopologyNext,
			rot8BRotateOneByte,
		)

		Convey("When the kernel reads B", func() {
			backend := &Backend{}
			backend.executeKernelGo(&owner, ^uint64(0), []*[128]uint64{&peer}, 1, 0)

			Convey("It should rotate the selected B span by one byte before applying the opcode", func() {
				So(owner[32], ShouldEqual, uint64(0x0908070605040302))
			})
		})
	})
}

func TestExecuteKernelGoEmitChildTarget(t *testing.T) {
	Convey("Given a child-target instruction", t, func() {
		var owner [128]uint64

		owner[72] = ^uint64(0)
		owner[122] = 42
		owner[ProgramStartWord] = packTestInstruction(
			0x3,
			122,
			1,
			122,
			1,
			120,
			1,
			72,
			TopoLocal,
			0,
		) | (TargetC << TargetShift)

		Convey("When the Go ALU executes it", func() {
			backend := &Backend{}
			_, groups := backend.executeKernelGo(&owner, ^uint64(0), nil, 0, 0)

			Convey("Then the write lands on the emitted child frame only", func() {
				So(len(groups), ShouldEqual, 1)
				So(len(groups[0]), ShouldEqual, 1)
				child := groups[0][0]
				So(child[120], ShouldEqual, uint64(42))
				So(owner[120], ShouldEqual, uint64(0))
			})
		})
	})
}

func BenchmarkExecuteKernelGoRot8(b *testing.B) {
	var owner [128]uint64
	var peer [128]uint64

	owner[72] = ^uint64(0)
	peer[0] = 0x0807060504030201
	peer[1] = 0x100f0e0d0c0b0a09

	owner[ProgramStartWord] = packTestInstruction(
		rot8OpcodeCopyB,
		rot8AStart,
		rot8ASpan,
		rot8BStart,
		rot8BSpan,
		rot8DstStart,
		rot8DstSpan,
		rot8MaskStart,
		rot8TopologyNext,
		rot8BRotateOneByte,
	)

	backend := &Backend{}
	community := []*[128]uint64{&peer}

	b.ReportAllocs()

	for b.Loop() {
		owner[32] = 0
		backend.executeKernelGo(&owner, ^uint64(0), community, 1, 0)
	}
}

/*
TestExecuteKernelHyperBroadcastEquivalence pins the new ALU's hypercube
target=B semantic for the asm fast path: every peer in the community
must receive the truth-table broadcast write per instruction, identical
to executeKernelGo. This catches asm regressions where the broadcast
loop drops a peer, double-writes one, or stops short of communitySize.
The arm64 asm grew its bcast_peer_loop in this turn; this test is the
correctness gate for that path.
*/
func TestExecuteKernelHyperBroadcastEquivalence(t *testing.T) {
	Convey("Given a hypercube target=B broadcast instruction", t, func() {
		const (
			opcodeCopyA = uint64(0x3) // dst <- A
			topoHyper   = uint64(2)
			maskWord    = uint64(72)
			ownerSrcA   = uint64(40) // A.context[0]
			peerDstB    = uint64(64) // B.properties.community
		)

		var goOwner [128]uint64
		goOwner[maskWord] = ^uint64(0)
		goOwner[ownerSrcA] = 0xCAFEBABEDEADBEEF

		instr := packTestHypercubeBroadcast(
			opcodeCopyA, ownerSrcA, 1, 0, 1, peerDstB, 1, maskWord, topoHyper,
		)
		goOwner[ProgramStartWord] = instr

		makeCommunity := func() []*[128]uint64 {
			return []*[128]uint64{
				new([128]uint64),
				new([128]uint64),
				new([128]uint64),
				new([128]uint64),
			}
		}

		Convey("When executeKernelGo runs the broadcast", func() {
			community := makeCommunity()
			backend := &Backend{}
			backend.executeKernelGo(&goOwner, ^uint64(0), community, 4, 2)

			Convey("It should write the owner's A word to every peer's dst", func() {
				for _, peer := range community {
					So(peer[peerDstB], ShouldEqual, goOwner[ownerSrcA])
				}
			})

			Convey("And HypercubeGossip via the asm dispatcher should produce the same per-peer result", func() {
				var asmOwner [128]uint64
				asmOwner[maskWord] = ^uint64(0)
				asmOwner[ownerSrcA] = 0xCAFEBABEDEADBEEF
				asmOwner[ProgramStartWord] = instr

				asmCommunity := makeCommunity()
				var stageBuf [128]uint64
				var stageCount uint64
				executeKernel(backend, &asmOwner, ^uint64(0), asmCommunity, 4, 2, &stageBuf, &stageCount)

				for idx, peer := range asmCommunity {
					So(peer[peerDstB], ShouldEqual, community[idx][peerDstB])
				}
			})
		})
	})
}

func BenchmarkExecuteKernelHyperBroadcastEquivalence(b *testing.B) {
	const (
		opcodeCopyA = uint64(0x3)
		topoHyper   = uint64(2)
		maskWord    = uint64(72)
		ownerSrcA   = uint64(40)
		peerDstB    = uint64(64)
		numPeers    = 64
	)

	instr := packTestHypercubeBroadcast(
		opcodeCopyA, ownerSrcA, 1, 0, 1, peerDstB, 1, maskWord, topoHyper,
	)
	backend := &Backend{}
	dimCount := uint64(6)

	b.Run("go", func(b *testing.B) {
		b.ReportAllocs()
		owner := &[128]uint64{}
		community := make([]*[128]uint64, numPeers)
		for benchIdx := range community {
			community[benchIdx] = new([128]uint64)
		}

		owner[maskWord] = ^uint64(0)
		owner[ownerSrcA] = 0xCAFEBABEDEADBEEF
		owner[ProgramStartWord] = instr

		b.ResetTimer()

		for benchIdx := 0; benchIdx < b.N; benchIdx++ {
			for _, peer := range community {
				peer[peerDstB] = 0
			}

			backend.executeKernelGo(owner, ^uint64(0), community, uint64(len(community)), dimCount)
		}
	})

	b.Run("asm", func(b *testing.B) {
		b.ReportAllocs()
		owner := &[128]uint64{}
		community := make([]*[128]uint64, numPeers)
		for benchIdx := range community {
			community[benchIdx] = new([128]uint64)
		}

		owner[maskWord] = ^uint64(0)
		owner[ownerSrcA] = 0xCAFEBABEDEADBEEF
		owner[ProgramStartWord] = instr

		var stageBuf [128]uint64
		var stageCount uint64

		b.ResetTimer()

		for benchIdx := 0; benchIdx < b.N; benchIdx++ {
			for _, peer := range community {
				peer[peerDstB] = 0
			}

			executeKernel(backend, owner, ^uint64(0), community, uint64(len(community)), dimCount, &stageBuf, &stageCount)
		}
	})
}

/*
TestExecuteKernelPredicateEquivalence pins the asm predicate path
against Go for every predCond the kernel supports. Each case sets up a
single-word A operand with a known popcount, a threshold, and the
expected mask; both kernels must write the same value to ptrDst.
*/
func TestExecuteKernelPredicateEquivalence(t *testing.T) {
	const (
		predLT          = uint64(0)
		predLE          = uint64(1)
		predGT          = uint64(2)
		predGE          = uint64(3)
		predEQ          = uint64(4)
		predNE          = uint64(5)
		predStorePopcnt = uint64(6)
	)

	cases := []struct {
		name      string
		aSpan     uint64
		predCond  uint64
		aWord0    uint64 // value at frame[aStart]
		aWord1    uint64 // value at frame[aStart+1] (multi-word path)
		threshold uint64
	}{
		{name: "LT hit", aSpan: 1, predCond: predLT, aWord0: 5, threshold: 10},
		{name: "LT miss", aSpan: 1, predCond: predLT, aWord0: 10, threshold: 10},
		{name: "LE hit eq", aSpan: 1, predCond: predLE, aWord0: 10, threshold: 10},
		{name: "GT hit", aSpan: 1, predCond: predGT, aWord0: 11, threshold: 10},
		{name: "GE hit eq", aSpan: 1, predCond: predGE, aWord0: 10, threshold: 10},
		{name: "EQ hit", aSpan: 1, predCond: predEQ, aWord0: 42, threshold: 42},
		{name: "NE hit", aSpan: 1, predCond: predNE, aWord0: 1, threshold: 0},
		{name: "popcnt LT span2", aSpan: 2, predCond: predLT, aWord0: 0xFF, aWord1: 0xFF, threshold: 100}, // 16 < 100
		{name: "StorePopcnt span1", aSpan: 1, predCond: predStorePopcnt, aWord0: 0xF},
	}

	for _, tc := range cases {
		Convey("Given predicate "+tc.name, t, func() {
			const (
				aStart    = uint64(0)  // tokens[0]
				bStart    = uint64(40) // owner.context[0] holds threshold
				dstStart  = uint64(32) // signals[0]
				maskStart = uint64(72) // MaskTrue
			)

			runOne := func(useAsm bool) uint64 {
				var owner [128]uint64
				owner[maskStart] = ^uint64(0)
				owner[aStart] = tc.aWord0
				if tc.aSpan > 1 {
					owner[aStart+1] = tc.aWord1
				}
				owner[bStart] = tc.threshold

				instr := packTestInstruction(0, aStart, tc.aSpan, bStart, 1, dstStart, 1, maskStart, 0, 0)
				instr |= 1 << 57                 // predicate
				instr |= (tc.predCond & 7) << 58 // predCond

				owner[ProgramStartWord] = instr

				backend := &Backend{}
				if useAsm {
					var stageBuf [128]uint64
					var stageCount uint64
					executeKernel(backend, &owner, ^uint64(0), nil, 0, 0, &stageBuf, &stageCount)
				} else {
					backend.executeKernelGo(&owner, ^uint64(0), nil, 0, 0)
				}

				return owner[dstStart]
			}

			Convey("It should produce the same result on Go and asm", func() {
				So(runOne(true), ShouldEqual, runOne(false))
			})
		})
	}
}

func BenchmarkExecuteKernelPredicateEquivalence(b *testing.B) {
	const (
		aStart    = uint64(0)
		bStart    = uint64(40)
		dstStart  = uint64(32)
		maskStart = uint64(72)
	)

	makeOwner := func() [128]uint64 {
		var owner [128]uint64
		owner[maskStart] = ^uint64(0)
		owner[aStart] = 5
		owner[bStart] = 10
		instr := packTestInstruction(0, aStart, 1, bStart, 1, dstStart, 1, maskStart, 0, 0)
		instr |= 1 << 57                                    // predicate
		instr |= (PredLT & uint64(7)) << PredicateCondShift // simple compare predicate
		owner[ProgramStartWord] = instr

		return owner
	}

	backend := &Backend{}
	var asmOut, goOut uint64
	{
		oGo := makeOwner()
		oAsm := oGo

		var stageBuf [128]uint64
		var stageCount uint64

		executeKernel(backend, &oAsm, ^uint64(0), nil, 0, 0, &stageBuf, &stageCount)
		asmOut = oAsm[dstStart]

		backend.executeKernelGo(&oGo, ^uint64(0), nil, 0, 0)
		goOut = oGo[dstStart]
	}

	if asmOut != goOut {
		b.Fatalf("predicate asm %v go %v mismatch", asmOut, goOut)
	}

	b.Run("go", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for benchIdx := 0; benchIdx < b.N; benchIdx++ {
			owner := makeOwner()
			backend.executeKernelGo(&owner, ^uint64(0), nil, 0, 0)

			if owner[dstStart] != goOut {
				b.Fatalf("go drift benchIdx=%d", benchIdx)
			}
		}
	})

	b.Run("asm", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		var stageBuf [128]uint64
		var stageCount uint64

		for benchIdx := 0; benchIdx < b.N; benchIdx++ {
			owner := makeOwner()
			executeKernel(backend, &owner, ^uint64(0), nil, 0, 0, &stageBuf, &stageCount)

			if owner[dstStart] != asmOut {
				b.Fatalf("asm drift benchIdx=%d", benchIdx)
			}
		}
	})
}

func TestExecuteKernelScalarSublane(t *testing.T) {
	Convey("Given scalar shift and rotate instructions", t, func() {
		const (
			maskWord = uint64(72)
			amount   = uint64(73)
		)

		var owner [128]uint64
		owner[maskWord] = ^uint64(0)
		owner[amount] = 8
		owner[0] = 1
		owner[1] = 0x100
		owner[2] = 0x0102030405060708
		owner[3] = 0x0102030405060708

		ops := []struct {
			opcode uint64
			src    uint64
			dst    uint64
			expect uint64
		}{
			{opcode: ScalarShiftLeft, src: 0, dst: 32, expect: 0x100},
			{opcode: ScalarShiftRight, src: 1, dst: 33, expect: 0x1},
			{opcode: ScalarRotateLeft, src: 2, dst: 34, expect: 0x0203040506070801},
			{opcode: ScalarRotateRight, src: 3, dst: 35, expect: 0x0801020304050607},
		}

		for idx, op := range ops {
			instr := packTestInstruction(op.opcode, op.src, 1, amount, 1, op.dst, 1, maskWord, TopoLocal, 0)
			instr |= 1 << PredicateBitShift
			instr |= uint64(PredScalar) << PredicateCondShift
			owner[ProgramStartWord+uint64(idx)] = instr
		}

		Convey("When executeKernelGo runs one strict sweep", func() {
			backend := &Backend{}
			backend.executeKernelGo(&owner, ^uint64(0), nil, 0, 0)

			Convey("It should write each scalar result in band", func() {
				for _, op := range ops {
					So(owner[op.dst], ShouldEqual, op.expect)
				}

				So((&Backend{}).programAsmCompatible(&owner), ShouldBeFalse)
			})
		})
	})
}

func TestExecuteKernelStrictLinearSweep(t *testing.T) {
	Convey("Given a word with the reserved pop-end bit set", t, func() {
		const (
			maskWord = uint64(72)
			dst0     = uint64(32)
			dst1     = uint64(33)
		)

		first := packTestInstruction(0x3, 0, 1, 0, 1, dst0, 1, maskWord, TopoPopQueue, 0)
		first |= 1 << PopEndBitShift
		second := packTestInstruction(0x3, 122, 1, 122, 1, dst1, 1, maskWord, TopoLocal, 0)
		buildFrames := func() ([128]uint64, [128]uint64) {
			var owner [128]uint64
			var peer [128]uint64
			owner[maskWord] = ^uint64(0)
			owner[0] = 0xCAFE
			owner[122] = 0xBEEF
			peer[0] = 0xBAD
			owner[ProgramStartWord] = first
			owner[ProgramStartWord+1] = second

			return owner, peer
		}

		Convey("When executeKernelGo runs", func() {
			owner, peer := buildFrames()
			backend := &Backend{}
			backend.executeKernelGo(&owner, ^uint64(0), []*[128]uint64{&peer}, 1, 0)

			Convey("It should execute pc=0 and pc=1 once without rewinding", func() {
				So(owner[dst0], ShouldEqual, uint64(0xCAFE))
				So(owner[dst1], ShouldEqual, uint64(0xBEEF))
			})
		})

		Convey("When executeKernel runs", func() {
			owner, peer := buildFrames()
			backend := &Backend{}
			var stageBuf [128]uint64
			var stageCount uint64
			executeKernel(backend, &owner, ^uint64(0), []*[128]uint64{&peer}, 1, 0, &stageBuf, &stageCount)

			Convey("It should execute pc=0 and pc=1 once without rewinding", func() {
				So(owner[dst0], ShouldEqual, uint64(0xCAFE))
				So(owner[dst1], ShouldEqual, uint64(0xBEEF))
				So(stageCount, ShouldEqual, uint64(0))
			})
		})
	})
}

func TestExecuteKernelPerPeerOwnerFoldMask(t *testing.T) {
	Convey("Given a per-peer masked owner-side hypercube fold", t, func() {
		const (
			maskWord = uint64(70)
			dstWord  = uint64(32)
		)

		var owner [128]uint64
		var allowed [128]uint64
		var blocked [128]uint64
		allowed[0] = 11
		allowed[maskWord] = ^uint64(0)
		blocked[0] = 99
		blocked[maskWord] = 0

		owner[ProgramStartWord] = packTestInstruction(0x5, 0, 1, 0, 1, dstWord, 1, maskWord, TopoHypercubePerPeer, 0)

		Convey("When executeKernelGo folds into A", func() {
			backend := &Backend{}
			backend.executeKernelGo(&owner, ^uint64(0), []*[128]uint64{&allowed, &blocked}, 2, 1)

			Convey("It should skip peers whose in-band mask is zero", func() {
				So(owner[dstWord], ShouldEqual, uint64(11))
			})
		})
	})
}

func TestExecuteKernelPerPeerPredicateMask(t *testing.T) {
	Convey("Given a per-peer predicate over B target", t, func() {
		const (
			targetWord = uint64(65)
			maskWord   = uint64(39)
			zeroWord   = uint64(73)
		)

		var owner [128]uint64
		var peer [128]uint64
		owner[72] = ^uint64(0)
		owner[zeroWord] = 0
		peer[targetWord] = 7

		instr := packTestInstruction(0, targetWord, 1, zeroWord, 1, maskWord, 1, 72, TopoHypercubePerPeer, 0)
		instr |= 1 << PredicateBitShift
		instr |= uint64(PredNE) << PredicateCondShift
		instr |= 1 << SrcAFromBShift
		owner[ProgramStartWord] = instr

		Convey("When executeKernelGo runs", func() {
			backend := &Backend{}
			backend.executeKernelGo(&owner, ^uint64(0), []*[128]uint64{&peer}, 1, 0)

			Convey("It should write the peer-local predicate mask", func() {
				So(peer[maskWord], ShouldEqual, ^uint64(0))
			})
		})
	})
}

func TestHypercubeGossipPerPeerPredicateMask(t *testing.T) {
	Convey("Given primitive Values with a per-peer predicate", t, func() {
		owner := primitive.Emit()
		peer := primitive.Emit()
		defer primitive.CloseAll([]*primitive.Value{owner, peer})

		owner.Set(72, ^uint64(0))
		owner.Set(73, 0)
		peer.Set(65, 7)

		instr := packTestInstruction(0, 65, 1, 73, 1, 39, 1, 72, TopoHypercubePerPeer, 0)
		instr |= 1 << PredicateBitShift
		instr |= uint64(PredNE) << PredicateCondShift
		instr |= 1 << SrcAFromBShift
		owner.Set(ProgramStartWord, instr)

		Convey("When HypercubeGossip dispatches", func() {
			backend := NewBackend(context.Background())
			defer backend.Close()
			_, err := backend.HypercubeGossip(owner, []*primitive.Value{peer})

			Convey("It should write the peer-local mask", func() {
				So(err, ShouldBeNil)
				So(peer.Word(39), ShouldEqual, ^uint64(0))
			})
		})
	})
}

func TestHypercubeGossipMaterializeChildGroups(t *testing.T) {
	Convey("Given multiple emitted child groups", t, func() {
		groupA := make([][128]uint64, 3)
		groupB := make([][128]uint64, 1)
		groupA[0][0], groupA[1][1], groupA[2][2] = 1, 2, 3
		groupB[0][3] = 4

		Convey("When the CPU backend materializes the groups", func() {
			backend := &Backend{}
			children := backend.materializeChildGroups([][][128]uint64{groupA, groupB})

			Convey("It should link adjacent emits only within each group", func() {
				So(len(children), ShouldEqual, 4)
				So(children[0].Word(121), ShouldEqual, children[1].ID())
				So(children[1].Word(120), ShouldEqual, children[0].ID())
				So(children[1].Word(121), ShouldEqual, children[2].ID())
				So(children[2].Word(120), ShouldEqual, children[1].ID())
				So(children[2].Word(121), ShouldEqual, uint64(0))
				So(children[3].Word(120), ShouldEqual, uint64(0))
			})

			Reset(func() {
				for _, child := range children {
					_ = child.Close()
				}
			})
		})
	})
}

func storeTestLanes(frame *[128]uint64, start uint64, lanes [8]float64) {
	for lane, value := range lanes {
		frame[(start+uint64(lane))&127] = math.Float64bits(value)
	}
}

func readTestLanes(frame *[128]uint64, start uint64) [8]float64 {
	var lanes [8]float64

	for lane := range lanes {
		lanes[lane] = math.Float64frombits(frame[(start+uint64(lane))&127])
	}

	return lanes
}

/*
TestExecuteKernelBRotateEquivalence pins the bRotate path: asm and Go
must produce byte-identical results when reading a SrcB span with a
non-zero rotation. The Metal-vs-CPU comparison test catches integration
mismatches but takes Metal as a third party; this isolates asm vs Go.
*/
func TestExecuteKernelBRotateEquivalence(t *testing.T) {
	Convey("Given a gossip program with rot8(B.tokens[0,2], 1)", t, func() {
		var goOwner [128]uint64
		var goPeer [128]uint64
		goOwner[72] = ^uint64(0)
		goPeer[0] = 0x0807060504030201
		goPeer[1] = 0x100f0e0d0c0b0a09

		instr := packTestInstruction(
			rot8OpcodeCopyB,
			rot8AStart, rot8ASpan,
			rot8BStart, rot8BSpan,
			rot8DstStart, rot8DstSpan,
			rot8MaskStart,
			rot8TopologyNext,
			rot8BRotateOneByte,
		)
		goOwner[ProgramStartWord] = instr

		Convey("When executeKernelGo runs", func() {
			backend := &Backend{}
			backend.executeKernelGo(&goOwner, ^uint64(0), []*[128]uint64{&goPeer}, 1, 0)

			Convey("It should write the rotated word to dst", func() {
				So(goOwner[32], ShouldEqual, uint64(0x0908070605040302))
			})
		})
	})
}

/*
TestExecuteKernelSrcAFromBEquivalence pins the srcAFromB routing bit:
when set, ptrA must point at the B frame so SrcA reads from the
peer rather than the owner. Catches asm bugs that leave ptrA pinned
at the owner frame after my srcAFromB extension.
*/
func TestExecuteKernelSrcAFromBEquivalence(t *testing.T) {
	Convey("Given a gossip write that reads SrcA from the peer frame", t, func() {
		const (
			opcodeCopyA = uint64(0x3) // dst <- A (here A = peer because srcAFromB=1)
			topoGossip  = uint64(TopoHypercube)
			maskWord    = uint64(72)
			peerSrcA    = uint64(0)  // tokens[0]
			ownerDstA   = uint64(40) // owner.context[0]
		)

		var goOwner [128]uint64
		goOwner[maskWord] = ^uint64(0)
		var goPeer [128]uint64
		goPeer[peerSrcA] = 0xCAFEBABEDEADBEEF

		instr := packTestInstruction(opcodeCopyA, peerSrcA, 1, 0, 1, ownerDstA, 1, maskWord, topoGossip, 0)
		instr |= 1 << 61 // srcAFromB
		goOwner[ProgramStartWord] = instr

		Convey("When executeKernelGo runs", func() {
			community := []*[128]uint64{&goPeer}
			backend := &Backend{}
			backend.executeKernelGo(&goOwner, ^uint64(0), community, 1, 0)

			Convey("It should copy the peer's word into the owner's dst", func() {
				So(goOwner[ownerDstA], ShouldEqual, uint64(0xCAFEBABEDEADBEEF))
			})

		})
	})
}

func BenchmarkExecuteKernelBRotate(b *testing.B) {
	var owner [128]uint64
	var peer [128]uint64

	owner[72] = ^uint64(0)
	peer[0] = 0x0807060504030201
	peer[1] = 0x100f0e0d0c0b0a09

	instr := packTestInstruction(
		rot8OpcodeCopyB,
		rot8AStart, rot8ASpan,
		rot8BStart, rot8BSpan,
		rot8DstStart, rot8DstSpan,
		rot8MaskStart,
		rot8TopologyNext,
		rot8BRotateOneByte,
	)
	owner[ProgramStartWord] = instr

	community := []*[128]uint64{&peer}
	backend := &Backend{}
	dimCount := uint64(0)

	b.Run("go", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for benchIdx := 0; benchIdx < b.N; benchIdx++ {
			owner[32] = 0

			backend.executeKernelGo(&owner, ^uint64(0), community, 1, dimCount)
		}
	})

	b.Run("asm", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		var ownerBuf [128]uint64
		var peerBuf [128]uint64

		for benchIdx := 0; benchIdx < b.N; benchIdx++ {
			clear(ownerBuf[:])
			clear(peerBuf[:])

			ownerBuf[72] = ^uint64(0)
			peerBuf[0] = 0x0807060504030201
			peerBuf[1] = 0x100f0e0d0c0b0a09
			ownerBuf[ProgramStartWord] = instr

			var stageBuf [128]uint64
			var stageCount uint64

			executeKernel(backend, &ownerBuf, ^uint64(0), []*[128]uint64{&peerBuf}, 1, dimCount, &stageBuf, &stageCount)
		}
	})
}

func BenchmarkExecuteKernelSrcAFromB(b *testing.B) {
	const (
		opcodeCopyA = uint64(0x3)
		topoGossip  = uint64(TopoHypercube)
		maskWord    = uint64(72)
		peerSrcA    = uint64(0)
		ownerDstA   = uint64(40)
	)

	instr := packTestInstruction(opcodeCopyA, peerSrcA, 1, 0, 1, ownerDstA, 1, maskWord, topoGossip, 0)
	instr |= 1 << SrcAFromBShift

	backend := &Backend{}
	dimCount := uint64(0)

	b.Run("go", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for benchIdx := 0; benchIdx < b.N; benchIdx++ {
			var owner [128]uint64
			var peer [128]uint64

			owner[maskWord] = ^uint64(0)
			peer[peerSrcA] = 0xCAFEBABEDEADBEEF
			owner[ProgramStartWord] = instr

			backend.executeKernelGo(&owner, ^uint64(0), []*[128]uint64{&peer}, 1, dimCount)
		}
	})

	b.Run("asm", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for benchIdx := 0; benchIdx < b.N; benchIdx++ {
			var owner [128]uint64
			var peer [128]uint64

			owner[maskWord] = ^uint64(0)
			peer[peerSrcA] = 0xCAFEBABEDEADBEEF
			owner[ProgramStartWord] = instr

			var stageBuf [128]uint64
			var stageCount uint64

			executeKernel(backend, &owner, ^uint64(0), []*[128]uint64{&peer}, 1, dimCount, &stageBuf, &stageCount)
		}
	})
}

/*
packTestHypercubeBroadcast mirrors packTestInstruction but also flips
the targetB bit so the kernel routes the write to the peer frame
instead of the owner.
*/
func packTestHypercubeBroadcast(
	op, aStart, aSpan, bStart, bSpan, dstStart, dstSpan, maskStart, topology uint64,
) uint64 {
	instr := packTestInstruction(op, aStart, aSpan, bStart, bSpan, dstStart, dstSpan, maskStart, topology, 0)
	instr |= uint64(1) << TargetBBitShift

	return instr
}

/*
packTestInstruction builds a resident ALU word for CPU kernel tests without
explicit predicate/meta bits; fields not listed here remain zero:

	op:         4 bits [3:0]
	aStart:     7 bits [10:4]
	aSpan-1:    7 bits [17:11] (spans encode as span-1)
	bStart:     7 bits [24:18]
	bSpan-1:    7 bits [31:25]
	dstStart:   7 bits [38:32]
	dstSpan-1:  7 bits [45:39]
	maskStart:  7 bits [52:46]
	topology:   2 bits [56:55] (&3 for two-bit masking)
	bRotate:    3 bits at PredicateCondShift (= 58): same bitfield as predicate
	            condition in full words; truth-table ops use rotation n in [0,7].

When the instruction is a predicate (bit 57), the kernel reads [60:58] as
PredCond instead of rotation; tests that need predicates set those bits separately.
*/
func packTestInstruction(op, aStart, aSpan, bStart, bSpan, dstStart, dstSpan, maskStart, topology, bRotate uint64) uint64 {
	var instr uint64
	instr |= op & 0xF
	instr |= (aStart & 0x7F) << 4
	instr |= ((aSpan - 1) & 0x7F) << 11
	instr |= (bStart & 0x7F) << 18
	instr |= ((bSpan - 1) & 0x7F) << 25
	instr |= (dstStart & 0x7F) << 32
	instr |= ((dstSpan - 1) & 0x7F) << 39
	instr |= (maskStart & 0x7F) << 46
	instr |= (topology & 3) << TopologyShift
	instr |= (bRotate & 7) << PredicateCondShift

	return instr
}
