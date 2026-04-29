package cpu

/*
programAsmCompatible gates whether resident programs may use executeKernel asm
or fall back to executeKernelGo.

Today the emitted asm path mirrors the canonical Go semantics for firmware this
repository loads; compatibility is a trivial pass-through. Re-introduce guards
when a lowered opcode/topology intentionally remains Go-only.
*/
func (backend *Backend) programAsmCompatible(ownerFrame *[128]uint64) bool {
	_ = backend

	if ownerFrame == nil {
		return true
	}

	for pc := uint64(0); pc < ProgramWords; pc++ {
		instr := ownerFrame[ProgramStartWord+pc]
		if instr == 0 {
			continue
		}

		if (instr>>TargetShift)&3 == TargetC {
			return false
		}
	}

	return true
}
