/*
Package program holds the DSL compiler that lowers human-authored firmware
strings (from cmd/cfg/config.yml) into the native 64-bit instruction format
that the universal-bitwise kernel executes directly.

Lowering happens once at config load. After that the runtime never touches
DSL source again — execution operates only on the packed uint64 instruction
words inside a Value's program region.

Instruction layout (one DSL line = one uint64 word):

	bits  0..6   dstSpan - 1   (7 bits, 1..128)
	bits  7..13  dstStart      (7 bits, 0..127)
	bits 14..20  bSpan - 1     (7 bits, 1..128)
	bits 21..27  bStart        (7 bits, 0..127)
	bits 28..34  aSpan - 1     (7 bits, 1..128)
	bits 35..41  aStart        (7 bits, 0..127)
	bits 42..45  opcode nibble (4 bits, truth table)
	bits     46  mode          (0 = accumulate, 1 = reduce)
	bits 47..63  reserved

A zero word terminates execution. The compiler also returns the uint64 to
write into kernel.SchedulingNextProgramWord (word 117) when the source
includes a trailing `next self` or `next <id>` directive — that word is part
of the install buffer, never written by a separate Go pass.

DSL grammar (one statement per non-empty, non-comment line):

	OpLine   := REGION_REF REGION_REF REGION_REF OPCODE MODE
	NextLine := "next" ("self" | UINT64)

Region refs are `name[start]` or `name[start,span]` (span defaults to 1). The
caller supplies a Layout describing the named regions and binary-string
opcode names so the substrate can re-shape its layout without forking the
compiler.
*/
package program

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Instruction encoding constants. Mirrors the decoder in
// pkg/compute/kernel/cpu/wordblock_universal.go.
const (
	InstrDstSpanShift  = 0
	InstrDstStartShift = 7
	InstrBSpanShift    = 14
	InstrBStartShift   = 21
	InstrASpanShift    = 28
	InstrAStartShift   = 35
	InstrOpcodeShift   = 42
	InstrModeShift     = 46
	InstrImmShift      = 48

	InstrFieldMask  uint64 = 0x7F
	InstrOpcodeMask uint64 = 0xF
	InstrModeMask   uint64 = 0x3
	InstrImmMask    uint64 = 0xFFFF
)

const (
	ModeAccumulate uint64 = 0
	ModeReduce     uint64 = 1
	ModeCmov       uint64 = 2
	ModeImm        uint64 = 3
)

// SelfSentinel marks a `next self` continuation in Compiled.SchedulingNext
// before installation. Callers rewrite it to the resident Value's ID at
// install time.
const SelfSentinel uint64 = 0xFFFFFFFFFFFFFFFF

// RegionExtent describes one named region in a Value frame: starting word
// index plus how many 64-bit words it covers.
type RegionExtent struct {
	Start int
	Words int
}

// Layout pairs a region name table with an opcode-name → 4-bit nibble table.
// Pass core-derived defaults via NewLayout; the compiler treats the names as
// authoritative and never imports the config package directly (avoids a
// cyclic dependency).
type Layout struct {
	Regions map[string]RegionExtent
	Opcodes map[string]uint64
}

// Compiled is the result of lowering a single named program. Words are the
// packed instruction stream that loads into a Value's program region.
// SchedulingNext is the value to write into word 117 when the program is
// installed: 0 (none), SelfSentinel (encoded "self" — see ResolveSchedulingNext),
// or a literal Value ID.
type Compiled struct {
	Words          []uint64
	SchedulingNext uint64
	HasSelfNext    bool
}

// ResolveSchedulingNext picks the actual scheduler word for an install: when
// the program declared `next self` it returns the resident Value's ID;
// otherwise it returns the literal continuation (0 = no follow-up).
func (c Compiled) ResolveSchedulingNext(residentValueID uint64) uint64 {
	if c.HasSelfNext {
		return residentValueID
	}

	return c.SchedulingNext
}

