package program

import (
	"errors"
	"fmt"
	"math/bits"
	"strconv"
	"strings"
	"sync"
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
	InstrBTypeShift     = 58 // 2 bits: 0=Direct, 1=Indirect, 2=Immediate, 3=Next

	InstrSpanMask  uint64 = 0x3F // 6 bits for spans (up to 64 words)
	InstrStartMask uint64 = 0x7F // 7 bits for starts (up to 128 words)
)

const (
	InstrFlagTargetB     uint64 = 1 << 60
	InstrFlagTargetOwner uint64 = 1 << 61
	InstrFlagAFromB      uint64 = 1 << 62
	InstrFlagBFromA      uint64 = 1 << 63
)

const (
	InstrBTypeDirect uint64 = iota
	InstrBTypeIndirect
	InstrBTypeImmediate
	InstrBTypeNext
)

const predExtended = 3

type predicateKind uint8

const predicatePopcntLTE predicateKind = 1

type predicateSpec struct {
	kind      predicateKind
	start     int
	span      int
	threshold uint64
}

/*
PredicateDeviceSpec is the compact predicate table entry copied into native
GPU kernels so extended predicates keep the same meaning as the CPU map.
*/
type PredicateDeviceSpec struct {
	Kind      uint32
	Start     uint32
	Span      uint32
	Threshold uint64
}

var predicates = struct {
	sync.RWMutex
	next  uint64
	ids   map[predicateSpec]uint64
	specs map[uint64]predicateSpec
}{
	next:  1,
	ids:   make(map[predicateSpec]uint64),
	specs: make(map[uint64]predicateSpec),
}

// Topologies
const (
	TopologySelf  = 0
	TopologyNext  = 1
	TopologyFold  = 2
	TopologySpawn = 3
)

// Modes
const (
	ModeTruth     = 0
	ModePopcnt    = 1
	ModeAnyZero   = 2
	ModeAllOnes   = 3
	ModeGeometric = 4
	ModeEmit      = 5
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

	"compose":  0x10,
	"sandwich": 0x20,
	"reverse":  0x30,
}

