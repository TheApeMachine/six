package core

import (
	"fmt"
	"strconv"
	"strings"
)

/*
opcodes maps mnemonic names to the truth-table nibble in bits [3:0] of each
32-bit LGP slot. That matches pkg/compute/kernel/cpu.executeScalarInstruction
(op := instr & 0xF). Extended ExecWord opcodes (0x10+) do not fit this packing
until the kernel widens the opcode field.
*/
var opcodes = map[string]uint32{
	"FALSE":       0x0,
	"AND":         0x1,
	"A_AND_NOT_B": 0x2,
	"COPY":        0x3,
	"NOT_A_AND_B": 0x4,
	"B":           0x5,
	"XOR":         0x6,
	"OR":          0x7,
	"NOR":         0x8,
	"XNOR":        0x9,
	"NOT_B":       0xA,
	"A_OR_NOT_B":  0xB,
	"NOT_A":       0xC,
	"NOT_A_OR_B":  0xD,
	"NAND":        0xE,
	"TRUE":        0xF,
}

func parseWordIndex(chunk string) (uint32, error) {
	w, err := strconv.ParseUint(chunk, 10, 32)

	if err != nil || w > 127 {
		return 0, fmt.Errorf("invalid word index %q (expected 0-127)", chunk)
	}

	return uint32(w), nil
}

/*
CompileFunc parses branchless assembly lines of the form "OP SRC_WORD DST_WORD"
into 32-bit slots: [31:18] dst | [17:4] src | [3:0|op] — matching
pkg/compute/kernel/cpu instruction packing.

NOP and HALT compile to instruction 0 (skipped by UniversalBitwise).
*/
func CompileFunc(src string) ([]uint32, error) {
	program := make([]uint32, 0)
	lines := strings.Split(src, "\n")

	for lineNum, line := range lines {
		line = strings.TrimSpace(strings.Split(line, "//")[0])
		line = strings.TrimSpace(strings.Split(line, "#")[0])

		if line == "" {
			continue
		}

		chunks := strings.Fields(line)
		lineNo := lineNum + 1

		if chunks[0] == "NOP" || chunks[0] == "HALT" {
			program = append(program, 0)
			continue
		}

		if len(chunks) != 3 {
			return nil, fmt.Errorf("compile line %d: expected 'OP SRC DST', got: %q", lineNo, line)
		}

		opStr := strings.ToUpper(chunks[0])
		op, ok := opcodes[opStr]

		if !ok {
			val, err := strconv.ParseUint(opStr, 0, 4)

			if err != nil || val > 0xF {
				return nil, fmt.Errorf("compile line %d: unknown opcode %q", lineNo, opStr)
			}

			op = uint32(val)
		}

		srcWord, err := parseWordIndex(chunks[1])

		if err != nil {
			return nil, fmt.Errorf("compile line %d: src: %w", lineNo, err)
		}

		dstWord, err := parseWordIndex(chunks[2])

		if err != nil {
			return nil, fmt.Errorf("compile line %d: dst: %w", lineNo, err)
		}

		instr := (op & 0xF) | (srcWord&0x3FFF)<<4 | (dstWord&0x3FFF)<<18
		program = append(program, instr)
	}

	return program, nil
}
