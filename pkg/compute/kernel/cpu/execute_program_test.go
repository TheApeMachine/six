package cpu

import (
	"maps"
	"testing"
	"unsafe"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

func cfgSnapshot(src *core.Config) *core.Config {
	if src == nil {
		return nil
	}
	out := *src
	if src.Firmware != nil {
		out.Firmware = make([][]uint32, len(src.Firmware))
		for i := range src.Firmware {
			out.Firmware[i] = append([]uint32(nil), src.Firmware[i]...)
		}
	}
	if src.FirmwareIndex != nil {
		out.FirmwareIndex = maps.Clone(src.FirmwareIndex)
	}
	return &out
}

func restoreCfg(dst *core.Config, src *core.Config) {
	if dst == nil || src == nil {
		return
	}
	*dst = *src
	if src.Firmware != nil {
		dst.Firmware = make([][]uint32, len(src.Firmware))
		for i := range src.Firmware {
			dst.Firmware[i] = append([]uint32(nil), src.Firmware[i]...)
		}
	} else {
		dst.Firmware = nil
	}
	if src.FirmwareIndex != nil {
		dst.FirmwareIndex = maps.Clone(src.FirmwareIndex)
	} else {
		dst.FirmwareIndex = nil
	}
}

func setTestCoreCfg(t *testing.T) {
	t.Helper()
	saved := cfgSnapshot(core.Cfg)
	t.Cleanup(func() { restoreCfg(core.Cfg, saved) })

	core.Cfg.RegPC = 78
	core.Cfg.ProgramIndex = 79 // word index of program region (matches cmd/cfg/config.yml)
	core.Cfg.ProgramBits = 49 * 64
	core.Cfg.MaxPC = int(core.Cfg.ProgramBits) / 32
	core.Cfg.R0 = 71
	core.Cfg.R1 = 72
	core.Cfg.R2 = 73
	core.Cfg.R3 = 74
	core.Cfg.R4 = 75
	core.Cfg.R5 = 76
	core.Cfg.FW = 77
}

// encodeInstruction is a helper to assemble a 32-bit instruction for the VM.
// Format:
// bits 0-3:   op (truth table)
// bits 4-17:  srcCode (immediate, indirection, or span modifier)
// bits 18-31: dstCode
func encodeInstruction(op uint8, srcCode, dstCode uint16) uint32 {
	return uint32(op&0xF) | (uint32(srcCode&0x3FFF) << 4) | (uint32(dstCode&0x3FFF) << 18)
}

func setupVMContexts(t *testing.T) (*primitive.Value, *primitive.Value, *Backend) {
	t.Helper()
	setTestCoreCfg(t)
	valA := primitive.NewValue()
	valB := primitive.NewValue()
	backend := NewBackend()
	return valA, valB, backend
}

func clearProgramRegion(valA *primitive.Value) {
	wordBase := uint64(core.Cfg.ProgramIndex) / 64
	n := uint64(core.Cfg.ProgramBits) / 64
	for i := uint64(0); i < n; i++ {
		valA[wordBase+i] = 0
	}
}

// baselineFirmware returns the same layout as the happy-path test.
func baselineFirmware() ([][]uint32, map[string]int) {
	return [][]uint32{
			{}, // 0 unused
			{ // 1 acts as bootloader
				encodeInstruction(3, 8, uint16(core.Cfg.RegPC)), // JUMP to 8
				0, // HALT
			},
			{ // direct firmware
				encodeInstruction(3, 0x777, uint16(core.Cfg.R5)),
				0, // HALT
			},
		},
		map[string]int{"bootloader": 1}
}

func TestExecuteProgram_FirmwareLoad(t *testing.T) {
	t.Run("valid_direct_firmware_index", func(t *testing.T) {
		valA, valB, backend := setupVMContexts(t)
		core.Cfg.Firmware, core.Cfg.FirmwareIndex = baselineFirmware()

		valA[core.Cfg.FW] = 2
		_ = backend.UniversalBitwise(unsafe.Pointer(valA), unsafe.Pointer(valB))

		if valA[core.Cfg.FW] != 0 {
			t.Fatalf("core.Cfg.FW: want 0 after load, got %d", valA[core.Cfg.FW])
		}
		if valA[core.Cfg.R5] != 0x777 {
			t.Fatalf("R5: want 0x777, got %d", valA[core.Cfg.R5])
		}
		if valA[core.Cfg.RegPC] != 1 {
			t.Fatalf("RegPC: want 1, got %d", valA[core.Cfg.RegPC])
		}
	})

	t.Run("invalid_firmware_index_out_of_range", func(t *testing.T) {
		valA, valB, backend := setupVMContexts(t)
		core.Cfg.Firmware, core.Cfg.FirmwareIndex = baselineFirmware()

		const bogus = uint64(99)
		valA[core.Cfg.FW] = bogus
		clearProgramRegion(valA)
		valA[core.Cfg.RegPC] = 0

		_ = backend.UniversalBitwise(unsafe.Pointer(valA), unsafe.Pointer(valB))

		// Loader requires int(fw) < len(Firmware); bogus index is skipped — FW unchanged, no words copied.
		if valA[core.Cfg.FW] != bogus {
			t.Fatalf("core.Cfg.FW: want %d (unchanged), got %d", bogus, valA[core.Cfg.FW])
		}
		if valA[core.Cfg.R5] != 0 {
			t.Fatalf("R5: want 0 (no program mutating regs), got %d", valA[core.Cfg.R5])
		}
	})

	t.Run("firmware_entry_empty_slice", func(t *testing.T) {
		valA, valB, backend := setupVMContexts(t)
		core.Cfg.Firmware = [][]uint32{
			{},
			{}, // index 1: empty program
			{
				encodeInstruction(3, 0x777, uint16(core.Cfg.R5)),
				0,
			},
		}
		core.Cfg.FirmwareIndex = map[string]int{"bootloader": 1}

		clearProgramRegion(valA)
		valA[core.Cfg.FW] = 1
		valA[core.Cfg.RegPC] = 0
		r5Before := valA[core.Cfg.R5]

		_ = backend.UniversalBitwise(unsafe.Pointer(valA), unsafe.Pointer(valB))

		if valA[core.Cfg.FW] != 0 {
			t.Fatalf("core.Cfg.FW: want cleared after empty load, got %d", valA[core.Cfg.FW])
		}
		if valA[core.Cfg.R5] != r5Before {
			t.Fatalf("R5: want unchanged %d with no opcodes loaded, got %d", r5Before, valA[core.Cfg.R5])
		}
	})

	t.Run("regpc_at_maxpc_halts_without_mutating", func(t *testing.T) {
		valA, valB, backend := setupVMContexts(t)
		core.Cfg.Firmware, core.Cfg.FirmwareIndex = baselineFirmware()

		clearProgramRegion(valA)
		valA[core.Cfg.FW] = 0
		wantPC := uint64(core.Cfg.MaxPC)
		valA[core.Cfg.RegPC] = wantPC

		_ = backend.UniversalBitwise(unsafe.Pointer(valA), unsafe.Pointer(valB))

		if valA[core.Cfg.RegPC] != wantPC {
			t.Fatalf("RegPC at MaxPC: want %d (unchanged), got %d", wantPC, valA[core.Cfg.RegPC])
		}
	})

	t.Run("regpc_near_max_zero_instruction_halts", func(t *testing.T) {
		valA, valB, backend := setupVMContexts(t)
		core.Cfg.Firmware, core.Cfg.FirmwareIndex = baselineFirmware()

		clearProgramRegion(valA)
		valA[core.Cfg.FW] = 0
		wantPC := uint64(core.Cfg.MaxPC - 1)
		valA[core.Cfg.RegPC] = wantPC

		_ = backend.UniversalBitwise(unsafe.Pointer(valA), unsafe.Pointer(valB))

		if valA[core.Cfg.RegPC] != wantPC {
			t.Fatalf("RegPC near Max with zero opcode: want %d (immediate HALT), got %d", wantPC, valA[core.Cfg.RegPC])
		}
	})
}

func BenchmarkExecuteProgram_FirmwareLoad(b *testing.B) {
	saved := cfgSnapshot(core.Cfg)
	b.Cleanup(func() { restoreCfg(core.Cfg, saved) })
	core.Cfg.RegPC = 78
	core.Cfg.ProgramIndex = 79
	core.Cfg.ProgramBits = 49 * 64
	core.Cfg.MaxPC = int(core.Cfg.ProgramBits) / 32
	core.Cfg.R0 = 71
	core.Cfg.R1 = 72
	core.Cfg.R2 = 73
	core.Cfg.R3 = 74
	core.Cfg.R4 = 75
	core.Cfg.R5 = 76
	core.Cfg.FW = 77

	valA := primitive.NewValue()
	valB := primitive.NewValue()
	backend := NewBackend()
	core.Cfg.Firmware, core.Cfg.FirmwareIndex = baselineFirmware()

	wordBase := uint64(core.Cfg.ProgramIndex) / 64
	progWords := uint64(core.Cfg.ProgramBits) / 64

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for w := uint64(0); w < progWords; w++ {
			valA[wordBase+w] = 0
		}
		valA[core.Cfg.FW] = 2
		valA[core.Cfg.RegPC] = 0

		_ = backend.UniversalBitwise(unsafe.Pointer(valA), unsafe.Pointer(valB))

		if valA[core.Cfg.R5] != 0x777 {
			b.Fatalf("benchmark invariant: R5 want 0x777 got %d", valA[core.Cfg.R5])
		}
		if valA[core.Cfg.RegPC] != 1 {
			b.Fatalf("benchmark invariant: RegPC want 1 got %d", valA[core.Cfg.RegPC])
		}
	}
}