var Topologies = map[string]uint64{
	"self":  TopologySelf,
	"next":  TopologyNext,
	"fold":  TopologyFold,
	"spawn": TopologySpawn,
	"emit":  TopologySpawn,
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

func Compile(source string, lay Layout) (Compiled, error) {
	var out Compiled
	var errs []string

	if strings.Contains(source, "{") || strings.Contains(source, "[(") {
		words, err := compileFeedSource(source, lay)
		if err != nil {
			return out, err
		}

		out.Words = words
		return out, nil
	}

	for lineNo, raw := range strings.Split(source, "\n") {
		line := stripComment(raw)
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parser := NewParser(line)
		instrNode, err := parser.ParseInstruction()
		if err != nil {
			errs = append(errs, fmt.Sprintf("line %d: %v", lineNo+1, err))
			continue
		}

		instr, err := compileInstruction(instrNode, lay)
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

type feedSite struct {
	emit bool
	body string
}

type feedAtom struct {
	owner string
	ref   string
	imm   bool
}

type feedExpr struct {
	left     feedAtom
	right    feedAtom
	op       string
	mode     uint64
	implicit bool
}

func compileFeedSource(source string, lay Layout) ([]uint64, error) {
	source = stripFeedComments(source)

	sites, err := parseFeedSites(source)
	if err != nil {
		return nil, err
	}

	out := make([]uint64, 0, len(sites))

	if !strings.Contains(source, "<=") {
		var pendingPredStart, pendingPredCond uint64
		var incoming []feedAtom
		for site := 0; site < len(sites); site++ {
			if predStart, predCond, ok, err := feedSiteGate(sites[site], lay); err != nil {
				return nil, fmt.Errorf("site %d: %w", site+1, err)
			} else if ok {
				pendingPredStart = predStart
				pendingPredCond = predCond
				continue
			}

			words, result, ok, err := compileFeedSite(sites[site], pendingPredStart, pendingPredCond, incoming, lay)
			if err != nil {
				return nil, fmt.Errorf("site %d: %w", site+1, err)
			}
			if ok {
				out = append(out, words...)
				incoming = result
				pendingPredStart = 0
				pendingPredCond = 0
			}
		}

		return out, nil
	}

	var pendingPredStart, pendingPredCond uint64
	var incoming []feedAtom
	for site := len(sites) - 1; site >= 0; site-- {
		if predStart, predCond, ok, err := feedSiteGate(sites[site], lay); err != nil {
			return nil, fmt.Errorf("site %d: %w", len(sites)-site, err)
		} else if ok {
			pendingPredStart = predStart
			pendingPredCond = predCond
			continue
		}

		words, result, ok, err := compileFeedSite(sites[site], pendingPredStart, pendingPredCond, incoming, lay)
		if err != nil {
			return nil, fmt.Errorf("site %d: %w", len(sites)-site, err)
		}
		if ok {
			out = append(out, words...)
			incoming = result
			pendingPredStart = 0
			pendingPredCond = 0
		}
	}

	return out, nil
}

func parseFeedSites(source string) ([]feedSite, error) {
	var sites []feedSite
	emitActive := false

	for pos := 0; pos < len(source); pos++ {
		if source[pos] == '<' && nextNonSpace(source, pos+1) == '[' {
			emitActive = true
			continue
		}

		if source[pos] != '[' {
			continue
		}

		depth := 1
		end := pos + 1

		for ; end < len(source); end++ {
			switch source[end] {
			case '[':
				depth++
			case ']':
				depth--
				if depth == 0 {
					body := strings.TrimSpace(source[pos+1 : end])
					sites = append(sites, feedSite{emit: emitActive, body: body})
					if nextNonSpace(source, end+1) == '>' {
						emitActive = false
					}
					pos = end
					goto next
				}
			}
		}

		return nil, fmt.Errorf("unclosed pipe starting at byte %d", pos)

	next:
	}

	if len(sites) == 0 {
		return nil, fmt.Errorf("feed source contains no pipes")
	}

	return sites, nil
}

func nextNonSpace(source string, after int) byte {
	for idx := after; idx < len(source); idx++ {
		switch source[idx] {
		case ' ', '\t', '\n', '\r':
			continue
		default:
			return source[idx]
		}
	}

	return 0
}

func stripFeedComments(source string) string {
	var out strings.Builder
	inComment := false

	for _, char := range source {
		if inComment {
			if char == '\n' || char == '\r' {
				inComment = false
				out.WriteRune(char)
			}
			continue
		}

		if char == ';' || char == '#' {
			inComment = true
			continue
		}

		out.WriteRune(char)
	}

	return out.String()
}

func compileFeedSite(site feedSite, inheritedPredStart, inheritedPredCond uint64, incoming []feedAtom, lay Layout) ([]uint64, []feedAtom, bool, error) {
	body := strings.TrimSpace(site.body)
	if !strings.Contains(body, "{") {
		body = strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(body, ")"), "("))
		parts := strings.Fields(body)
		if len(parts) == 0 {
			return nil, nil, false, nil
		}
		if site.emit {
			instr, ok, err := compileEmitSite(parts, inheritedPredStart, inheritedPredCond, lay)
			if !ok || err != nil {
				return nil, nil, ok, err
			}

			return []uint64{instr}, nil, true, nil
		}

		instr, ok, err := compileOperationSite(parts, inheritedPredStart, inheritedPredCond, incomingFeedAtom(incoming, 0), lay)
		if !ok || err != nil {
			return nil, nil, ok, err
		}

		return []uint64{instr}, nil, true, nil
	}

	blocks, err := bracedBlocks(body)
	if err != nil {
		return nil, nil, false, err
	}
	if len(blocks) == 0 {
		return nil, nil, false, fmt.Errorf("missing operation block")
	}

	var predStart, predCond uint64
	if inheritedPredCond != 0 {
		predStart = inheritedPredStart
		predCond = inheritedPredCond
	}
	if len(blocks) > 1 && strings.Contains(blocks[1], "?") {
		pred, err := parseFeedPredicate(blocks[1])
		if err != nil {
			return nil, nil, false, err
		}

		predStart, predCond, err = compilePredicate(pred, lay)
		if err != nil {
			return nil, nil, false, err
		}

		blocks = blocks[:1]
	}

	words := make([]uint64, 0, len(blocks))
	result := make([]feedAtom, 0, len(blocks))
	for opIdx, opBlock := range blocks {
		if strings.Contains(opBlock, "{") && strings.Contains(opBlock, "?") {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(opBlock), "{") {
			nested, err := bracedBlocks(opBlock)
			if err != nil {
				return nil, nil, false, err
			}
			if len(nested) == 0 {
				return nil, nil, false, fmt.Errorf("empty nested operation")
			}

			opBlock = nested[0]
		}

		parts := feedFields(opBlock)
		if len(parts) == 0 {
			return nil, nil, false, fmt.Errorf("empty operation")
		}

		var instr uint64
		var ok bool
		if site.emit {
			instr, ok, err = compileEmitOperationSite(parts, predStart, predCond, incomingFeedAtom(incoming, opIdx), lay)
		} else {
			instr, ok, err = compileOperationSite(parts, predStart, predCond, incomingFeedAtom(incoming, opIdx), lay)
		}
		if err != nil {
			return nil, nil, false, err
		}
		if ok {
			words = append(words, instr)
			expr, exprErr := parseFeedExpr(parts)
			if exprErr == nil {
				result = append(result, feedAtom{ref: expr.left.ref})
			}
		}
	}

	return words, result, len(words) > 0, nil
}

func incomingFeedAtom(incoming []feedAtom, idx int) feedAtom {
	if len(incoming) == 0 {
		return feedAtom{}
	}
	if idx < len(incoming) {
		return incoming[idx]
	}
	return incoming[len(incoming)-1]
}

func feedFields(raw string) []string {
	var fields []string
	var token strings.Builder
	depth := 0

	flush := func() {
		if token.Len() == 0 {
			return
		}

		fields = append(fields, strings.ReplaceAll(token.String(), ", ", ","))
		token.Reset()
	}

	for _, char := range raw {
		switch char {
		case ' ', '\t', '\n', '\r':
			if depth == 0 {
				flush()
				continue
			}
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		}

		token.WriteRune(char)
	}

	flush()

	return fields
}

func bracedBlocks(raw string) ([]string, error) {
	var blocks []string

	for idx := 0; idx < len(raw); idx++ {
		if raw[idx] != '{' {
			continue
		}

		start := idx
		depth := 0
		for ; idx < len(raw); idx++ {
			switch raw[idx] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					blocks = append(blocks, strings.TrimSpace(raw[start+1:idx]))
					goto next
				}
			}
		}

		return nil, fmt.Errorf("unclosed operation brace")

	next:
	}

	return blocks, nil
}

