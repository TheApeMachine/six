package cpu

/*
programAsmCompatible gates whether resident programs may use executeKernel asm
or fall back to executeKernelGo.

Today the emitted asm path mirrors the canonical Go semantics for firmware this
repository loads; compatibility is a trivial pass-through. Re-introduce guards
when a lowered opcode/topology intentionally remains Go-only.
*/
func (backend *Backend) programAsmCompatible(ownerFrame *[128]uint64) bool {
	_, _ = backend, ownerFrame

	return true
}
