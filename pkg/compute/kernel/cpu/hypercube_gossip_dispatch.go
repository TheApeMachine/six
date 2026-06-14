package cpu

/*
programAsmCompatible gates whether resident programs may use executeKernel asm
or fall back to executeKernelGo.

Today the asm path mirrors canonical Go semantics except for target-C child
writes, which stay on the Go kernel until the assembly path materializes child
frames directly.
*/
func (backend *Backend) programAsmCompatible(ownerFrame *[128]uint64) bool {
	_ = backend // receiver kept so dispatch policy can use Backend state later

	if ownerFrame == nil {
		return true
	}

	if ProgramStartWord+ProgramWords > len(*ownerFrame) {
		return false
	}

	for pc := range uint64(ProgramWords) {
		instr := ownerFrame[ProgramStartWord+pc]
		if instr == 0 {
			continue
		}

		if backend.geometricSlot(instr) {
			return false
		}

		if (instr>>PredicateBitShift)&1 == 1 && (instr>>PredicateCondShift)&7 == PredScalar {
			return false
		}

		target := (instr >> TargetShift) & TargetMask
		topology := (instr >> TopologyShift) & 3
		stage := (instr >> StageBitShift) & 1
		popEnd := (instr >> PopEndBitShift) & 1

		if topology == TopoPopQueue || popEnd == 1 || (stage == 1 && target != TargetC) {
			return false
		}

		if target == TargetC {
			return false
		}

		if (topology == TopoHypercube || topology == TopoHypercubePerPeer) && target == TargetA {
			return false
		}
	}

	return true
}
