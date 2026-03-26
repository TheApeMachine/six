package primitive

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/theapemachine/six/pkg/core"
)

/*
Compile compiles pure Polish Notation source into 32-bit executable instructions.

Syntax (exactly 3 tokens per line):

	src dest gate

Operand notation:
  - Plain number (0–127): IMMEDIATE VALUE (the number itself, not a memory read)
  - rN (r0–r5): register — reads/writes the physical word at that register index
  - pc: program counter register
  - bN or brN: partner (Value B) word or register
  - *N: dereference — read word[N] to get the value (memory load)
  - *rN: dereference through register value
  - *bN: dereference through partner word

Gate: 4-bit binary truth table (0000–1111). All 16 two-input boolean functions.

2-address model: dest = gate(src, dest).
Halt: full 32-bit zero instruction. Program ends at first empty slot.
Skip-if-zero: zero result skips the next instruction (conditional control flow).

Comments: // (full-line or inline).
*/
func Compile(program string) ([]uint32, error) {
	lines := strings.Split(program, "\n")
	var kernel []uint32

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}

		// Strip inline comments
		if idx := strings.Index(line, "//"); idx != -1 {
			line = strings.TrimSpace(line[:idx])
		}
		if line == "" {
			continue
		}

		tokens := strings.Fields(line)
		if len(tokens) != 3 {
			return nil, fmt.Errorf(
				"primitive: expected 3 tokens [src dest gate], got %d in: %s",
				len(tokens), line,
			)
		}

		srcImm, srcDeref, srcPart, srcIdx, err := resolveOperand(tokens[0])
		if err != nil {
			return nil, fmt.Errorf("primitive: bad src %q: %w", tokens[0], err)
		}

		_, destDeref, destPart, destIdx, err := resolveOperand(tokens[1])
		if err != nil {
			return nil, fmt.Errorf("primitive: bad dest %q: %w", tokens[1], err)
		}

		gate, err := strconv.ParseUint(tokens[2], 2, 8)
		if err != nil {
			return nil, fmt.Errorf("primitive: invalid gate %q", tokens[2])
		}

		bin := MakeInstruction(
			uint8(gate),
			srcImm, srcDeref, srcPart, srcIdx,
			destDeref, destPart, destIdx,
			destDeref, destPart, destIdx,
		)
		kernel = append(kernel, bin)
	}

	return kernel, nil
}

// resolveOperand returns (immediate, deref, partner, wordIndex, error).
// Plain numbers are immediate values. Registers and deref are memory operations.
func resolveOperand(token string) (imm bool, deref bool, part bool, idx uint8, err error) {
	// Deref prefix
	if strings.HasPrefix(token, "*") {
		deref = true
		token = token[1:]
	}

	// Partner prefix
	if strings.HasPrefix(token, "b") && len(token) > 1 {
		rest := token[1:]
		if strings.HasPrefix(rest, "r") || isDigit(rest[0]) {
			part = true
			token = rest
		}
	}

	// Program counter
	if token == "pc" {
		return false, deref, part, uint8(core.Cfg.RegPC), nil
	}

	// Register names r0–r5
	if strings.HasPrefix(token, "r") && len(token) >= 2 {
		regStr := token[1:]
		regNum, parseErr := strconv.ParseUint(regStr, 10, 8)
		if parseErr == nil && regNum <= 5 {
			regs := [6]int{core.Cfg.R0, core.Cfg.R1, core.Cfg.R2, core.Cfg.R3, core.Cfg.R4, core.Cfg.R5}
			return false, deref, part, uint8(regs[regNum]), nil
		}
	}

	// Plain number = immediate value (0–127)
	val, parseErr := strconv.ParseUint(token, 10, 8)
	if parseErr != nil {
		return false, false, false, 0, fmt.Errorf("unrecognized operand %q", token)
	}
	if val > 127 {
		return false, false, false, 0, fmt.Errorf("value %d out of range (0-127)", val)
	}
	// If deref is set (e.g. *42), it's a memory read, not immediate.
	// Otherwise plain numbers are immediate values.
	if deref {
		return false, true, part, uint8(val), nil
	}
	return true, false, part, uint8(val), nil
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
