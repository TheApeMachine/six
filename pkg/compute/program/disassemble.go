package program

import (
	"fmt"
	"strings"
)

/*
Disassembler attaches the layout snapshot used when authoring ProgramIR dumps.
The layout informs future labeling work while Sweep16 rendering stays pure.
TODO: Thread layout into textual dumps when named spans are rendered alongside slots.
*/
type Disassembler struct {
	layout Layout
}

/*
NewDisassembler captures the lowering layout mirrored by Compiler so tooling can
resolve named regions alongside raw spans when richer dumps land.
*/
func NewDisassembler(layout Layout) Disassembler {
	return Disassembler{layout: layout}
}

/*
Disassemble returns the Sweep16 textual view for machine words wired exactly as kernels
consume them—native uint64 storage, no endian swap. The formatted string allocates only
scratch builder memory and touches neither shared Value state nor callee buffers.
*/
func Disassemble(words []uint64) string {
	return Disassembler{}.Disassemble(words)
}

/*
Disassemble emits the Sweep16 dump while retaining the snapped layout captured at
construction for diagnostics parity with EncoderIR.
*/
func (dis Disassembler) Disassemble(words []uint64) string {
	return FormatProgramSweep16(words)
}

/*
FormatProgramSweep16 prints Sweep16: sixteen rows ("slot NN") where NN is zero padded.
Occupied slots print DecodeInstruction fields decoded from each packed uint64 limb:
four-bit opcode in hex (`op=0xN`), unpacked seven-bit SrcA/B/dst spans, predStart masking
lane (same bitfield kernels call mask start), topological routing two-bit code, predicate
enable plus three-bit PredicateCond, srcAFromB, and the reserved high bits. Empty words
render `slot NN: empty`; slices short of sixteen implicitly continue with empties until
the sweep completes row fifteen. Historical DecodeInstruction stubs (aIndirect, bType)
stay zero-reserved internally and omit from textual output altogether. Returned text is
deterministic for a given slice and newline-separated without trailing separators.
*/
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

		// Legacy indirect decode slots eleven and twelve are always zero yet reserved for codegen parity with DecodeInstruction.
		aStart, aSpan, bStart, bSpan, dstStart, dstSpan, opcode, mode, topology, predStart, predCond,
			_, _, predicate, emit, srcAFromB, stage, popEnd := DecodeInstruction(words[slot])

		fmt.Fprintf(
			&builder,
			"slot %02d: op=0x%x A[%d,%d] B[%d,%d] -> dst[%d,%d] predStart=%d mode=%d topo=%d pred=%d cond=%d emit=%d srcAFromB=%d stage=%d popEnd=%d",
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