func feedSiteGate(site feedSite, lay Layout) (uint64, uint64, bool, error) {
	body := strings.TrimSpace(site.body)
	if !strings.Contains(body, "{") {
		return 0, 0, false, nil
	}

	blocks, err := bracedBlocks(body)
	if err != nil {
		return 0, 0, false, err
	}
	if len(blocks) == 0 || !strings.Contains(blocks[0], "{") || !strings.Contains(blocks[0], "?") {
		return 0, 0, false, nil
	}

	nested, err := bracedBlocks(blocks[0])
	if err != nil {
		return 0, 0, false, err
	}
	if len(nested) == 0 {
		return 0, 0, false, nil
	}

	pred, err := parseNestedGatePredicate(blocks[0], nested[0])
	if err != nil {
		return 0, 0, false, err
	}

	predStart, predCond, err := compilePredicate(pred, lay)
	if err != nil {
		return 0, 0, false, err
	}

	return predStart, predCond, true, nil
}

func parseNestedGatePredicate(block, nested string) (*PredicateNode, error) {
	close := strings.LastIndexByte(block, '}')
	if close < 0 {
		return nil, fmt.Errorf("invalid gate %q", block)
	}

	tail := strings.Fields(block[close+1:])
	if len(tail) == 2 && tail[1] == "?" {
		parts := strings.Fields(nested)
		if len(parts) == 2 && parts[1] == "popcnt" {
			left, err := parseFeedAtom(parts[0])
			if err != nil {
				return nil, err
			}

			return &PredicateNode{
				IsPopcnt:  true,
				Region:    left.ref,
				Op:        "|",
				Threshold: tail[0],
			}, nil
		}
	}

	return parseFeedPredicate(nested)
}

func compileEmitSite(parts []string, predStart, predCond uint64, lay Layout) (uint64, bool, error) {
	dstStart, dstSpan, _, err := parseRef("properties.continuation", lay)
	if err != nil {
		return 0, false, err
	}

	aStart, aSpan, aInd, err := parseRef("id", lay)
	if err != nil {
		return 0, false, err
	}

	return EncodeInstruction(
		aStart, aSpan, 0, 1, dstStart, dstSpan,
		Opcodes["A"], ModeEmit, TopologySpawn,
		predStart, predCond, aInd, 0,
	) | InstrFlagTargetOwner, true, nil
}

