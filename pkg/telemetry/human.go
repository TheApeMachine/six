package telemetry

import (
	"fmt"
	"strings"

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

func ReadVMInstruction() uint8 {
	return 0b0011
}

/*
HumanDescribeValue returns a compact, readable summary of the new
multi-region architecture for debugging and visualization.
*/
func HumanDescribeValue(v *primitive.Value) string {
	if v == nil {
		return "nil Value"
	}

	instr := ReadVMInstruction() & 0xF
	dataPop := cpu.Popcount(v, 0, int(core.Cfg.TokenBits))
	affPop := cpu.Popcount(v, int(core.Cfg.AffinityIndex), 64) // first 64 bits of affinity
	progPop := cpu.Popcount(v, int(core.Cfg.ProgramIndex), 64) // first 64 bits of program

	// Show program info if present
	progInfo := ""
	if progPop > 0 {
		progInfo = fmt.Sprintf(" program=%dops", countProgramOps(v))
	}

	return fmt.Sprintf(
		"op=%s · data=%d aff=%d prog=%d%s",
		TruthOpName(instr), dataPop, affPop, progPop, progInfo,
	)
}

// countProgramOps counts how many non-zero operations are in the program region.
func countProgramOps(v *primitive.Value) int {
	count := 0
	for i := 0; i < 8; i++ {
		if ReadVMInstruction() != 0 {
			count++
		} else {
			break // stop at first HALT
		}
	}
	return count
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
