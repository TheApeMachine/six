package cpu

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
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
	rot8TopologyNext   = uint64(1)
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
TestExecuteKernelStageEquivalence pins the stage(B) path: asm and Go
must record the same peer indices in their staged-output buffers when
a stage instruction fires inside a pop body.
*/
func TestExecuteKernelStageEquivalence(t *testing.T) {
	Convey("Given a pop(B) body that stages each popped peer", t, func() {
		const (
			topoPop  = uint64(1)
			maskWord = uint64(72)
		)

		// Three-instruction program: pop seed (CopyA identity), then
		// stage(B), then end. The stage instruction has popEnd=1 so the
		// kernel rewinds to drain the lane.
		const opcodeCopyA = uint64(3)
		seed := packTestInstruction(opcodeCopyA, 0, 1, 0, 1, 0, 1, maskWord, topoPop, 0)

		stageInstr := packTestInstruction(0, 0, 1, 0, 1, 0, 1, maskWord, topoPop, 0)
		stageInstr |= uint64(1) << StageBitShift
		stageInstr |= uint64(1) << PopEndBitShift

		var owner [128]uint64
		owner[maskWord] = ^uint64(0)
		owner[ProgramStartWord] = seed
		owner[ProgramStartWord+1] = stageInstr

		peer0 := new([128]uint64)
		peer1 := new([128]uint64)
		peer2 := new([128]uint64)
		community := []*[128]uint64{peer0, peer1, peer2}

		Convey("When executeKernelGo runs", func() {
			backend := &Backend{}
			goCopy := owner
			goStaged, _, _ := backend.executeKernelGo(&goCopy, ^uint64(0), community, 3, 2)

			Convey("And the asm path runs the same program", func() {
				asmCopy := owner
				var stageBuf [128]uint64
				var stageCount uint64
				executeKernel(backend, &asmCopy, ^uint64(0), community, 3, 2, &stageBuf, &stageCount)

				So(stageCount, ShouldEqual, uint64(len(goStaged)))
				for idx := uint64(0); idx < stageCount; idx++ {
					So(stageBuf[idx], ShouldEqual, goStaged[idx])
				}
			})
		})
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
		predAnyZero     = uint64(7)
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
		{name: "AnyZero hit", aSpan: 2, predCond: predAnyZero, aWord0: 0xFF, aWord1: 0},
		{name: "AnyZero miss", aSpan: 2, predCond: predAnyZero, aWord0: 0xFF, aWord1: 0xFF},
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

/*
TestExecuteKernelZipfSelect pins the candidate-program reducer. The zero
temperature path is intentionally greedy so empty temperature words do not
introduce stochastic behavior into firmware that has not opted into exploration.
*/
func TestExecuteKernelZipfSelect(t *testing.T) {
	Convey("Given ranked candidate program Values", t, func() {
		const (
			valueStart       = uint64(63) // properties.program_id
			utilityStart     = uint64(57) // properties.confidence
			dstStart         = uint64(63) // owner properties.program_id
			temperatureStart = uint64(60) // properties.temperature
		)

		var owner [128]uint64
		var peer0 [128]uint64
		var peer1 [128]uint64
		var peer2 [128]uint64

		peer0[valueStart] = 11
		peer0[utilityStart] = 30
		peer1[valueStart] = 22
		peer1[utilityStart] = 90
		peer2[valueStart] = 33
		peer2[utilityStart] = 10

		instr := packTestInstruction(
			OpReduceZipfSelect,
			valueStart, 1,
			utilityStart, 1,
			dstStart, 1,
			temperatureStart,
			TopoHypercube,
			0,
		)
		instr |= 1 << PredicateBitShift
		instr |= uint64(PredEQ) << PredicateCondShift
		instr |= 1 << SrcAFromBShift

		owner[ProgramStartWord] = instr

		Convey("When temperature is zero", func() {
			backend := &Backend{}
			community := []*[128]uint64{&peer0, &peer1, &peer2}
			backend.executeKernelGo(&owner, ^uint64(0), community, uint64(len(community)), 2)

			Convey("It should select the highest utility non-zero program id", func() {
				So(owner[dstStart], ShouldEqual, uint64(22))
			})
		})

		Convey("And temperature is non-zero", func() {
			goOwner := owner
			goOwner[zipfIDWord] = 0x12345678
			goOwner[zipfEpochWord] = 7
			goOwner[zipfCommunityWord] = 0x55
			goOwner[zipfSurprisalWord] = 0xAA
			goOwner[temperatureStart] = 256

			asmOwner := goOwner
			goCommunity := []*[128]uint64{&peer0, &peer1, &peer2}

			var asmPeer0 [128]uint64
			var asmPeer1 [128]uint64
			var asmPeer2 [128]uint64
			asmPeer0 = peer0
			asmPeer1 = peer1
			asmPeer2 = peer2
			asmCommunity := []*[128]uint64{&asmPeer0, &asmPeer1, &asmPeer2}

			backend := &Backend{}
			backend.executeKernelGo(&goOwner, ^uint64(0), goCommunity, uint64(len(goCommunity)), 2)

			var stageBuf [128]uint64
			var stageCount uint64
			executeKernel(backend, &asmOwner, ^uint64(0), asmCommunity, uint64(len(asmCommunity)), 2, &stageBuf, &stageCount)

			Convey("It should match the asm kernel selection exactly", func() {
				So(asmOwner[dstStart], ShouldEqual, goOwner[dstStart])
			})
		})
	})
}

/*
TestExecuteKernelModeEqEquivalence checks OpReduceModeEq (mode_eq) produces the
same owner dst slot when run through Backend.executeKernelGo and executeKernel
given identical owner/match/community frames.
*/
func TestExecuteKernelModeEqEquivalence(t *testing.T) {
	Convey("Given a mode_eq reducer instruction", t, func() {
		const (
			valueStart = uint64(56) // properties.labels
			keyStart   = uint64(64) // properties.community
			dstStart   = uint64(56)
			matchStart = uint64(64)
		)

		var owner [128]uint64
		owner[matchStart] = 7
		instr := packTestInstruction(
			OpReduceModeEq,
			valueStart, 1,
			keyStart, 1,
			dstStart, 1,
			matchStart,
			TopoHypercube,
			0,
		)
		instr |= 1 << PredicateBitShift
		instr |= uint64(PredEQ) << PredicateCondShift
		instr |= 1 << SrcAFromBShift
		owner[ProgramStartWord] = instr

		makeCommunity := func() []*[128]uint64 {
			peer0 := new([128]uint64)
			peer1 := new([128]uint64)
			peer2 := new([128]uint64)
			peer3 := new([128]uint64)

			peer0[valueStart], peer0[keyStart] = 11, 7
			peer1[valueStart], peer1[keyStart] = 22, 7
			peer2[valueStart], peer2[keyStart] = 22, 7
			peer3[valueStart], peer3[keyStart] = 33, 9

			return []*[128]uint64{peer0, peer1, peer2, peer3}
		}

		Convey("When Go and asm execute it", func() {
			backend := &Backend{}
			goOwner := owner
			goCommunity := makeCommunity()
			backend.executeKernelGo(&goOwner, ^uint64(0), goCommunity, uint64(len(goCommunity)), 2)

			asmOwner := owner
			asmCommunity := makeCommunity()
			var stageBuf [128]uint64
			var stageCount uint64
			executeKernel(backend, &asmOwner, ^uint64(0), asmCommunity, uint64(len(asmCommunity)), 2, &stageBuf, &stageCount)

			Convey("It should select the same modal value", func() {
				So(goOwner[dstStart], ShouldEqual, uint64(22))
				So(asmOwner[dstStart], ShouldEqual, goOwner[dstStart])
			})
		})
	})
}

func BenchmarkExecuteKernelModeEqEquivalence(b *testing.B) {
	const (
		valueStart = uint64(56)
		keyStart   = uint64(64)
		dstStart   = uint64(56)
		matchStart = uint64(64)
	)

	baseOwner := func() [128]uint64 {
		var owner [128]uint64
		owner[matchStart] = 7

		instr := packTestInstruction(
			OpReduceModeEq,
			valueStart, 1,
			keyStart, 1,
			dstStart, 1,
			matchStart,
			TopoHypercube,
			0,
		)
		instr |= 1 << PredicateBitShift
		instr |= uint64(PredEQ) << PredicateCondShift
		instr |= 1 << SrcAFromBShift

		owner[ProgramStartWord] = instr

		return owner
	}

	makeCommunity := func() []*[128]uint64 {
		peer0 := new([128]uint64)
		peer1 := new([128]uint64)
		peer2 := new([128]uint64)
		peer3 := new([128]uint64)

		peer0[valueStart], peer0[keyStart] = 11, 7
		peer1[valueStart], peer1[keyStart] = 22, 7
		peer2[valueStart], peer2[keyStart] = 22, 7
		peer3[valueStart], peer3[keyStart] = 33, 9

		return []*[128]uint64{peer0, peer1, peer2, peer3}
	}

	dimCount := uint64(2)

	backend := &Backend{}

	goOut := func() uint64 {
		owner := baseOwner()
		goCommunity := makeCommunity()

		backend.executeKernelGo(&owner, ^uint64(0), goCommunity, uint64(len(goCommunity)), dimCount)

		return owner[dstStart]
	}()

	b.Run("go", func(b *testing.B) {
		b.ReportAllocs()
		expect := goOut

		b.ResetTimer()

		for benchIdx := 0; benchIdx < b.N; benchIdx++ {
			owner := baseOwner()
			goCommunity := makeCommunity()

			backend.executeKernelGo(&owner, ^uint64(0), goCommunity, uint64(len(goCommunity)), dimCount)

			if owner[dstStart] != expect {
				b.Fatalf("modeEq go mismatch benchIdx=%d", benchIdx)
			}
		}
	})

	b.Run("asm", func(b *testing.B) {
		b.ReportAllocs()
		expect := goOut

		b.ResetTimer()

		for benchIdx := 0; benchIdx < b.N; benchIdx++ {
			owner := baseOwner()
			asmCommunity := makeCommunity()
			var stageBuf [128]uint64
			var stageCount uint64

			executeKernel(backend, &owner, ^uint64(0), asmCommunity, uint64(len(asmCommunity)), dimCount, &stageBuf, &stageCount)

			if owner[dstStart] != expect {
				b.Fatalf("modeEq asm mismatch benchIdx=%d", benchIdx)
			}
		}
	})
}

func BenchmarkExecuteKernelZipfSelect(b *testing.B) {
	const (
		valueStart       = uint64(63)
		utilityStart     = uint64(57)
		dstStart         = uint64(63)
		temperatureStart = uint64(60)
	)

	var owner [128]uint64
	owner[zipfIDWord] = 0x1234
	owner[temperatureStart] = 256

	peers := make([][128]uint64, 128)
	community := make([]*[128]uint64, len(peers))
	for idx := range peers {
		peers[idx][valueStart] = uint64(idx + 1)
		peers[idx][utilityStart] = uint64((idx*37 + 11) & 255)
		community[idx] = &peers[idx]
	}

	instr := packTestInstruction(
		OpReduceZipfSelect,
		valueStart, 1,
		utilityStart, 1,
		dstStart, 1,
		temperatureStart,
		TopoHypercube,
		0,
	)
	instr |= 1 << PredicateBitShift
	instr |= uint64(PredEQ) << PredicateCondShift
	instr |= 1 << SrcAFromBShift
	owner[ProgramStartWord] = instr

	backend := &Backend{}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		owner[dstStart] = 0
		backend.executeKernelGo(&owner, ^uint64(0), community, uint64(len(community)), 7)
	}
}

/*
TestExecuteKernelBRotateEquivalence pins the bRotate path: asm and Go
must produce byte-identical results when reading a SrcB span with a
non-zero rotation. The Metal-vs-CPU comparison test catches integration
mismatches but takes Metal as a third party; this isolates asm vs Go.
*/
func TestExecuteKernelBRotateEquivalence(t *testing.T) {
	Convey("Given a pop(B) program with rot8(B.tokens[0,2], 1)", t, func() {
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

		Convey("When executeKernelGo and asm both run", func() {
			backend := &Backend{}
			backend.executeKernelGo(&goOwner, ^uint64(0), []*[128]uint64{&goPeer}, 1, 0)

			var asmOwner [128]uint64
			var asmPeer [128]uint64
			asmOwner[72] = ^uint64(0)
			asmPeer[0] = 0x0807060504030201
			asmPeer[1] = 0x100f0e0d0c0b0a09
			asmOwner[ProgramStartWord] = instr
			var stageBuf [128]uint64
			var stageCount uint64
			executeKernel(backend, &asmOwner, ^uint64(0), []*[128]uint64{&asmPeer}, 1, 0, &stageBuf, &stageCount)

			Convey("They should write the identical rotated word to dst", func() {
				So(goOwner[32], ShouldEqual, uint64(0x0908070605040302))
				So(asmOwner[32], ShouldEqual, goOwner[32])
			})
		})
	})
}

/*
TestExecuteKernelSrcAFromBEquivalence pins the srcAFromB routing bit:
when set, ptrA must point at the bound B frame so SrcA reads from the
peer rather than the owner. Catches asm bugs that leave ptrA pinned
at the owner frame after my srcAFromB extension.
*/
func TestExecuteKernelSrcAFromBEquivalence(t *testing.T) {
	Convey("Given a pop(B) write that reads SrcA from the popped frame", t, func() {
		const (
			opcodeCopyA = uint64(0x3) // dst <- A (here A = peer because srcAFromB=1)
			topoPop     = uint64(1)
			maskWord    = uint64(72)
			peerSrcA    = uint64(0)  // tokens[0]
			ownerDstA   = uint64(40) // owner.context[0]
		)

		var goOwner [128]uint64
		goOwner[maskWord] = ^uint64(0)
		var goPeer [128]uint64
		goPeer[peerSrcA] = 0xCAFEBABEDEADBEEF

		// pop(B) topo, target=A, srcAFromB=1, copy from B[peerSrcA] -> A[ownerDstA]
		instr := packTestInstruction(opcodeCopyA, peerSrcA, 1, 0, 1, ownerDstA, 1, maskWord, topoPop, 0)
		instr |= 1 << 61 // srcAFromB
		goOwner[ProgramStartWord] = instr

		Convey("When executeKernelGo runs", func() {
			community := []*[128]uint64{&goPeer}
			backend := &Backend{}
			backend.executeKernelGo(&goOwner, ^uint64(0), community, 1, 0)

			Convey("It should copy the peer's word into the owner's dst", func() {
				So(goOwner[ownerDstA], ShouldEqual, uint64(0xCAFEBABEDEADBEEF))
			})

			Convey("And HypercubeGossip via the asm dispatcher should produce the same result", func() {
				var asmOwner [128]uint64
				asmOwner[maskWord] = ^uint64(0)
				asmOwner[ProgramStartWord] = instr
				var asmPeer [128]uint64
				asmPeer[peerSrcA] = 0xCAFEBABEDEADBEEF

				asmCommunity := []*[128]uint64{&asmPeer}
				var stageBuf [128]uint64
				var stageCount uint64
				executeKernel(backend, &asmOwner, ^uint64(0), asmCommunity, 1, 0, &stageBuf, &stageCount)

				So(asmOwner[ownerDstA], ShouldEqual, goOwner[ownerDstA])
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
		topoPop     = uint64(1)
		maskWord    = uint64(72)
		peerSrcA    = uint64(0)
		ownerDstA   = uint64(40)
	)

	instr := packTestInstruction(opcodeCopyA, peerSrcA, 1, 0, 1, ownerDstA, 1, maskWord, topoPop, 0)
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