func compileEmitOperationSite(parts []string, predStart, predCond uint64, incoming feedAtom, lay Layout) (uint64, bool, error) {
	expr, err := parseFeedExpr(parts)
	if err != nil {
		return 0, false, err
	}

	expr = bindImplicitFeed(expr, incoming)
	expr.mode = ModeEmit

	return compileFeedExprWithTopology(expr, predStart, predCond, TopologySpawn, lay)
}

func compileGateSite(predStart, predCond uint64, lay Layout) (uint64, bool, error) {
	return 0, false, nil
}

func compileOperationSite(parts []string, predStart, predCond uint64, incoming feedAtom, lay Layout) (uint64, bool, error) {
	expr, err := parseFeedExpr(parts)
	if err != nil {
		return 0, false, err
	}

	expr = bindImplicitFeed(expr, incoming)

	return compileFeedExpr(expr, predStart, predCond, lay)
}

func parseFeedExpr(parts []string) (feedExpr, error) {
	stack := make([]feedExpr, 0, len(parts))

	for _, part := range parts {
		if isFeedReducer(part) {
			if len(stack) < 1 {
				return feedExpr{}, fmt.Errorf("reducer %q needs one operand", part)
			}

			src := stack[len(stack)-1]
			stack[len(stack)-1] = feedExpr{
				left: src.left,
				op:   "A",
				mode: feedReducerMode(part),
			}
			continue
		}

		if feedTokenIsOperator(part, len(stack)) {
			if len(stack) < 2 {
				return feedExpr{}, fmt.Errorf("operator %q needs two operands", part)
			}

			right := stack[len(stack)-1]
			left := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			stack = append(stack, feedExpr{
				left:  left.left,
				right: right.left,
				op:    part,
				mode:  ModeTruth,
			})
			continue
		}

		atom, err := parseFeedAtom(part)
		if err != nil {
			return feedExpr{}, err
		}
		stack = append(stack, feedExpr{left: atom, op: "A", mode: ModeTruth})
	}

	if len(stack) == 1 {
		if len(parts) == 1 {
			stack[0].implicit = true
		}

		return stack[0], nil
	}

	if len(stack) == 2 {
		return feedExpr{left: stack[0].left, right: stack[1].left, op: "B", mode: stack[1].mode}, nil
	}

	return feedExpr{}, fmt.Errorf("operation leaves %d stack values: %v", len(stack), parts)
}

func bindImplicitFeed(expr feedExpr, incoming feedAtom) feedExpr {
	if !expr.implicit || incoming.ref == "" {
		return expr
	}

	expr.right = incoming
	expr.op = "B"
	return expr
}

func feedTokenIsOperator(part string, depth int) bool {
	if isNumeric(part) || strings.EqualFold(part, "done") || strings.EqualFold(part, "clear") {
		return false
	}

	if (part == "A" || part == "B") && depth < 2 {
		return false
	}

	_, ok := Opcodes[part]
	return ok
}

func compileFeedExpr(expr feedExpr, predStart, predCond uint64, lay Layout) (uint64, bool, error) {
	return compileFeedExprWithTopology(expr, predStart, predCond, TopologySelf, lay)
}

func compileFeedExprWithTopology(expr feedExpr, predStart, predCond, topology uint64, lay Layout) (uint64, bool, error) {
	aStart, aSpan, aInd, err := parseFeedOperand(expr.left, lay)
	if err != nil {
		return 0, false, fmt.Errorf("left: %w", err)
	}

	bStart, bSpan, bType, err := compileFeedRight(expr.right, lay)
	if err != nil {
		return 0, false, fmt.Errorf("right: %w", err)
	}

	dstRef := feedDestination(expr.left)
	dstStart, dstSpan, _, err := parseRef(dstRef, lay)
	if err != nil {
		return 0, false, fmt.Errorf("target: %w", err)
	}

	opcode, ok := Opcodes[expr.op]
	if !ok {
		return 0, false, fmt.Errorf("unknown operator %q", expr.op)
	}
	if IsGeometricOpcode(opcode) {
		expr.mode = ModeGeometric
	}

	leftOwner := feedOwner(expr.left, "A")
	rightOwner := feedOwner(expr.right, leftOwner)

	flags := uint64(0)
	if leftOwner == "B" {
		flags |= InstrFlagAFromB
		flags |= InstrFlagTargetB
	}
	if leftOwner == "A" {
		flags |= InstrFlagTargetOwner
	}
	if rightOwner == "A" && bType != InstrBTypeImmediate {
		flags |= InstrFlagBFromA
	}
	if leftOwner == "B" && expr.right.owner == "B" && bType == InstrBTypeDirect {
		bType = InstrBTypeNext
	}

	return EncodeInstruction(
		aStart, aSpan, bStart, bSpan, dstStart, dstSpan,
		opcode, expr.mode, topology,
		predStart, predCond, aInd, bType,
	) | flags, true, nil
}

