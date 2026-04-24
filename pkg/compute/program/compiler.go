package program

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Instruction encoding constants
const (
	InstrDstSpanShift  = 0
	InstrDstStartShift = 6

	InstrASpanShift  = 13
	InstrAStartShift = 19

	InstrBSpanShift  = 26
	InstrBStartShift = 32

	InstrOpcodeShift   = 39 // 4 bits
	InstrModeShift     = 43 // 3 bits: 0=Truth, 1=Popcnt, 2=AnyZero, 3=AllOnes
	InstrTopologyShift = 46 // 2 bits: 0=self, 1=next, 2=fold, 3=spawn

	InstrPredStartShift = 48 // 7 bits
	InstrPredCondShift  = 55 // 2 bits: 0=Always, 1=NotZero (!=0), 2=IsZero (==0), 3=GreaterThan (>)

	InstrAIndirectShift = 57 // 1 bit
	InstrBTypeShift     = 58 // 2 bits: 0=Direct, 1=Indirect, 2=Immediate

	InstrScopeShift     = 60 // 4 bits: 0=Community, 1=Prompt, 2=Learner, etc.

	InstrSpanMask  uint64 = 0x3F // 6 bits for spans (up to 64 words)
	InstrStartMask uint64 = 0x7F // 7 bits for starts (up to 128 words)
)

// Topologies
const (
	TopologySelf  = 0
	TopologyNext  = 1
	TopologyFold  = 2
	TopologySpawn = 3
)

// Scopes
const (
	ScopeCommunity = 0
	ScopePrompt    = 1
	ScopeLearner   = 2
	// For future property-based scopes, we might need a dedicated scope payload
)

// Modes
const (
	ModeTruth   = 0
	ModePopcnt  = 1
	ModeAnyZero = 2
	ModeAllOnes = 3
)

var Opcodes = map[string]uint64{
	"0":  0b0000,
	"&":  0b0001,
	"\\": 0b0010,
	"A":  0b0011,
	"/":  0b0100,
	"B":  0b0101,
	"^":  0b0110,
	"|":  0b0111,
	"~|": 0b1000,
	"==": 0b1001,
	"~B": 0b1010,
	"<-": 0b1011,
	"~A": 0b1100,
	"->": 0b1101,
	"~&": 0b1110,
	"1":  0b1111,
}

var Topologies = map[string]uint64{
	"self":  TopologySelf,
	"next":  TopologyNext,
	"fold":  TopologyFold,
	"spawn": TopologySpawn,
}

type RegionExtent struct {
	Start int
	Words int
}

type Layout struct {
	Regions    map[string]RegionExtent
	Properties map[string]int
	Opcodes    map[string]uint64 // Unused in new AST, but kept for signature
}

type Compiled struct {
	Words []uint64
}

// AST Syntax: [ (Target Topology) <= (Expr) ? (Predicate) <= Scope ]
// e.g. [ (16..24 self) <= (0..8 ^ 8..16) <= community ]
// e.g. [ (gradient fold) <= (scratch ^ context) ? (properties.falsified != 0) <= community ]
var parserRe = regexp.MustCompile(`\[\s*\((.*?)\)\s*<=\s*(.*?)\s*(?:\?\s*\((.*?)\))?\s*<=\s*(.*?)\s*\]`)

