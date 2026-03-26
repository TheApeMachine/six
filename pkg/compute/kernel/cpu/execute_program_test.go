package cpu

import (
	"testing"
	"unsafe"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
	. "github.com/smartystreets/goconvey/convey"
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

func encodeProgram(v *primitive.Value, instrs []uint32) {
	copyProgram(v, instrs, 0)
}

func setupVMContexts() (*primitive.Value, *primitive.Value, *Backend) {
	valA := primitive.NewValue()
	valB := primitive.NewValue()
	backend := NewBackend()
	return valA, valB, backend
}

func TestExecuteProgram_MathLogical(t *testing.T) {
	Convey("Executing simple logical programs", t, func() {
		valA, valB, backend := setupVMContexts()

		// Initial states
		valA[core.Cfg.R0] = 0xAAAA // 10101010...
		valA[core.Cfg.R1] = 0x5555 // 01010101...

		// Program:
		// 1. Copy R0 to R2 (op=3, Copy Left)
		instr1 := encodeInstruction(3, 0x2000|uint16(core.Cfg.R0), uint16(core.Cfg.R2))
		// 2. XOR R0 and R1 into R1 (op=6, L ^ R)
		instr2 := encodeInstruction(6, 0x2000|uint16(core.Cfg.R0), 0x2000|uint16(core.Cfg.R1))
		// 3. AND R0 and R1 into R3 (using the new R1) (op=1, L & R)
		// Wait, to target R3, dstCode must be R3, but right is valA[R3], so we must copy R1 to R3 first
		instr3Copy := encodeInstruction(3, 0x2000|uint16(core.Cfg.R1), uint16(core.Cfg.R3))
		instr3And := encodeInstruction(1, 0x2000|uint16(core.Cfg.R0), 0x2000|uint16(core.Cfg.R3))
		// 4. Halt
		instrHalt := encodeInstruction(0, 0, 0)

		encodeProgram(valA, []uint32{instr1, instr2, instr3Copy, instr3And, instrHalt})

		// Run
		backend.UniversalBitwise(unsafe.Pointer(valA), unsafe.Pointer(valB), nil, 1)

		So(valA[core.Cfg.RegPC], ShouldEqual, 4)        // PC incremented past Halt at PC 4
		So(valA[core.Cfg.R2], ShouldEqual, 0xAAAA)      // Copied R0
		So(valA[core.Cfg.R1], ShouldEqual, 0xFFFF)      // 0xAAAA ^ 0x5555
		So(valA[core.Cfg.R3], ShouldEqual, 0xAAAA)      // 0xAAAA & 0xFFFF
	})
}

func TestExecuteProgram_ImmediateLoad(t *testing.T) {
	Convey("Loading an immediate 12-bit payload", t, func() {
		valA, valB, backend := setupVMContexts()

		// Program: Copy literal 0xABC into R4
		instr := encodeInstruction(3, 0xABC, uint16(core.Cfg.R4)) // Op=3 (Copy L), L is immediate 0xABC
		encodeProgram(valA, []uint32{instr, 0})

		backend.UniversalBitwise(unsafe.Pointer(valA), unsafe.Pointer(valB), nil, 1)

		So(valA[core.Cfg.R4], ShouldEqual, 0xABC)
	})
}

func TestExecuteProgram_PCJump(t *testing.T) {
	Convey("Program explicitly branches by setting RegPC", t, func() {
		valA, valB, backend := setupVMContexts()

		// Program:
		// 0: Jump to instruction 3 by setting RegPC to 3
		instrJump := encodeInstruction(3, 3, uint16(core.Cfg.RegPC)) // op=3 is Copy L
		// 1: Poison pill - if executed, tests fail
		instrPoison := encodeInstruction(3, 0xAAA, uint16(core.Cfg.R1))
		// 2: Poison pill
		instrPoison2 := encodeInstruction(3, 0xBBB, uint16(core.Cfg.R2))
		// 3: Safe landing
		instrSafe := encodeInstruction(3, 0xCCC, uint16(core.Cfg.R3))

		// Encode safely
		encodeProgram(valA, []uint32{instrJump, instrPoison, instrPoison2, instrSafe, 0})

		backend.UniversalBitwise(unsafe.Pointer(valA), unsafe.Pointer(valB), nil, 1)

		So(valA[core.Cfg.R1], ShouldEqual, 0)
		So(valA[core.Cfg.R2], ShouldEqual, 0)
		So(valA[core.Cfg.R3], ShouldEqual, 0xCCC)
		So(valA[core.Cfg.RegPC], ShouldEqual, 4) // Exited after Safe and Halt at PC 4
	})
}

func TestExecuteProgram_SpanOperation(t *testing.T) {
	Convey("Bitwise operation over spans spanning multiple words", t, func() {
		valA, valB, backend := setupVMContexts()

		// Let's set some distinct bits in word 1 and 2
		valA[1] = 0b1010
		valA[2] = 0b0000 // To be written

		// Span A Descriptor at R0
		valA[core.Cfg.R0] = 0        // Ctx (0 = valA)
		valA[core.Cfg.R0+1] = 64     // Offset (start at word 1 bit 0)
		valA[core.Cfg.R0+2] = 4      // Length (4 bits)

		// Span B Descriptor at R3
		valA[core.Cfg.R3] = 0        // Ctx (0 = valA)
		valA[core.Cfg.R3+1] = 128    // Offset (start at word 2 bit 0)
		valA[core.Cfg.R3+2] = 4      // Length (4 bits)

		// Operator: Copy bits from Span A to Span B
		// Opcode 3 (Copy Left)
		instrSpan := encodeInstruction(3, 0x1000|uint16(core.Cfg.R0), 0x1000|uint16(core.Cfg.R3))

		encodeProgram(valA, []uint32{instrSpan, 0})

		backend.UniversalBitwise(unsafe.Pointer(valA), unsafe.Pointer(valB), nil, 1)

		// Check word 2
		So(valA[2], ShouldEqual, 0b1010)
	})
}

func TestExecuteProgram_FirmwareLoad(t *testing.T) {
	Convey("In-band firmware registers hook program execution", t, func() {
		valA, valB, backend := setupVMContexts()

		// Populate a fake firmware in config
		core.Cfg.Firmware = [][]uint32{
			{}, // 0 unused
			{   // 1 acts as bootloader
				encodeInstruction(3, 8, uint16(core.Cfg.RegPC)), // JUMP to 8
				0, // HALT
			},
			{   // 2 acts as actual firmware program
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