func compileFeedRight(atom feedAtom, lay Layout) (start, span int, bType uint64, err error) {
	if atom.ref == "" && atom.owner == "" && !atom.imm {
		return 0, 1, 0, nil
	}

	start, span, indirect, err := parseFeedOperand(atom, lay)
	if err != nil {
		return 0, 0, 0, err
	}
	if atom.imm {
		return start, span, 2, nil
	}

	return start, span, indirect, nil
}

func feedDestination(left feedAtom) string {
	if left.ref == "" {
		return "signals[0,8]"
	}

	return left.ref
}

func feedOwner(atom feedAtom, fallback string) string {
	owner := strings.ToUpper(atom.owner)
	if owner == "A" || owner == "B" {
		return owner
	}
	if fallback == "B" {
		return "B"
	}
	return "A"
}

func parseFeedPredicate(raw string) (*PredicateNode, error) {
	parts := strings.Fields(raw)
	if len(parts) == 3 {
		left, err := parseFeedAtom(parts[0])
		if err != nil {
			return nil, err
		}

		return &PredicateNode{
			Region: left.ref,
			Op:     parts[2],
			Value:  parts[1],
		}, nil
	}

	if len(parts) == 4 && parts[1] == "popcnt" {
		left, err := parseFeedAtom(parts[0])
		if err != nil {
			return nil, err
		}

		return &PredicateNode{
			IsPopcnt:  true,
			Region:    left.ref,
			Op:        "|",
			Threshold: parts[2],
		}, nil
	}

	return nil, fmt.Errorf("invalid gate %v", parts)
}

func parseFeedAtom(raw string) (feedAtom, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return feedAtom{}, fmt.Errorf("empty operand")
	}

	if strings.HasPrefix(raw, "{") && strings.HasSuffix(raw, "}") {
		return parseFeedAtom(strings.TrimSpace(raw[1 : len(raw)-1]))
	}
	if strings.Contains(raw, "{") && strings.Contains(raw, "}") {
		blocks, err := bracedBlocks(raw)
		if err != nil {
			return feedAtom{}, err
		}
		if len(blocks) == 1 {
			return parseFeedAtom(blocks[0])
		}
	}

	if strings.EqualFold(raw, "A") || strings.EqualFold(raw, "B") {
		return feedAtom{owner: strings.ToUpper(raw), ref: "signals[0,8]"}, nil
	}

	if isNumeric(raw) || strings.EqualFold(raw, "done") || strings.EqualFold(raw, "clear") {
		return feedAtom{ref: strings.ToLower(raw), imm: true}, nil
	}

	if len(raw) > 3 && (raw[0] == 'A' || raw[0] == 'B') && raw[1] == '(' && raw[len(raw)-1] == ')' {
		ref, err := parseFeedAtomRef(raw[2 : len(raw)-1])
		if err != nil {
			return feedAtom{}, err
		}

		return feedAtom{owner: raw[:1], ref: ref}, nil
	}

	if strings.Contains(raw, "[") || isKnownFeedRegion(raw) {
		return feedAtom{ref: raw}, nil
	}

	if isBareFeedRef(raw) {
		return feedAtom{ref: raw}, nil
	}

	return feedAtom{}, fmt.Errorf("operand %q must be A(region), B(region), region, clear, done, or a number", raw)
}

func isBareFeedRef(raw string) bool {
	for idx, char := range raw {
		if char >= 'a' && char <= 'z' {
			continue
		}
		if char >= 'A' && char <= 'Z' {
			continue
		}
		if char >= '0' && char <= '9' && idx > 0 {
			continue
		}
		if char == '_' || char == '.' {
			continue
		}

		return false
	}

	return raw != ""
}

func parseFeedAtomRef(raw string) (string, error) {
	parts := feedFields(raw)
	if len(parts) == 1 {
		return parts[0], nil
	}

	if len(parts) == 3 && isNumeric(parts[1]) && (parts[2] == "<<" || parts[2] == ">>") {
		amount, _ := strconv.Atoi(parts[1])
		if parts[2] == ">>" {
			amount = -amount
		}

		return rotateFeedRef(parts[0], amount)
	}

	return "", fmt.Errorf("invalid operand ref %q", raw)
}

