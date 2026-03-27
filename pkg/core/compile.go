package core

import (
	"strconv"
	"strings"
)

/*
CompileFunc compiles the firmware programs from the config
into the proper instruction format.
*/
func CompileFunc(src string) ([]uint32, error) {
	program := make([]uint32, 0)

	for _, line := range strings.Split(src, "\n") {
		// First remove all the comments.
		line = strings.TrimSpace(strings.Split(line, "//")[0])

		for _, chunk := range strings.Split(line, " ") {
			chunk = strings.TrimSpace(chunk)

			if chunk == "" {
				continue
			}

			val, err := parseInstruction(chunk)

			if err != nil {
				return nil, err
			}

			program = append(program, uint32(val))
		}
	}

	return program, nil
}

func parseInstruction(chunk string) (uint64, error) {
	switch chunk[0] {
	case 'r':
		r, err := strconv.ParseUint(chunk[1:], 10, 64)

		if err != nil {
			return 0, err
		}

		switch r {
		case 0:
			return uint64(Cfg.R0), nil
		case 1:
			return uint64(Cfg.R1), nil
		case 2:
			return uint64(Cfg.R2), nil
		case 3:
			return uint64(Cfg.R3), nil
		case 4:
			return uint64(Cfg.R4), nil
		case 5:
			return uint64(Cfg.R5), nil
		}
	case '*':
		return parseInstruction(chunk[1:])
	case 'p':
		return uint64(Cfg.RegPC), nil
	default:
		return strconv.ParseUint(chunk, 10, 64)
	}

	return 0, nil
}
