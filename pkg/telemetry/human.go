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

// InstructionFromValue returns the current 4-bit opcode at the Value's PC.
// When v is nil or the PC is out of range, it returns DefaultVMInstruction.
func InstructionFromValue(v *primitive.Value) uint8 {
	if v == nil {
		return DefaultVMInstruction & 0xF
	}
	pc := int(v[core.Cfg.RegPC])
	return programOpAt(v, pc) & 0xF
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
	dataPop := cpu.Popcount(unsafe.Pointer(v), 0, int(core.Cfg.TokenBits))
	affPop := cpu.Popcount(unsafe.Pointer(v), int(core.Cfg.AffinityIndex), int(core.Cfg.AffinityBits))
	progPop := cpu.Popcount(unsafe.Pointer(v), int(core.Cfg.ProgramIndex), int(core.Cfg.ProgramBits))
	tokens := v.String()
	if tokens == "" {
		tokens = v.String()
	}

	// Show program info if present (first-slot opcode when program bits exist)
	progInfo := ""
	if progPop > 0 {
		instr = programOpAt(v, 0)
		progInfo = fmt.Sprintf(" program=%dops", countProgramOps(v))
	} else {
		instr = DefaultVMInstruction & 0xF
	}

	return fmt.Sprintf(
		"tokens=%q · op=%s · data=%d aff=%d prog=%d%s",
		tokens,
		TruthOpName(instr), dataPop, affPop, progPop, progInfo,
	)
}

// countProgramOps counts instruction slots until VM HALT (opcode 0 with slot > 0), matching the CPU core loop.
func countProgramOps(v *primitive.Value) int {
	count := 0
	for i := 0; i < core.Cfg.MaxPC; i++ {
		op := programOpAt(v, i)
		if op == 0 && i > 0 {
			break
		}
		if op != 0 {
			count++
		}
	}
	return count
}

func programOpAt(v *primitive.Value, slot int) uint8 {
	if v == nil || slot < 0 || slot >= core.Cfg.MaxPC {
		return 0
	}
	wordPos := core.Cfg.ProgramIndex + slot/2
	if wordPos < 0 || wordPos >= primitive.Words {
		return 0
	}
	shift := uint((slot % 2) * 32)
	instr := uint32(v[wordPos] >> shift)
	return uint8(instr & 0xF)
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
