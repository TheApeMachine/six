package core

import "fmt"

// ViralFirmwareSelectorSlot is the instruction slot inside the Viral prelude
// that loads the next firmware selector into r8. The Viral program later copies
// r8 into partner/self fw, so patching this slot lets Go seed a carrier Value
// without appending dead instructions after the Viral halt.
const ViralFirmwareSelectorSlot = 7

// EncodeInstruction packs a 4-bit opcode and two 14-bit operands into the
// native 32-bit instruction word used by the bitwise kernels.
func EncodeInstruction(op uint8, src, dst uint16) uint32 {
	return uint32(op&0xF) | (uint32(src&0x3FFF) << 4) | (uint32(dst&0x3FFF) << 18)
}

// EncodeWriteRegisterImmediate builds a writeReg-style instruction that applies
// gate A (0011) with an immediate src and a register destination.
func EncodeWriteRegisterImmediate(src uint16, dstReg int) uint32 {
	dst := uint16(OperandFlagRegister | uint64(uint16(dstReg)))
	return EncodeInstruction(0x3, src&0x3FFF, dst&0x3FFF)
}

// PatchProgramSlot clones program and rewrites one instruction slot. Out-of-
// range slots leave the cloned program unchanged.
func PatchProgramSlot(program []uint32, slot int, instr uint32) []uint32 {
	cloned := append([]uint32(nil), program...)
	if slot < 0 || slot >= len(cloned) {
		return cloned
	}
	cloned[slot] = instr
	return cloned
}

// ViralSeedProgram returns a cloned Viral carrier whose selector slot has been
// patched to route the payload into next. This preserves the in-band carrier
// semantics: the seed still executes the Viral prelude, but it now primes the
// infected partner to boot the requested payload instead of the config default.
func ViralSeedProgram(next FirmwareType) ([]uint32, error) {
	base := Cfg.Firmware[FirmwareTypeViral]
	if len(base) <= ViralFirmwareSelectorSlot {
		return nil, fmt.Errorf("core.ViralSeedProgram: viral firmware too short: have %d slots, need slot %d", len(base), ViralFirmwareSelectorSlot)
	}
	selector := FirmwareRegisterValue(next)
	if selector == FirmwareRegisterNone {
		return nil, fmt.Errorf("core.ViralSeedProgram: next firmware register code must be non-zero")
	}
	return PatchProgramSlot(
		base,
		ViralFirmwareSelectorSlot,
		EncodeWriteRegisterImmediate(uint16(selector), Cfg.R8),
	), nil
}
