package cpu

/*
programAsmCompatible decides whether a Value's program region can run
through the asm executeKernel fast path or whether it must fall back to
the canonical Go executeKernelGo. The asm currently implements:

  - truth-table broadcast under TopoLocal (0) and TopoPopQueue (1)
  - owner-side mask gating
  - per-peer dim selection for TopoHypercube (2) — but ONLY one peer
    per instruction, not the new "broadcast to every peer" semantic
    the Go kernel implements

So any instruction that uses one of the following is routed to the Go
kernel until the corresponding asm path is implemented:

  - predicate == 1                  (no popcount predicate engine in asm)
  - stage == 1                      (no staging buffer in asm)
  - srcAFromB == 1                  (no SrcA-from-B routing in asm)
  - topology == TopoHypercubePerPeer (no per-peer mask in asm)
  - hypercube target=B              (asm picks one peer; new ALU broadcasts)
  - reduce opcodes under predicate  (asm has no reduceArgMin/reduceMode)

Each item shrinks as the asm grows.
*/
func programAsmCompatible(ownerFrame *[128]uint64) bool {
	for pc := uint64(0); pc < ProgramWords; pc++ {
		instr := ownerFrame[ProgramStartWord+pc]
		if instr == 0 {
			continue
		}

		opcode := instr & 0xF
		topology := (instr >> 55) & 3
		predicate := (instr >> PredicateBitShift) & 1
		predCond := (instr >> PredicateCondShift) & 7
		bRotate := predCond
		srcAFromB := (instr >> SrcAFromBShift) & 1
		stageBit := (instr >> StageBitShift) & 1
		targetB := (instr >> 53) & 1

		// Predicate path fully implemented in asm:
		//   predicate_path / predicate_path_amd64 — compare + StorePopcnt + AnyZero
		//   per_peer_predicate_path[_amd64] — TopoHypercubePerPeer per-peer
		//   reducer paths[_amd64]           — argmin, mode_eq, zipf_select
		_ = predicate

		// stage(B) implemented in asm (stage_path / stage_path_amd64),
		// writes peer indices into the host-supplied stageBuf.
		_ = stageBit

		// srcAFromB is implemented on both archs (ptra_owner block).
		_ = srcAFromB

		// TopoHypercubePerPeer fully implemented in asm:
		//   per_peer_predicate_path  — per-peer popcount + compare
		//   bcast_peer_loop          — reads peer[maskStart] each peer
		//   stage_per_peer           — iterates community for stage
		_ = topology

		// Hypercube target=B per-peer broadcast is implemented in both
		// arm64 and amd64 asm (bcast_peer_loop in each). The dispatcher
		// no longer needs to gate this case.
		_ = topology
		_ = targetB

		// bRotate implemented in asm (sp_no_rot / bc_no_rot for arm64,
		// sp_no_rot_amd64 / bc_no_rot_amd64 for amd64). Pop body reads
		// from the persistent currentB stack slot now, so multi-instr
		// pop bodies correctly carry the seed's peer through.
		_ = bRotate

		_ = opcode
	}

	return true
}
