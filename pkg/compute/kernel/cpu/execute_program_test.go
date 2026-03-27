package cpu

import (
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

func init() {
	// Initialize deterministic core configuration for our tests.
	core.Cfg.RegPC = 78
	core.Cfg.ProgramIndex = 79 * 64
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

func setupVMContexts() (*primitive.Value, *primitive.Value, *Backend) {
	valA := primitive.NewValue()
	valB := primitive.NewValue()
	backend := NewBackend()
	return valA, valB, backend
}

func TestExecuteProgram_FirmwareLoad(t *testing.T) {
	Convey("In-band firmware registers hook program execution", t, func() {
		valA, valB, backend := setupVMContexts()

		// Populate a fake firmware in config
		core.Cfg.Firmware = [][]uint32{
			{}, // 0 unused
			{ // 1 acts as bootloader
				encodeInstruction(3, 8, uint16(core.Cfg.RegPC)), // JUMP to 8
				0, // HALT
			},
			{ // 2 acts as actual firmware program
				encodeInstruction(3, 0x777, uint16(core.Cfg.R5)),
				0, // HALT
			},
		}

		core.Cfg.FirmwareIndex = map[string]int{
			"bootloader": 1,
		}

		// Trigger firmware execution by setting FW register to index 2
		valA[core.Cfg.FW] = 2

		// Execute frame
		backend.UniversalBitwise(unsafe.Pointer(valA), unsafe.Pointer(valB), nil, 1)

		// Assertions: FW register is cleared; PC jumped to 8 then progressed to 9; R5 set
		So(valA[core.Cfg.FW], ShouldEqual, 0)
		So(valA[core.Cfg.R5], ShouldEqual, 0x777)
		So(valA[core.Cfg.RegPC], ShouldEqual, 9) // Exited at PC=9
	})
}
