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

	for pc := uint64(0); pc < ProgramWords; pc++ {
		instr := ownerFrame[ProgramStartWord+pc]
		if instr == 0 {
			continue
		}

		if (instr>>TargetShift)&TargetMask == TargetC {
			return false
		}
	}

	return true
}