// Compile lowers DSL source into a packed instruction stream against the
// supplied Layout. The returned Compiled is safe to inspect even when err
// is non-nil (it carries whatever lines parsed successfully so callers can
// surface partial diagnostics).
func Compile(source string, lay Layout) (Compiled, error) {
	var (
		out  Compiled
		errs []string
	)

	for lineNo, raw := range strings.Split(source, "\n") {
		line := stripComment(raw)
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)

		if strings.EqualFold(fields[0], "next") {
			if err := parseNext(fields, &out); err != nil {
				errs = append(errs, fmt.Sprintf("line %d: %v", lineNo+1, err))
			}

			continue
		}

		instr, err := parseOpLine(fields, lay)
		if err != nil {
			errs = append(errs, fmt.Sprintf("line %d: %v", lineNo+1, err))

			continue
		}

		out.Words = append(out.Words, instr)
	}

	if len(errs) > 0 {
		return out, errors.New(strings.Join(errs, "; "))
	}

	return out, nil
}

func stripComment(s string) string {
	if i := strings.Index(s, "#"); i >= 0 {
		return s[:i]
	}

	return s
}

func parseNext(fields []string, out *Compiled) error {
	if len(fields) != 2 {
		return fmt.Errorf("`next` requires exactly one argument, got %d", len(fields)-1)
	}

	target := strings.ToLower(fields[1])

	if target == "self" {
		out.HasSelfNext = true
		out.SchedulingNext = SelfSentinel

		return nil
	}

	id, err := strconv.ParseUint(target, 10, 64)
	if err != nil {
		return fmt.Errorf("`next %s`: not `self` and not a uint64", fields[1])
	}

	out.HasSelfNext = false
	out.SchedulingNext = id

	return nil
}

func parseOpLine(fields []string, lay Layout) (uint64, error) {
	if len(fields) != 5 {
		return 0, fmt.Errorf("op line wants `srcA srcB dst op mode`, got %d fields", len(fields))
	}

	var mode uint64
	switch strings.ToLower(fields[4]) {
	case "accumulate":
		mode = ModeAccumulate
	case "reduce":
		mode = ModeReduce
	case "cmov":
		mode = ModeCmov
	case "imm":
		mode = ModeImm
	default:
		return 0, fmt.Errorf("unknown mode %q (want `accumulate`, `reduce`, `cmov`, or `imm`)", fields[4])
	}

	aStart, aSpan, err := parseRegionRef(fields[0], lay)
	if err != nil {
		return 0, fmt.Errorf("srcA: %w", err)
	}

	var bStart, bSpan int
	var imm uint64
	if mode == ModeImm {
		immVal, err := strconv.ParseUint(fields[1], 10, 16)
		if err != nil {
			return 0, fmt.Errorf("srcB (imm): %w", err)
		}
		imm = immVal
	} else {
		bStart, bSpan, err = parseRegionRef(fields[1], lay)
		if err != nil {
			return 0, fmt.Errorf("srcB: %w", err)
		}
	}

	dstStart, dstSpan, err := parseRegionRef(fields[2], lay)
	if err != nil {
		return 0, fmt.Errorf("dst: %w", err)
	}

	op, ok := lay.Opcodes[strings.ToLower(fields[3])]
	if !ok {
		return 0, fmt.Errorf("unknown opcode %q (known: %s)", fields[3], knownOpcodes(lay))
	}

	return EncodeInstruction(aStart, aSpan, bStart, bSpan, dstStart, dstSpan, op, mode, imm), nil
}