func rotateFeedRef(raw string, amount int) (string, error) {
	open := strings.IndexByte(raw, '[')
	if open < 0 || !strings.HasSuffix(raw, "]") {
		return "", fmt.Errorf("rotation requires indexed region, got %q", raw)
	}

	name := raw[:open]
	body := raw[open+1 : len(raw)-1]
	parts := strings.Split(body, ",")
	if len(parts) == 0 || len(parts) > 2 {
		return "", fmt.Errorf("invalid indexed region %q", raw)
	}

	start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return "", err
	}

	span := 1
	if len(parts) == 2 {
		span, err = strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return "", err
		}
	}
	if span <= 0 {
		return "", fmt.Errorf("invalid span in %q", raw)
	}

	rotated := (start + amount) % span
	if rotated < 0 {
		rotated += span
	}

	return fmt.Sprintf("%s[%d,%d]", name, rotated, span-rotated), nil
}

func isKnownFeedRegion(raw string) bool {
	switch strings.ToLower(raw) {
	case "tokens", "program", "signals", "context", "gradient", "properties", "asset", "prev", "next", "id", "affinity":
		return true
	default:
		return false
	}
}

func parseFeedOperand(atom feedAtom, lay Layout) (start, span int, indirect uint64, err error) {
	if !atom.imm {
		return parseRef(atom.ref, lay)
	}

	switch atom.ref {
	case "clear":
		return 0, 1, 0, nil
	case "done":
		return 4, 1, 0, nil
	default:
		imm, err := strconv.ParseUint(atom.ref, 10, 14)
		if err != nil {
			return 0, 0, 0, err
		}

		return int(imm & 0x7F), int((imm>>7)&0x7F) + 1, 0, nil
	}
}

func isFeedReducer(raw string) bool {
	return raw == "popcnt" || raw == "any_zero" || raw == "all_ones"
}

func feedReducerMode(raw string) uint64 {
	switch raw {
	case "popcnt":
		return ModePopcnt
	case "any_zero":
		return ModeAnyZero
	case "all_ones":
		return ModeAllOnes
	default:
		return ModeTruth
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}

	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}

	return b
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

