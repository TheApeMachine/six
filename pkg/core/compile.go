package core

import (
	"strconv"
	"strings"
)

func CompileFunc(src string) ([]uint32, error) {
	program := make([]uint32, 0)

	for _, line := range strings.Split(src, "\n") {
		// First remove all the comments.
		line = strings.TrimSpace(strings.Split(line, "//")[0])
		
		if line == "" {
			continue
		}

		chunks := strings.Fields(line)
		if len(chunks) < 3 {
			continue
		}

		srcCode, err := parseInstruction(chunks[0])
		if err != nil {
			return nil, err
		}

		dstCode, err := parseInstruction(chunks[1])
		if err != nil {
			return nil, err
		}

		opVal, err := strconv.ParseUint(chunks[2], 2, 8)
		if err != nil {
			return nil, err
		}

		instr := uint32(opVal&0xF) | (uint32(srcCode&0x3FFF) << 4) | (uint32(dstCode&0x3FFF) << 18)
		program = append(program, instr)
	}

	return program, nil
}

func parseInstruction(chunk string) (uint64, error) {
	if strings.HasPrefix(chunk, "*") {
		val, err := parseInstruction(chunk[1:])
		if err != nil {
			return 0, err
		}
		// Clear 0x2000 if it was a register, then add 0x1000 to mark as span/pointer
		return (val &^ 0x2000) | 0x1000, nil
	}

	if chunk == "pc" {
		return uint64(Cfg.RegPC) | 0x2000, nil
	}
	if chunk == "fw" {
		return uint64(Cfg.FW) | 0x2000, nil
	}

	if strings.HasPrefix(chunk, "r") {
		r, err := strconv.ParseUint(chunk[1:], 10, 64)
		if err != nil {
			return 0, err
		}

		var reg uint64
		switch r {
		case 0:
			reg = uint64(Cfg.R0)
		case 1:
			reg = uint64(Cfg.R1)
		case 2:
			reg = uint64(Cfg.R2)
		case 3:
			reg = uint64(Cfg.R3)
		case 4:
			reg = uint64(Cfg.R4)
		case 5:
			reg = uint64(Cfg.R5)
		}
		return reg | 0x2000, nil
	}

	val, err := strconv.ParseUint(chunk, 10, 64)
	if err != nil {
		return 0, err
	}
	return val, nil
}
