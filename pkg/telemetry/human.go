package telemetry

import (
	"fmt"
	"strings"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
TruthOpName maps the 4-bit instruction index to a conventional boolean
function name (rows ordered 00, 01, 10, 11 for A,B).
*/
func TruthOpName(instr uint8) string {
	instr &= 0xF
	if int(instr) < len(truthOpNames) {
		return truthOpNames[instr]
	}

	return fmt.Sprintf("op-%X", instr)
}

var truthOpNames = []string{
	"const-0",
	"nor",
	"lt", // A and not B
	"not-b",
	"gt", // not A and B
	"not-a",
	"xor",
	"nand",
	"and",
	"xnor",
	"proj-b",
	"implies",
	"proj-a",
	"converse",
	"or",
	"const-1",
}

// DefaultVMInstruction is the opcode used only when a Value has no in-band
// program to read (HumanDescribeValue fallback). Real paths should use
// InstructionFromValue instead.
const DefaultVMInstruction uint8 = 0b0011

// InstructionFromValue returns the 4-bit truth-table opcode at the first LGP
// slot of the program word referenced by the PC register (PC is an absolute
// word index, usually program.start — not an LGP slot counter).
func InstructionFromValue(v *primitive.Value) uint8 {
	if v == nil {
		return DefaultVMInstruction & 0xF
	}

	pcWord := int(v[core.Cfg.Value.Region.Registers.PC])
	progStart := core.Cfg.Value.Region.Program.Start
	slot := 0

	if pcWord >= progStart {
		slot = 2 * (pcWord - progStart)
	}

	return programOpAt(v, slot) & 0xF
}

/*
HumanDescribeValue returns a compact, readable summary of the new
multi-region architecture for debugging and visualization.
*/
func HumanDescribeValue(v *primitive.Value) string {
	if v == nil {
		return "nil Value"
	}

	instr := uint8(0)
	dataPop := cpu.Popcount(unsafe.Pointer(v), 0, int(core.Cfg.Value.Region.Tokens.Bits))
	affPop := cpu.Popcount(unsafe.Pointer(v), int(core.Cfg.Value.Region.Affinity.Start), int(core.Cfg.Value.Region.Affinity.Bits))
	progPop := cpu.Popcount(unsafe.Pointer(v), int(core.Cfg.Value.Region.Program.Start), int(core.Cfg.Value.Region.Program.Bits))
	tokens := v.String()
	if tokens == "" {
		tokens = v.String()
	}

	// Show program info if present (first payload slot opcode when program bits exist)
	progInfo := ""
	if progPop > 0 {
		first := int(core.PayloadProgramWordOffset) * 2
		instr = programOpAt(v, first)
		progInfo = fmt.Sprintf(" program=%dslots", countProgramSlots(v))
	} else {
		instr = DefaultVMInstruction & 0xF
	}

	return fmt.Sprintf(
		"tokens=%q · op=%s · data=%d aff=%d prog=%d%s",
		tokens,
		TruthOpName(instr), dataPop, affPop, progPop, progInfo,
	)
}

// countProgramSlots counts non-NOP 32-bit LGP slots in the configured program band.
func countProgramSlots(v *primitive.Value) int {
	nSlots := programLGPSlotCount()
	count := 0

	for slot := 0; slot < nSlots; slot++ {
		if fullInstructionAt(v, slot) != 0 {
			count++
		}
	}

	return count
}

func programLGPSlotCount() int {
	nWords := int((core.Cfg.Value.Region.Program.Bits + 63) / 64)

	return nWords * 2
}

func fullInstructionAt(v *primitive.Value, slot int) uint32 {
	if v == nil || slot < 0 || slot >= programLGPSlotCount() {
		return 0
	}

	wordPos := core.Cfg.Value.Region.Program.Start + slot/2

	if wordPos < 0 || wordPos >= core.Cfg.Value.Words {
		return 0
	}

	shift := uint((slot % 2) * 32)

	return uint32(v[wordPos] >> shift)
}

func programOpAt(v *primitive.Value, slot int) uint8 {
	return uint8(fullInstructionAt(v, slot) & 0xF)
}

/*
ASCIIFramePreview turns the first bytes of a 1024-byte frame into a
printable preview for HUD display.
*/
func ASCIIFramePreview(frame []byte, max int) string {
	if max <= 0 {
		max = 80
	}

	end := max
	if end > len(frame) {
		end = len(frame)
	}

	var b strings.Builder

	for i := range end {
		c := frame[i]

		if c >= 32 && c < 127 {
			b.WriteByte(c)
		} else {
			b.WriteByte('.')
		}
	}

	if len(frame) > end {
		b.WriteString("…")
	}

	return b.String()
}