func Compile(source string, lay Layout) (Compiled, error) {
	var out Compiled
	var errs []string

	for lineNo, raw := range strings.Split(source, "\n") {
		line := stripComment(raw)
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		matches := parserRe.FindStringSubmatch(line)
		if matches == nil {
			errs = append(errs, fmt.Sprintf("line %d: invalid syntax. Must be [ (Target) <= (Expr) <= Scope ]", lineNo+1))
			continue
		}

		targetGrp := matches[1]
		exprGrp := matches[2]
		predGrp := matches[3] // optional
		instr, err := parseInstruction(targetGrp, exprGrp, predGrp, matches[4], lay)
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
	if i := strings.Index(s, ";"); i >= 0 {
		return s[:i]
	}
	if i := strings.Index(s, "#"); i >= 0 {
		return s[:i]
	}
	return s
}

func parseInstruction(targetGrp, exprGrp, predGrp, scopeGrp string, lay Layout) (uint64, error) {
	// 1. Target & Topology
	tParts := strings.Fields(targetGrp)
	if len(tParts) != 2 {
		return 0, fmt.Errorf("target must be 'Region Topology', got %q", targetGrp)
	}
	dstStart, dstSpan, _, err := parseRef(tParts[0], lay)
	if err != nil {
		return 0, fmt.Errorf("target region: %w", err)
	}
	topology, ok := Topologies[strings.ToLower(tParts[1])]
	if !ok {
		return 0, fmt.Errorf("unknown topology %q", tParts[1])
	}

	// 2. Expr & Mode
	var mode uint64 = ModeTruth
	var aStart, aSpan int
	var bStart, bSpan int
	var aInd, bType uint64
	var opcode uint64

	// Extract optional reduction function
	if strings.HasPrefix(exprGrp, "popcnt(") {
		mode = ModePopcnt
		exprGrp = exprGrp[7 : len(exprGrp)-1]
	} else if strings.HasPrefix(exprGrp, "any_zero(") {
		mode = ModeAnyZero
		exprGrp = exprGrp[9 : len(exprGrp)-1]
	} else if strings.HasPrefix(exprGrp, "all_ones(") {
		mode = ModeAllOnes
		exprGrp = exprGrp[9 : len(exprGrp)-1]
	} else if strings.HasPrefix(exprGrp, "(") && strings.HasSuffix(exprGrp, ")") {
		exprGrp = exprGrp[1 : len(exprGrp)-1]
	}

	eParts := strings.Fields(exprGrp)
	if len(eParts) == 1 {
		// e.g. `(0)` or `(A)` or `(rom.unsupervised)`
		if eParts[0] == "0" {
			opcode = Opcodes["0"]
		} else if eParts[0] == "1" {
			opcode = Opcodes["1"]
		} else {
			aStart, aSpan, aInd, err = parseRef(eParts[0], lay)
			if err != nil {
				return 0, fmt.Errorf("expr A: %w", err)
			}
			opcode = Opcodes["A"]
		}
	} else if len(eParts) == 3 {
		// e.g. `0..8 ^ 8..16`
		aStart, aSpan, aInd, err = parseRef(eParts[0], lay)
		if err != nil {
			return 0, fmt.Errorf("expr A: %w", err)
		}
		op, ok := Opcodes[eParts[1]]
		if !ok {
			return 0, fmt.Errorf("unknown operator %q", eParts[1])
		}
		opcode = op

		// B operand could be region or immediate
		if isNumeric(eParts[2]) {
			bType = 2 // Immediate
			imm, _ := strconv.ParseUint(eParts[2], 10, 14)
			bStart = int(imm & 0x7F)
			bSpan = int((imm>>7)&0x7F) + 1
		} else {
			var bInd uint64
			bStart, bSpan, bInd, err = parseRef(eParts[2], lay)
			if err != nil {
				return 0, fmt.Errorf("expr B: %w", err)
			}
			if bInd == 1 {
				bType = 1 // Indirect
			}
		}
	} else {
		return 0, fmt.Errorf("expr must be 'A op B' or 'A', got %q", exprGrp)
	}

	// 2a. Validate topology & fold semantics
	if topology == TopologyFold {
		// Only associative/commutative ops are generally allowed for fold, unless explicit ordering is requested.
		// For now, we reject non-associative ops to enforce SYNTAX.md constraints.
		switch opcode {
		case Opcodes["0"], Opcodes["1"], Opcodes["A"], Opcodes["B"], Opcodes["nota"], Opcodes["notb"]:
			// Ok
		case Opcodes["&"], Opcodes["|"], Opcodes["^"], Opcodes["~&"], Opcodes["~|"], Opcodes["=="]:
			// Ok (associative/commutative)
		default:
			// "->", "<-", "\", "/" etc. are NOT commutative/associative
			return 0, fmt.Errorf("fold topology requires associative/commutative operators, got opcode 0x%x", opcode)
		}
	}
	// 3. Predicate
	var predStart, predCond uint64
	if predGrp != "" {
		pParts := strings.Fields(predGrp)
		if len(pParts) != 3 {
			return 0, fmt.Errorf("predicate must be 'Region OP Value'")
		}
		pStart, _, _, err := parseRef(pParts[0], lay)
		if err != nil {
			return 0, fmt.Errorf("predicate region: %w", err)
		}
		predStart = uint64(pStart)

		// 1: != 0, 2: == 0, 3: >
		// Note: The right hand side of the predicate is currently constrained by our parsing
		// strategy or we only test against 0, but if we assume `pParts[2]` is the target value,
		// we'd need to encode that too. We're currently limited by bit budget for predicates.
		if pParts[1] == "!=" && pParts[2] == "0" {
			predCond = 1
		} else if pParts[1] == "==" && pParts[2] == "0" {
			predCond = 2
		} else if pParts[1] == ">" && pParts[2] == "0" {
			predCond = 3
		} else {
			return 0, fmt.Errorf("predicate condition %q %q not fully supported yet", pParts[1], pParts[2])
		}
	}

	var scope uint64
	switch strings.ToLower(scopeGrp) {
	case "community":
		scope = ScopeCommunity
	case "role.prompt":
		scope = ScopePrompt
	case "role.learner":
		scope = ScopeLearner
	default:
		// For now, if we don't recognize it, just fallback to community
		scope = ScopeCommunity
	}

	return EncodeInstruction(
		aStart, aSpan, bStart, bSpan, dstStart, dstSpan,
		opcode, mode, topology,
		predStart, predCond, aInd, bType, scope,
	), nil
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func parseRef(token string, lay Layout) (start, span int, indirect uint64, err error) {
	if strings.HasPrefix(token, "*") {
		indirect = 1
		token = token[1:]
	}

	// Case 1: Numeric range "16..24" or "16"
	if isNumeric(token) {
		start, _ = strconv.Atoi(token)
		span = 1
		return start, span, indirect, nil
	}
	if parts := strings.Split(token, ".."); len(parts) == 2 {
		if isNumeric(parts[0]) && isNumeric(parts[1]) {
			s, _ := strconv.Atoi(parts[0])
			e, _ := strconv.Atoi(parts[1])
			if e < s {
				return 0, 0, 0, fmt.Errorf("invalid range %s", token)
			}
			return s, e - s, indirect, nil
		}
	}

	// Case 2: Symbolic region "properties.stuck" or "properties[14]"
	if idx := strings.IndexByte(token, '.'); idx >= 0 {
		regionName := strings.ToLower(token[:idx])
		propName := strings.ToLower(token[idx+1:])

		region, ok := lay.Regions[regionName]
		if !ok {
			return 0, 0, 0, fmt.Errorf("unknown region %q", regionName)
		}

		propOffset, ok := lay.Properties[propName]
		if !ok {
			return 0, 0, 0, fmt.Errorf("unknown property %q in region %q", propName, regionName)
		}

		absStart := region.Start + propOffset
		return absStart, 1, indirect, nil
	}

	open := strings.IndexByte(token, '[')
	if open >= 0 && strings.HasSuffix(token, "]") {
		name := strings.ToLower(token[:open])
		region, ok := lay.Regions[name]
		if !ok {
			return 0, 0, 0, fmt.Errorf("unknown region %q", name)
		}
		body := token[open+1 : len(token)-1]
		parts := strings.Split(body, ",")
		relStart, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
		span = 1
		if len(parts) == 2 {
			span, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
		}
		return region.Start + relStart, span, indirect, nil
	}

	// Case 3: Bare symbolic region "program", "tokens", "properties.surprisal"
	// For dot notation, we expect it's a known alias in lay.Regions, OR we implement a hardcoded lookup map.
	// We'll trust lay.Regions has exact mappings like "properties.surprisal" = {Start: 68, Words: 1}.
	name := strings.ToLower(token)
	if region, ok := lay.Regions[name]; ok {
		return region.Start, region.Words, indirect, nil
	}

	return 0, 0, 0, fmt.Errorf("unknown region alias %q", token)
}

func EncodeInstruction(
	aStart, aSpan, bStart, bSpan, dstStart, dstSpan int,
	opcode, mode, topology, predStart, predCond, aInd, bType, scope uint64,
) uint64 {
	if aSpan <= 0 {
		aSpan = 1
	}
	if bSpan <= 0 {
		bSpan = 1
	}
	if dstSpan <= 0 {
		dstSpan = 1
	}

	return ((uint64(dstSpan-1) & InstrSpanMask) << InstrDstSpanShift) |
		((uint64(dstStart) & InstrStartMask) << InstrDstStartShift) |
		((uint64(aSpan-1) & InstrSpanMask) << InstrASpanShift) |
		((uint64(aStart) & InstrStartMask) << InstrAStartShift) |
		((uint64(bSpan-1) & InstrSpanMask) << InstrBSpanShift) |
		((uint64(bStart) & InstrStartMask) << InstrBStartShift) |
		((opcode & 0xF) << InstrOpcodeShift) |
		((mode & 0x7) << InstrModeShift) |
		((topology & 0x3) << InstrTopologyShift) |
		((predStart & InstrStartMask) << InstrPredStartShift) |
		((predCond & 0x3) << InstrPredCondShift) |
		((aInd & 0x1) << InstrAIndirectShift) |
		((bType & 0x3) << InstrBTypeShift) |
		((scope & 0xF) << InstrScopeShift)
}

func DecodeInstruction(instr uint64) (
	aStart, aSpan, bStart, bSpan, dstStart, dstSpan int,
	opcode, mode, topology, predStart, predCond, aInd, bType, scope uint64,
) {
	dstSpan = int((instr>>InstrDstSpanShift)&InstrSpanMask) + 1
	dstStart = int((instr >> InstrDstStartShift) & InstrStartMask)
	aSpan = int((instr>>InstrASpanShift)&InstrSpanMask) + 1
	aStart = int((instr >> InstrAStartShift) & InstrStartMask)
	bSpan = int((instr>>InstrBSpanShift)&InstrSpanMask) + 1
	bStart = int((instr >> InstrBStartShift) & InstrStartMask)
	opcode = (instr >> InstrOpcodeShift) & 0xF
	mode = (instr >> InstrModeShift) & 0x7
	topology = (instr >> InstrTopologyShift) & 0x3
	predStart = (instr >> InstrPredStartShift) & InstrStartMask
	predCond = (instr >> InstrPredCondShift) & 0x3
	aInd = (instr >> InstrAIndirectShift) & 0x1
	bType = (instr >> InstrBTypeShift) & 0x3
	scope = (instr >> InstrScopeShift) & 0xF
	return
}
