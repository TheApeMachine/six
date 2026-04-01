package stepwise

import (
	"fmt"
	"strconv"
	"strings"
)

var compileTT = map[string]uint8{
	"FALSE": 0x0, "AND": 0x1, "AANDNOTB": 0x2, "COPY": 0x3, "NOTAANDB": 0x4,
	"B": 0x5, "XOR": 0x6, "OR": 0x7, "NOR": 0x8, "XNOR": 0x9,
	"NOTB": 0xA, "IFBTHENA": 0xB, "NOTA": 0xC, "IFATHENB": 0xD,
	"NAND": 0xE, "TRUE": 0xF,
	"POPCOUNT": 0x10, "SHL": 0x11, "SHR": 0x12, "ADD": 0x13,
}

/*
CompileDescriptors parses a line-oriented program for InstallEmbedded.

	Line syntax (case-insensitive tokens):

	  IMM <dstWord> <imm16>
	  NOP <wordIndex>                  — identity copy on that word
	  <TTNAME> <ia> <ib> <idst> [LEFT_B] [RIGHT_B]

	# and // start comments.
*/
func CompileDescriptors(src string) ([]uint64, error) {

	var out []uint64
	lines := strings.Split(src, "\n")

	for lineNum, raw := range lines {
		line := strings.TrimSpace(raw)
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}

		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("stepwise.CompileDescriptors line %d: empty", lineNum+1)
		}

		op := strings.ToUpper(fields[0])
		lineNo := lineNum + 1

		switch op {

		case "IMM":
			if len(fields) != 3 {
				return nil, fmt.Errorf("compile line %d: IMM wants dst imm", lineNo)
			}

			dst64, errDst := strconv.ParseUint(fields[1], 10, 8)
			if errDst != nil || dst64 > 127 {
				return nil, fmt.Errorf("compile line %d: bad dst", lineNo)
			}

			imm64, errImm := strconv.ParseUint(fields[2], 10, 16)
			if errImm != nil {
				return nil, fmt.Errorf("compile line %d: bad imm", lineNo)
			}

			out = append(out, EncodeImm(uint8(dst64), uint16(imm64)))

		case "NOP":
			if len(fields) != 2 {
				return nil, fmt.Errorf("compile line %d: NOP wants idx", lineNo)
			}

			idx64, errI := strconv.ParseUint(fields[1], 10, 8)
			if errI != nil || idx64 > 127 {
				return nil, fmt.Errorf("compile line %d: bad idx", lineNo)
			}

			idx := uint8(idx64)
			out = append(out, EncodeStep(0x3, idx, idx, idx))

		default:
			ttop, ok := compileTT[op]
			if !ok {
				return nil, fmt.Errorf("compile line %d: unknown op %q", lineNo, fields[0])
			}

			if len(fields) < 4 {
				return nil, fmt.Errorf("compile line %d: TT wants op ia ib idst [flags]", lineNo)
			}

			ia, errA := strconv.ParseUint(fields[1], 10, 8)
			ib, errB := strconv.ParseUint(fields[2], 10, 8)
			id, errD := strconv.ParseUint(fields[3], 10, 8)
			if errA != nil || errB != nil || errD != nil || ia > 127 || ib > 127 || id > 127 {
				return nil, fmt.Errorf("compile line %d: bad word index", lineNo)
			}

			leftB, rightB := false, false

			for _, ext := range fields[4:] {
				switch strings.ToUpper(ext) {

				case "LEFT_B", "LB", "A_FROM_B":
					leftB = true

				case "RIGHT_B", "RB", "B_FROM_B":
					rightB = true

				default:
					return nil, fmt.Errorf("compile line %d: unknown flag %q", lineNo, ext)
				}
			}

			out = append(out, EncodeStepFrames(ttop, uint8(ia), uint8(ib), uint8(id), leftB, rightB))
		}
	}

	return out, nil
}
