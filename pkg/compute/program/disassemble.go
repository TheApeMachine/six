package program

import (
	"fmt"
	"strings"
)

func Disassemble(words []uint64) string {
	return FormatProgramSweep16(words)
}

func FormatProgramSweep16(words []uint64) string {
	var builder strings.Builder

	for slot := 0; slot < 16; slot++ {
		if slot > 0 {
			builder.WriteByte('\n')
		}

		if slot >= len(words) || words[slot] == 0 {
			fmt.Fprintf(&builder, "slot %02d: empty", slot)
			continue
		}

		aStart, aSpan, bStart, bSpan, dstStart, dstSpan, opcode, mode, topology, predStart, predCond, _, _, predicate, emit, srcAFromB, stage, popEnd := DecodeInstruction(words[slot])
		fmt.Fprintf(
			&builder,
			"slot %02d: op=0x%x A[%d,%d] B[%d,%d] -> dst[%d,%d] mask=%d mode=%d topo=%d pred=%d cond=%d emit=%d srcAFromB=%d stage=%d popEnd=%d",
			slot,
			opcode,
			aStart,
			aSpan,
			bStart,
			bSpan,
			dstStart,
			dstSpan,
			predStart,
			mode,
			topology,
			predicate,
			predCond,
			emit,
			srcAFromB,
			stage,
			popEnd,
		)
	}

	return builder.String()
}