func parseRegionRef(token string, lay Layout) (start, span int, err error) {
	open := strings.IndexByte(token, '[')
	if open < 0 || !strings.HasSuffix(token, "]") {
		return 0, 0, fmt.Errorf("region ref %q must look like name[start] or name[start,span]", token)
	}

	name := strings.ToLower(token[:open])

	region, ok := lay.Regions[name]
	if !ok {
		return 0, 0, fmt.Errorf("unknown region %q (known: %s)", name, knownRegions(lay))
	}

	body := token[open+1 : len(token)-1]
	parts := strings.Split(body, ",")

	relStart, parseErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	if parseErr != nil {
		return 0, 0, fmt.Errorf("region ref %q: %v", token, parseErr)
	}

	span = 1

	if len(parts) == 2 {
		spanVal, parseErr := strconv.Atoi(strings.TrimSpace(parts[1]))
		if parseErr != nil {
			return 0, 0, fmt.Errorf("region ref %q span: %v", token, parseErr)
		}

		span = spanVal
	}

	if len(parts) > 2 {
		return 0, 0, fmt.Errorf("region ref %q: too many components", token)
	}

	if relStart < 0 || span <= 0 {
		return 0, 0, fmt.Errorf("region ref %q: start/span must be non-negative", token)
	}

	if relStart+span > region.Words {
		return 0, 0, fmt.Errorf(
			"region ref %q: [%d,%d] exceeds %s region (%d words)",
			token, relStart, span, name, region.Words,
		)
	}

	start = region.Start + relStart

	return start, span, nil
}

// EncodeInstruction packs the seven operand fields into a 64-bit instruction
// word. Out-of-range values are clamped to the field width; zero would mean
// "halt" so spans of zero are silently coerced to one.
func EncodeInstruction(aStart, aSpan, bStart, bSpan, dstStart, dstSpan int, opcode, mode, imm uint64) uint64 {
	if aSpan <= 0 {
		aSpan = 1
	}

	if bSpan <= 0 {
		bSpan = 1
	}

	if dstSpan <= 0 {
		dstSpan = 1
	}

	return ((uint64(dstSpan-1) & InstrFieldMask) << InstrDstSpanShift) |
		((uint64(dstStart) & InstrFieldMask) << InstrDstStartShift) |
		((uint64(bSpan-1) & InstrFieldMask) << InstrBSpanShift) |
		((uint64(bStart) & InstrFieldMask) << InstrBStartShift) |
		((uint64(aSpan-1) & InstrFieldMask) << InstrASpanShift) |
		((uint64(aStart) & InstrFieldMask) << InstrAStartShift) |
		((opcode & InstrOpcodeMask) << InstrOpcodeShift) |
		((mode & InstrModeMask) << InstrModeShift) |
		((imm & InstrImmMask) << InstrImmShift)
}

// DecodeInstruction is the inverse of EncodeInstruction. The kernel's hot
// path inlines the bit math, but tests and tooling go through this helper.
func DecodeInstruction(instr uint64) (aStart, aSpan, bStart, bSpan, dstStart, dstSpan int, opcode, mode, imm uint64) {
	dstSpan = int((instr>>InstrDstSpanShift)&InstrFieldMask) + 1
	dstStart = int((instr >> InstrDstStartShift) & InstrFieldMask)
	bSpan = int((instr>>InstrBSpanShift)&InstrFieldMask) + 1
	bStart = int((instr >> InstrBStartShift) & InstrFieldMask)
	aSpan = int((instr>>InstrASpanShift)&InstrFieldMask) + 1
	aStart = int((instr >> InstrAStartShift) & InstrFieldMask)
	opcode = (instr >> InstrOpcodeShift) & InstrOpcodeMask
	mode = (instr >> InstrModeShift) & InstrModeMask
	imm = (instr >> InstrImmShift) & InstrImmMask

	return aStart, aSpan, bStart, bSpan, dstStart, dstSpan, opcode, mode, imm
}

func knownOpcodes(lay Layout) string {
	names := make([]string, 0, len(lay.Opcodes))
	for name := range lay.Opcodes {
		names = append(names, name)
	}

	sort.Strings(names)

	return strings.Join(names, ", ")
}

func knownRegions(lay Layout) string {
	names := make([]string, 0, len(lay.Regions))
	for name := range lay.Regions {
		names = append(names, name)
	}

	sort.Strings(names)

	return strings.Join(names, ", ")
}
