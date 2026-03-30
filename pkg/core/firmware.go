package core

const (
	// Firmware register protocol values carried in-band by Value frames.
	// These are distinct from the FirmwareType enum because Bootloader is not
	// selected through the fw register; new frames already begin with the
	// bootloader installed in the system slots.
	FirmwareRegisterNone uint64 = iota
	FirmwareRegisterLearn
	FirmwareRegisterTombstone
	FirmwareRegisterViral
	FirmwareRegisterBuild
)

const (
	// PayloadProgramWordOffset is the fixed payload span used by the in-band
	// programs themselves (config.yml refers to bits 5120..8192 => words 80..127).
	// The first four program words remain the persistent bootstrap prefix. The
	// bootloader may occupy more slots during cold boot, but sequenced payloads
	// intentionally overwrite the remainder in place after bootstrapping.
	PayloadProgramWordOffset = 4
	PayloadProgramPCOffset   = PayloadProgramWordOffset * 2

	// UserProgramPCStart is kept as a compatibility alias for older call sites.
	UserProgramPCStart uint64 = PayloadProgramPCOffset
)

// FirmwareRegisterValue converts a compiled firmware type to the in-band fw
// register protocol. Bootloader is a special case and is never selected via fw.
func FirmwareRegisterValue(ft FirmwareType) uint64 {
	switch ft {
	case FirmwareTypeLearn:
		return FirmwareRegisterLearn
	case FirmwareTypeTombstone:
		return FirmwareRegisterTombstone
	case FirmwareTypeViral:
		return FirmwareRegisterViral
	case FirmwareTypeBuild:
		return FirmwareRegisterBuild
	default:
		return FirmwareRegisterNone
	}
}

// FirmwareTypeFromRegister maps the in-band fw register protocol to the
// compiled firmware table.
func FirmwareTypeFromRegister(sel uint64) (FirmwareType, bool) {
	switch sel {
	case FirmwareRegisterLearn:
		return FirmwareTypeLearn, true
	case FirmwareRegisterTombstone:
		return FirmwareTypeTombstone, true
	case FirmwareRegisterViral:
		return FirmwareTypeViral, true
	case FirmwareRegisterBuild:
		return FirmwareTypeBuild, true
	default:
		return 0, false
	}
}

// ResolveFirmwareRegister is a compatibility alias used by kernel code.
func ResolveFirmwareRegister(sel uint64) (FirmwareType, bool) {
	return FirmwareTypeFromRegister(sel)
}

// PayloadProgramWordStart returns the word index of the first payload slot.
func PayloadProgramWordStart() int {
	return Cfg.ProgramIndex + PayloadProgramWordOffset
}

// PayloadProgramWordCount returns the number of words available for payload
// firmware after the reserved bootstrap prefix.
func PayloadProgramWordCount() int {
	total := int((Cfg.ProgramBits + 63) / 64)
	total -= PayloadProgramWordOffset
	if total < 0 {
		return 0
	}
	return total
}

// PayloadProgramPCStart returns the first absolute instruction slot available
// to payload firmware.
func PayloadProgramPCStart() uint64 {
	return uint64(PayloadProgramPCOffset)
}

// CurrentInstruction returns the instruction currently addressed by the frame's
// pc, or zero when the pc points outside the program region.
func CurrentInstruction(frame []uint64) uint32 {
	if len(frame) <= Cfg.RegPC || Cfg.ProgramIndex < 0 || Cfg.MaxPC <= 0 {
		return 0
	}
	pc := frame[Cfg.RegPC]
	if pc >= uint64(Cfg.MaxPC) {
		return 0
	}
	wordPos := Cfg.ProgramIndex + int(pc/2)
	if wordPos < 0 || wordPos >= len(frame) {
		return 0
	}
	shift := uint((pc % 2) * 32)
	return uint32(frame[wordPos] >> shift)
}

// FrameReadyForFirmwareLoad reports whether a new payload firmware can be
// loaded into the payload slots without interrupting an active instruction
// stream. A Value is ready when execution is at the bootstrap entry (pc=0),
// halted/exhausted, or currently pointing at a zero instruction. Existing
// payload words are not a blocker because sequenced firmware intentionally
// overwrites the previous payload in place.
func FrameReadyForFirmwareLoad(frame []uint64) bool {
	if len(frame) <= Cfg.RegPC {
		return false
	}
	pc := frame[Cfg.RegPC]
	if pc == 0 || pc >= uint64(Cfg.MaxPC) {
		return true
	}
	return CurrentInstruction(frame) == 0
}

func clearProgramSlots(frame []uint64, startSlot int) {
	if frame == nil {
		return
	}
	if startSlot < 0 {
		startSlot = 0
	}
	if startSlot >= Cfg.MaxPC {
		return
	}
	for slot := startSlot; slot < Cfg.MaxPC; slot++ {
		wordIdx := Cfg.ProgramIndex + slot/2
		if wordIdx < 0 || wordIdx >= len(frame) {
			break
		}
		shift := uint((slot % 2) * 32)
		mask := uint64(0xFFFFFFFF) << shift
		frame[wordIdx] &^= mask
	}
}

// ClearProgramRegion zeros the full in-band program region.
func ClearProgramRegion(frame []uint64) {
	clearProgramSlots(frame, 0)
}

// ClearPayloadProgram zeros only the payload span, not the reserved bootstrap
// prefix at the front of the program region.
func ClearPayloadProgram(frame []uint64) {
	clearProgramSlots(frame, int(PayloadProgramPCStart()))
}

// InstallProgramAtSlot writes program starting at absolute instruction slot
// startSlot. It preserves any instruction halves outside the overwritten slots.
func InstallProgramAtSlot(frame []uint64, startSlot int, program []uint32) {
	if frame == nil || startSlot < 0 || startSlot >= Cfg.MaxPC {
		return
	}
	for i, instr := range program {
		slot := startSlot + i
		if slot >= Cfg.MaxPC {
			break
		}
		wordIdx := Cfg.ProgramIndex + slot/2
		if wordIdx < 0 || wordIdx >= len(frame) {
			break
		}
		shift := uint((slot % 2) * 32)
		mask := uint64(0xFFFFFFFF) << shift
		frame[wordIdx] = (frame[wordIdx] &^ mask) | (uint64(instr) << shift)
	}
}

// ReplaceProgramAtSlot clears all instruction slots from startSlot onward and
// installs program beginning at that slot.
func ReplaceProgramAtSlot(frame []uint64, startSlot int, program []uint32) {
	clearProgramSlots(frame, startSlot)
	InstallProgramAtSlot(frame, startSlot, program)
}

// PreloadPendingFirmware copies the firmware selected by the in-band fw
// register into the payload slots and moves execution to the payload start.
// It returns true when a firmware was loaded.
func PreloadPendingFirmware(frame []uint64) bool {
	if len(frame) <= Cfg.FW || len(frame) <= Cfg.RegPC {
		return false
	}
	ft, ok := FirmwareTypeFromRegister(frame[Cfg.FW])
	if !ok {
		return false
	}
	if !FrameReadyForFirmwareLoad(frame) {
		return false
	}

	prog := Cfg.Firmware[ft]
	if len(prog) == 0 {
		frame[Cfg.FW] = FirmwareRegisterNone
		return false
	}

	startSlot := int(PayloadProgramPCStart())
	ClearPayloadProgram(frame)
	InstallProgramAtSlot(frame, startSlot, prog)
	frame[Cfg.FW] = FirmwareRegisterNone
	frame[Cfg.RegPC] = PayloadProgramPCStart()
	return true
}
