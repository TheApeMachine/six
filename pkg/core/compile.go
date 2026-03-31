package core

import (
	"fmt"
	"strconv"
	"strings"
)

var aluOps = map[string]uint16{
	"FALSE":    0x0,
	"AND":      0x1,
	"COPY":     0x3,
	"XOR":      0x6,
	"OR":       0x7,
	"TRUE":     0xF,
	"POPCOUNT": 0x10,
	"ADD":      0x13,
}

var ctlOps = map[string]uint16{
	"JMPZ": 0,
	"DJNZ": 1,
	"SHL":  2,
	"SHR":  3,
	// Legacy aliases kept for backward compatibility.
	"SKIPZ":  0,
	"SKIPNZ": 1,
}

func parseReg(chunk string) (uint16, error) {
	if !strings.HasPrefix(chunk, "r") {
		return 0, fmt.Errorf("expected register 'r0-r3' got %q", chunk)
	}
	r, err := strconv.ParseUint(chunk[1:], 10, 16)
	if err != nil || r > 3 {
		return 0, fmt.Errorf("invalid register %q", chunk)
	}
	return uint16(r), nil
}

func parseCtx(chunk string) (uint16, error) {
	if chunk == "c0" || chunk == "c" {
		return 0, nil
	}
	if chunk == "c1" || chunk == "p" {
		return 1, nil
	}
	return 0, fmt.Errorf("invalid context %q (expected c0 or c1)", chunk)
}

func parseWord(chunk string) (uint16, error) {
	w, err := strconv.ParseUint(chunk, 10, 16)
	if err != nil || w > 127 {
		return 0, fmt.Errorf("invalid word %q (expected 0-127)", chunk)
	}
	return uint16(w), nil
}