func compileInstruction(node *InstructionNode, lay Layout) (uint64, error) {
	// 1. Target & Topology
	flags := uint64(0)
	dstStart, dstSpan, _, err := parseRef(node.Target.Region, lay)
	if err != nil {
		return 0, fmt.Errorf("target region: %w", err)
	}
	topologyName := strings.ToLower(node.Target.Topology)
	if topologyName == "b" {
		topologyName = "self"
		flags |= InstrFlagTargetB
	}
	topology, ok := Topologies[topologyName]
	if !ok {
		return 0, fmt.Errorf("unknown topology %q", node.Target.Topology)
	}

	// 2. Expr & Mode
	var mode uint64 = ModeTruth
	var aStart, aSpan int
	var bStart, bSpan int
	var aInd, bType uint64
	var opcode uint64

	switch node.Expr.Mode {
	case "popcnt":
		mode = ModePopcnt
	case "any_zero":
		mode = ModeAnyZero
	case "all_ones":
		mode = ModeAllOnes
	case "saturates":
		return 0, fmt.Errorf("saturates is not a language intrinsic")
	}

	if node.Expr.A == "DONE" && node.Expr.Op == "" && node.Expr.B == "" {
		opcode = Opcodes["B"]
		bType = 2
		bStart = 4
		bSpan = 1
		flags |= InstrFlagTargetOwner
	} else if topologyName == "emit" && node.Expr.A == "A" && node.Expr.Op == "" && node.Expr.B == "" {
		mode = ModeEmit
		opcode = Opcodes["A"]
		flags |= InstrFlagTargetOwner
	} else if node.Expr.Op == "" && node.Expr.B == "" {
		// e.g. `(0)` or `(A)` or `(rom.unsupervised)`
		if node.Expr.A == "0" {
			opcode = Opcodes["0"]
		} else if node.Expr.A == "1" {
			opcode = Opcodes["1"]
		} else if node.Expr.A == "A" {
			opcode = Opcodes["A"]
		} else {
			aStart, aSpan, aInd, err = parseRef(node.Expr.A, lay)
			if err != nil {
				return 0, fmt.Errorf("expr A: %w", err)
			}
			opcode = Opcodes["A"]
		}
	} else if node.Expr.Op == "A" && node.Expr.B == "" {
		aStart, aSpan, aInd, err = parseRef(node.Expr.A, lay)
		if err != nil {
			return 0, fmt.Errorf("expr A: %w", err)
		}
		opcode = Opcodes["A"]
	} else if node.Expr.Op != "" && node.Expr.B != "" {
		// e.g. `0..8 ^ 8..16`
		aStart, aSpan, aInd, err = parseRef(node.Expr.A, lay)
		if err != nil {
			return 0, fmt.Errorf("expr A: %w", err)
		}
		op, ok := Opcodes[node.Expr.Op]
		if !ok {
			return 0, fmt.Errorf("unknown operator %q", node.Expr.Op)
		}
		opcode = op
		if IsGeometricOpcode(opcode) {
			mode = ModeGeometric
		}

		// B operand could be region or immediate
		if isNumeric(node.Expr.B) {
			bType = 2 // Immediate
			imm, _ := strconv.ParseUint(node.Expr.B, 10, 14)
			bStart = int(imm & 0x7F)
			bSpan = int((imm>>7)&0x7F) + 1
		} else {
			var bInd uint64
			bStart, bSpan, bInd, err = parseRef(node.Expr.B, lay)
			if err != nil {
				return 0, fmt.Errorf("expr B: %w", err)
			}
			if bInd == 1 {
				bType = 1 // Indirect
			}
		}
	} else {
		return 0, fmt.Errorf("expr must be 'A op B' or 'A', got %+v", node.Expr)
	}

	// 2a. Validate topology & fold semantics
	if topology == TopologyFold {
		if !isFoldOpcode(opcode) {
			return 0, fmt.Errorf("fold topology requires associative/commutative operators, got opcode 0x%x", opcode)
		}
	}

	var predStart, predCond uint64
	if node.Predicate != nil {
		predStart, predCond, err = compilePredicate(node.Predicate, lay)
		if err != nil {
			return 0, err
		}
	}

	return EncodeInstruction(
		aStart, aSpan, bStart, bSpan, dstStart, dstSpan,
		opcode, mode, topology,
		predStart, predCond, aInd, bType,
	) | flags, nil
}

func compilePredicate(node *PredicateNode, lay Layout) (uint64, uint64, error) {
	if node.IsPopcnt {
		if node.Op != "|" {
			return 0, 0, fmt.Errorf("popcnt predicate must use '| Threshold'")
		}
		threshold, err := strconv.ParseUint(node.Threshold, 10, 7)
		if err != nil {
			return 0, 0, fmt.Errorf("popcnt predicate threshold: %w", err)
		}
		start, span, _, err := parseRef(node.Region, lay)
		if err != nil {
			return 0, 0, fmt.Errorf("popcnt predicate region: %w", err)
		}
		id, err := registerPredicate(predicateSpec{
			kind:      predicatePopcntLTE,
			start:     start,
			span:      span,
			threshold: threshold,
		})
		if err != nil {
			return 0, 0, err
		}
		return id, predExtended, nil
	}

	pStart, _, _, err := parseRef(node.Region, lay)
	if err != nil {
		return 0, 0, fmt.Errorf("predicate region: %w", err)
	}

	if node.Op == "!=" && node.Value == "0" {
		return uint64(pStart), 1, nil
	}
	if node.Op == "==" && node.Value == "0" {
		return uint64(pStart), 2, nil
	}
	if node.Op == ">" && node.Value == "0" {
		return uint64(pStart), predExtended, nil
	}
	return 0, 0, fmt.Errorf("predicate condition %q %q not fully supported yet", node.Op, node.Value)
}

func registerPredicate(spec predicateSpec) (uint64, error) {
	predicates.Lock()
	defer predicates.Unlock()

	if id, ok := predicates.ids[spec]; ok {
		return id, nil
	}
	if predicates.next > InstrStartMask {
		return 0, fmt.Errorf("too many extended predicates")
	}
	id := predicates.next
	predicates.next++
	predicates.ids[spec] = id
	predicates.specs[id] = spec
	return id, nil
}

