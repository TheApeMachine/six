package telemetry

import (
	"fmt"
	"strings"

	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
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

/*
HumanDescribeValue returns a compact, readable summary of register
pressure and the active boolean op for debugging and visualization.
*/
func HumanDescribeValue(v *primitive.Value) string {
	if v == nil {
		return "nil Value"
	}

	instr := uint8(cpu.ReadRegion(v, cpu.RegionInstruction) & 0xF)
	dPop := cpu.Popcount(v, 0, primitive.DataBits)
	oPop := cpu.Popcount(v, primitive.InstrStart, primitive.InstrBits)

	return fmt.Sprintf(
		"op=%s · popcount data=%d instruction=%d",
		TruthOpName(instr), dPop, oPop,
	)
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
