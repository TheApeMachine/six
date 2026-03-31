package firmware

import (
	"unsafe"

	"github.com/theapemachine/six/pkg/core"
)

func ResolveFirmwareRegister(sel uint64) (core.FirmwareType, bool) {
	switch sel {
	case core.FirmwareRegisterLearn:
		return core.FirmwareTypeLearn, true
	case core.FirmwareRegisterTombstone:
		return core.FirmwareTypeTombstone, true
	case core.FirmwareRegisterViral:
		return core.FirmwareTypeViral, true
	case core.FirmwareRegisterBuild:
		return core.FirmwareTypeBuild, true
	default:
		return 0, false
	}
}

func PayloadProgramPCStart() uint64 {
	return uint64(core.PayloadProgramPCOffset)
}

func ClearPayloadProgram(c *[128]uint64) {
	if c == nil {
		return
	}
	for slot := int(PayloadProgramPCStart()); slot < core.Cfg.MaxPC; slot++ {
		wordIdx := core.Cfg.ProgramIndex + slot/2
		if wordIdx < 0 || wordIdx >= len(c) {
			break
		}
		shift := uint((slot % 2) * 32)
		mask := uint64(0xFFFFFFFF) << shift
		c[wordIdx] &^= mask
	}
}

func installProgramAtSlot(c *[128]uint64, startSlot int, program []uint16) {
	if c == nil || startSlot < 0 || startSlot >= core.Cfg.MaxPC {
		return
	}
	for i, instr := range program {
		slot := startSlot + i
		if slot >= core.Cfg.MaxPC {
			break
		}
		wordIdx := core.Cfg.ProgramIndex + slot/4
		if wordIdx < 0 || wordIdx >= len(c) {
			break
		}
		shift := uint((slot % 4) * 16)
		mask := uint64(0xFFFF) << shift
		c[wordIdx] = (c[wordIdx] &^ mask) | (uint64(instr) << shift)
	}
}

func frameReadyForFirmwareLoad(c *[128]uint64) bool {
	if c == nil {
		return false
	}
	pc := c[core.Cfg.RegPC]
	if pc == 0 || pc >= uint64(core.Cfg.MaxPC) {
		return true
	}
	wordPos := core.Cfg.ProgramIndex + int(pc/2)
	if wordPos < 0 || wordPos >= len(c) {
		return true
	}
	shift := uint((pc % 2) * 32)
	return uint32(c[wordPos]>>shift) == 0
}

func PreloadFirmwareFrame(c *[128]uint64) bool {
	if c == nil {
		return false
	}
	ft, ok := ResolveFirmwareRegister(c[core.Cfg.FW])
	if !ok || !frameReadyForFirmwareLoad(c) {
		return false
	}
	prog := core.Cfg.Firmware[ft]
	if len(prog) == 0 {
		c[core.Cfg.FW] = core.FirmwareRegisterNone
		return false
	}
	ClearPayloadProgram(c)
	installProgramAtSlot(c, int(PayloadProgramPCStart()), prog)
	cc := core.FirmwareRegisterNone
	c[core.Cfg.FW] = cc
	c[core.Cfg.RegPC] = PayloadProgramPCStart()
	return true
}

func PreloadFirmwareBatch(a unsafe.Pointer, count int) {
	if a == nil {
		return
	}
	for i := 0; i < count; i++ {
		c := (*[128]uint64)(unsafe.Pointer(uintptr(a) + uintptr(i)*1024))
		PreloadFirmwareFrame(c)
	}
}