func PredicateAllows(frame *[128]uint64, predStart, predCond uint64) bool {
	switch predCond {
	case 0:
		return true
	case 1:
		return frame[predStart] != 0
	case 2:
		return frame[predStart] == 0
	case predExtended:
		predicates.RLock()
		spec, ok := predicates.specs[predStart]
		predicates.RUnlock()
		if !ok {
			return frame[predStart] > 0
		}
		switch spec.kind {
		case predicatePopcntLTE:
			count := 0
			for i := 0; i < spec.span; i++ {
				idx := spec.start + i
				if idx >= 128 {
					break
				}
				count += bits.OnesCount64(frame[idx])
			}
			return uint64(count) <= spec.threshold
		default:
			return false
		}
	default:
		return false
	}
}

/*
PredicateDeviceSpecs snapshots the extended predicate registry for native
substrates. IDs are direct table indices because predStart stores the registry
ID when predCond is the extended predicate mode.
*/
func PredicateDeviceSpecs() [128]PredicateDeviceSpec {
	var out [128]PredicateDeviceSpec

	predicates.RLock()
	defer predicates.RUnlock()

	for id, spec := range predicates.specs {
		if id >= uint64(len(out)) {
			continue
		}

		out[id] = PredicateDeviceSpec{
			Kind:      uint32(spec.kind),
			Start:     uint32(spec.start),
			Span:      uint32(spec.span),
			Threshold: spec.threshold,
		}
	}

	return out
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

func IsGeometricOpcode(opcode uint64) bool {
	switch opcode & 0xF0 {
	case 0x10, 0x20, 0x30:
		return true
	default:
		return false
	}
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
	if propOffset, ok := lay.Properties[name]; ok {
		region, ok := lay.Regions["properties"]
		if !ok {
			return 0, 0, 0, fmt.Errorf("unknown properties region")
		}

		return region.Start + propOffset, 1, indirect, nil
	}

	return 0, 0, 0, fmt.Errorf("unknown region alias %q", token)
}

func isFoldOpcode(opcode uint64) bool {
	switch opcode {
	case Opcodes["0"], Opcodes["1"], Opcodes["&"], Opcodes["|"], Opcodes["^"], Opcodes["=="]:
		return true
	default:
		return false
	}
}

func EncodeInstruction(
	aStart, aSpan, bStart, bSpan, dstStart, dstSpan int,
	opcode, mode, topology, predStart, predCond, aInd, bType uint64,
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

	encodedOpcode := opcode
	if IsGeometricOpcode(opcode) {
		mode = ModeGeometric
		encodedOpcode = opcode >> 4
	}

	return ((uint64(dstSpan-1) & InstrSpanMask) << InstrDstSpanShift) |
		((uint64(dstStart) & InstrStartMask) << InstrDstStartShift) |
		((uint64(aSpan-1) & InstrSpanMask) << InstrASpanShift) |
		((uint64(aStart) & InstrStartMask) << InstrAStartShift) |
		((uint64(bSpan-1) & InstrSpanMask) << InstrBSpanShift) |
		((uint64(bStart) & InstrStartMask) << InstrBStartShift) |
		((encodedOpcode & 0xF) << InstrOpcodeShift) |
		((mode & 0x7) << InstrModeShift) |
		((topology & 0x3) << InstrTopologyShift) |
		((predStart & InstrStartMask) << InstrPredStartShift) |
		((predCond & 0x3) << InstrPredCondShift) |
		((aInd & 0x1) << InstrAIndirectShift) |
		((bType & 0x3) << InstrBTypeShift)
}

func DecodeInstruction(instr uint64) (
	aStart, aSpan, bStart, bSpan, dstStart, dstSpan int,
	opcode, mode, topology, predStart, predCond, aInd, bType uint64,
) {
	dstSpan = int((instr>>InstrDstSpanShift)&InstrSpanMask) + 1
	dstStart = int((instr >> InstrDstStartShift) & InstrStartMask)
	aSpan = int((instr>>InstrASpanShift)&InstrSpanMask) + 1
	aStart = int((instr >> InstrAStartShift) & InstrStartMask)
	bSpan = int((instr>>InstrBSpanShift)&InstrSpanMask) + 1
	bStart = int((instr >> InstrBStartShift) & InstrStartMask)
	opcode = (instr >> InstrOpcodeShift) & 0xF
	mode = (instr >> InstrModeShift) & 0x7
	if mode == ModeGeometric {
		opcode <<= 4
	}
	topology = (instr >> InstrTopologyShift) & 0x3
	predStart = (instr >> InstrPredStartShift) & InstrStartMask
	predCond = (instr >> InstrPredCondShift) & 0x3
	aInd = (instr >> InstrAIndirectShift) & 0x1
	bType = (instr >> InstrBTypeShift) & 0x3
	return
}