func CompileFunc(src string) ([]uint16, error) {
	program := make([]uint16, 0)
	lines := strings.Split(src, "\n")

	for lineNum, line := range lines {
		line = strings.TrimSpace(strings.Split(line, "//")[0])
		line = strings.TrimSpace(strings.Split(line, "#")[0])
		if line == "" {
			continue
		}

		chunks := strings.Fields(line)
		lineNo := lineNum + 1

		if chunks[0] == "HALT" {
			program = append(program, 0)
			continue
		}

		if len(chunks) < 3 {
			return nil, fmt.Errorf("compile line %d: need more fields, got: %q", lineNo, line)
		}

		clsStr := chunks[0]
		switch clsStr {
		case "MEM":
			if len(chunks) < 4 {
				return nil, fmt.Errorf("compile line %d: MEM needs at least 4 fields", lineNo)
			}

			reg, err := parseReg(chunks[2])
			if err != nil {
				return nil, fmt.Errorf("compile line %d: %w", lineNo, err)
			}

			switch chunks[1] {
			case "LOAD":
				if len(chunks) != 5 {
					return nil, fmt.Errorf("compile line %d: MEM LOAD needs 5 fields (MEM LOAD reg ctx word)", lineNo)
				}
				ctx, err := parseCtx(chunks[3])
				if err != nil {
					return nil, fmt.Errorf("compile line %d: %w", lineNo, err)
				}
				word, err := parseWord(chunks[4])
				if err != nil {
					return nil, fmt.Errorf("compile line %d: %w", lineNo, err)
				}
				// LOAD: [15:14]=01, [13]=0, [12:11]=reg, [10]=ctx, [6:0]=word
				instr := (uint16(1) << 14) | (reg << 11) | (ctx << 10) | word
				program = append(program, instr)

			case "STORE":
				if len(chunks) != 4 {
					return nil, fmt.Errorf("compile line %d: MEM STORE needs 4 fields (MEM STORE reg word)", lineNo)
				}
				word, err := parseWord(chunks[3])
				if err != nil {
					return nil, fmt.Errorf("compile line %d: %w", lineNo, err)
				}
				// STORE: [15:14]=01, [13]=1, [12:11]=reg, [10]=0, [6:0]=word
				instr := (uint16(1) << 14) | (uint16(1) << 13) | (reg << 11) | word
				program = append(program, instr)

			case "IMM":
				if len(chunks) != 4 {
					return nil, fmt.Errorf("compile line %d: MEM IMM needs 4 fields (MEM IMM reg value)", lineNo)
				}
				imm, err := strconv.ParseUint(chunks[3], 0, 16)
				if err != nil || imm > 1023 {
					return nil, fmt.Errorf("compile line %d: MEM IMM value must be 0-1023, got %q", lineNo, chunks[3])
				}
				// IMM: [15:14]=01, [13]=1, [12:11]=reg, [10]=1, [9:0]=immediate
				instr := (uint16(1) << 14) | (uint16(1) << 13) | (reg << 11) | (uint16(1) << 10) | uint16(imm)
				program = append(program, instr)

			case "ILOAD":
				if len(chunks) != 5 {
					return nil, fmt.Errorf("compile line %d: MEM ILOAD needs 5 fields (MEM ILOAD reg ctx addr_reg)", lineNo)
				}
				ctx, err := parseCtx(chunks[3])
				if err != nil {
					return nil, fmt.Errorf("compile line %d: %w", lineNo, err)
				}
				addrReg, err := parseReg(chunks[4])
				if err != nil {
					return nil, fmt.Errorf("compile line %d: %w", lineNo, err)
				}
				// ILOAD: [15:14]=01, [13]=0, [12:11]=reg, [10]=ctx, [9]=1, [1:0]=addrReg
				instr := (uint16(1) << 14) | (reg << 11) | (ctx << 10) | (uint16(1) << 9) | addrReg
				program = append(program, instr)

			case "ISTORE":
				if len(chunks) != 4 {
					return nil, fmt.Errorf("compile line %d: MEM ISTORE needs 4 fields (MEM ISTORE reg addr_reg)", lineNo)
				}
				addrReg, err := parseReg(chunks[3])
				if err != nil {
					return nil, fmt.Errorf("compile line %d: %w", lineNo, err)
				}
				// ISTORE: [15:14]=01, [13]=1, [12:11]=reg, [10]=0, [9]=1, [1:0]=addrReg
				instr := (uint16(1) << 14) | (uint16(1) << 13) | (reg << 11) | (uint16(1) << 9) | addrReg
				program = append(program, instr)

			default:
				return nil, fmt.Errorf("compile line %d: MEM mode must be LOAD, STORE, IMM, ILOAD, or ISTORE", lineNo)
			}

		case "ALU":
			if len(chunks) != 5 {
				return nil, fmt.Errorf("compile line %d: ALU needs 5 fields (ALU op reg ctx word)", lineNo)
			}
			opStr := chunks[1]
			op, ok := aluOps[opStr]
			if !ok {
				if val, err := strconv.ParseUint(opStr, 0, 16); err == nil {
					op = uint16(val)
				} else {
					return nil, fmt.Errorf("compile line %d: unknown ALU opcode %q", lineNo, opStr)
				}
			}

			reg, err := parseReg(chunks[2])
			if err != nil {
				return nil, fmt.Errorf("compile line %d: %w", lineNo, err)
			}
			ctx, err := parseCtx(chunks[3])
			if err != nil {
				return nil, fmt.Errorf("compile line %d: %w", lineNo, err)
			}
			word, err := parseWord(chunks[4])
			if err != nil {
				return nil, fmt.Errorf("compile line %d: %w", lineNo, err)
			}

			var cls uint16 = 2
			if op >= 16 {
				cls = 0 // EXT ALU
				op &= 0xF
			}

			instr := (cls << 14) | (op << 10) | (reg << 8) | (ctx << 7) | word
			program = append(program, instr)

		case "CTL":
			if len(chunks) < 3 {
				return nil, fmt.Errorf("compile line %d: CTL needs at least 3 fields (CTL sub reg [offset])", lineNo)
			}
			subStr := chunks[1]
			sub, ok := ctlOps[subStr]
			if !ok {
				return nil, fmt.Errorf("compile line %d: unknown CTL subcode %q", lineNo, subStr)
			}
			reg, err := parseReg(chunks[2])
			if err != nil {
				return nil, fmt.Errorf("compile line %d: %w", lineNo, err)
			}

			// Optional 10-bit signed offset for JMPZ/DJNZ, or shift count for SHL/SHR.
			var imm uint16
			if len(chunks) == 4 {
				raw, err := strconv.ParseInt(chunks[3], 0, 16)
				if err != nil {
					return nil, fmt.Errorf("compile line %d: CTL offset/imm parse error: %w", lineNo, err)
				}
				imm = uint16(raw) & 0x3FF // keep lower 10 bits (handles negatives via two's complement)
			}

			instr := (uint16(3) << 14) | (sub << 12) | (reg << 10) | imm
			program = append(program, instr)

		default:
			return nil, fmt.Errorf("compile line %d: Expected MEM, ALU, CTL, or HALT. Legacy format unsupported.", lineNo)
		}
	}

	return program, nil
}
