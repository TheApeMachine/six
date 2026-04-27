package program

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

const maxSweepWords = 16

/*
CompileFirmware is the only authoring entry: human `program name { … }` source.
*/
func compileFirmwareSource(ctx context.Context, source string, lay Layout) ([]uint64, error) {
	_ = ctx
	trim := strings.TrimSpace(source)
	if !strings.HasPrefix(trim, "program ") {
		return nil, fmt.Errorf("firmware: source must begin with program ")
	}
	rest := strings.TrimSpace(strings.TrimPrefix(trim, "program "))
	idx := strings.IndexByte(rest, '{')
	if idx < 0 {
		return nil, fmt.Errorf("firmware: expected { after program name")
	}
	name := strings.TrimSpace(rest[:idx])
	body, err := extractBraceBlock(rest[idx:])
	if err != nil {
		return nil, err
	}
	body = stripLineComments(body)
	_ = name

	predBase := beginFirmwarePredCompile()
	c := &compiler{
		lay:          lay,
		ops:          MergeOpcodes(lay),
		predKeySlot:  make(map[string]int),
		nextPredSlot: predBase,
	}
	if err := c.compileBody(body, compileEnv{}); err != nil {
		return nil, err
	}
	finishFirmwarePredCompile(c.nextPredSlot)
	if len(c.out) > maxSweepWords {
		return nil, fmt.Errorf("firmware: program exceeds %d sweep slots (got %d)", maxSweepWords, len(c.out))
	}
	return c.out, nil
}

type compileEnv struct {
	whenWord      int
	whenCondNE    bool
	whenActive    bool
	targetB       bool
	hamSlot       int
	hamActive     bool
	hamStart      int
	hamSpan       int
	hamThresh     uint64
	emitSpawn     bool
	maskPopSlot   int
	maskPopActive bool
	inTargetB     bool
}

type compiler struct {
	lay          Layout
	ops          map[string]uint64
	predKeySlot  map[string]int
	nextPredSlot int
	out          []uint64
}

func (comp *compiler) compileBody(body string, env compileEnv) error {
	stmts := splitTopLevel(body)
	runEnv := env

	for _, stmt := range stmts {
		line := strings.TrimSpace(stmt)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "mask ") {
			next, err := comp.compileMaskLine(line, runEnv)
			if err != nil {
				return err
			}

			runEnv = next

			continue
		}
		if err := comp.compileStmt(line, runEnv); err != nil {
			return err
		}
	}

	return nil
}

func (comp *compiler) compileStmt(stmt string, env compileEnv) error {
	stmt = strings.TrimSpace(stmt)
	if stmt == "" {
		return nil
	}
	if strings.HasPrefix(stmt, "target ") {
		return comp.compileTarget(stmt, env)
	}
	if strings.HasPrefix(stmt, "when ") {
		return comp.compileWhen(stmt, env)
	}
	if strings.HasPrefix(stmt, "emit ") {
		return comp.compileEmit(stmt, env)
	}
	if strings.HasPrefix(stmt, "set ") {
		return comp.compileSet(stmt, env)
	}
	if strings.HasPrefix(stmt, "write ") {
		return comp.compileWrite(stmt, env)
	}
	return fmt.Errorf("firmware: unknown statement %q", stmt)
}

func (comp *compiler) compileMaskLine(stmt string, env compileEnv) (compileEnv, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(stmt, "mask "))
	if !strings.Contains(rest, "popcnt") {
		return env, fmt.Errorf("firmware: only mask popcnt(…) forms are supported today")
	}

	rest = strings.TrimSpace(strings.TrimPrefix(rest, "popcnt"))
	arg, cmpTail, err := parseCallArgs(rest)
	if err != nil {
		return env, err
	}

	ref, err := resolveRef(comp.lay, strings.TrimSpace(arg))
	if err != nil {
		return env, err
	}

	threshold, strictLess, err := parseCmpThresholdRel(cmpTail)
	if err != nil {
		return env, err
	}

	kind := PredKindPopcntLTE
	if strictLess {
		kind = PredKindPopcntLT
	}

	key := fmt.Sprintf("mask|popcnt|%s|%d|%v", strings.TrimSpace(arg), threshold, strictLess)
	slot := comp.allocPred(key, PredicateDeviceSpec{
		Kind:      kind,
		Start:     uint64(ref.start),
		Span:      uint64(ref.span),
		Threshold: threshold,
	})

	next := env
	next.maskPopActive = true
	next.maskPopSlot = slot

	return next, nil
}

func (comp *compiler) compileTarget(stmt string, env compileEnv) error {
	rest := strings.TrimPrefix(stmt, "target ")
	rest = strings.TrimSpace(rest)
	if !strings.HasPrefix(rest, "B") {
		return fmt.Errorf("firmware: only target B … is supported")
	}
	rest = strings.TrimSpace(strings.TrimPrefix(rest, "B"))
	var whereExpr, body string
	var err error
	if strings.HasPrefix(rest, "where ") {
		brace := strings.IndexByte(rest, '{')
		if brace < 0 {
			return fmt.Errorf("firmware: target where … missing {")
		}
		wherePart := strings.TrimSpace(rest[len("where "):brace])
		whereExpr = wherePart
		body, err = extractBraceBlock(rest[brace:])
		if err != nil {
			return err
		}
	} else if strings.HasPrefix(rest, "{") {
		body, err = extractBraceBlock(rest)
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("firmware: malformed target")
	}

	sub := env
	sub.inTargetB = true
	if whereExpr != "" {
		slot, hs, hspan, hth, err := comp.allocHammingLine(whereExpr)
		if err != nil {
			return err
		}
		sub.hamActive = true
		sub.hamSlot = slot
		sub.hamStart = hs
		sub.hamSpan = hspan
		sub.hamThresh = hth
	}
	return comp.compileBody(body, sub)
}

func (comp *compiler) compileWhen(stmt string, env compileEnv) error {
	cond, body, err := parseWhen(stmt)
	if err != nil {
		return err
	}
	word, ne, err := comp.parseScalarCompare(cond)
	if err != nil {
		return err
	}
	sub := env
	sub.whenActive = true
	sub.whenWord = word
	sub.whenCondNE = ne
	bodyBlock, err := extractBraceBlock(body)
	if err != nil {
		return err
	}
	return comp.compileBody(bodyBlock, sub)
}

func (comp *compiler) compileEmit(stmt string, env compileEnv) error {
	rest := strings.TrimSpace(strings.TrimPrefix(stmt, "emit "))
	if !strings.HasPrefix(rest, "child") {
		return fmt.Errorf("firmware: only emit child { … }")
	}
	rest = strings.TrimSpace(strings.TrimPrefix(rest, "child"))
	block, err := extractBraceBlock(rest)
	if err != nil {
		return err
	}
	sub := env
	sub.emitSpawn = true
	return comp.compileBody(block, sub)
}

func (comp *compiler) compileSet(stmt string, env compileEnv) error {
	rest := strings.TrimSpace(strings.TrimPrefix(stmt, "set "))
	lhs, rhs, ok := splitArrow(rest)
	if !ok {
		return fmt.Errorf("firmware: set expects dst <- src")
	}
	lhs = strings.TrimSpace(lhs)
	rhs = strings.TrimSpace(rhs)
	ne := env
	ne.targetB = strings.HasPrefix(lhs, "B.")
	return comp.lowerAssign(lhs, rhs, ne, false, false)
}

func (comp *compiler) compileWrite(stmt string, env compileEnv) error {
	rest := strings.TrimSpace(strings.TrimPrefix(stmt, "write "))
	fold := false
	if strings.HasSuffix(rest, " fold") {
		fold = true
		rest = strings.TrimSpace(strings.TrimSuffix(rest, " fold"))
	}
	lhs, rhs, ok := splitArrow(rest)
	if !ok {
		return fmt.Errorf("firmware: write expects dst <- expr")
	}
	lhs = strings.TrimSpace(lhs)
	rhs = strings.TrimSpace(rhs)
	ne := env
	ne.targetB = strings.HasPrefix(lhs, "B.")
	return comp.lowerAssign(lhs, rhs, ne, true, fold)
}

func (comp *compiler) lowerAssign(lhs, rhs string, env compileEnv, isWrite bool, fold bool) error {
	dst, err := resolveRef(comp.lay, lhs)
	if err != nil {
		return err
	}

	if strings.HasPrefix(rhs, "popcnt(") {
		inner := strings.TrimSuffix(strings.TrimPrefix(rhs, "popcnt("), ")")
		inner = strings.TrimSpace(inner)
		src, err := resolveRef(comp.lay, inner)
		if err != nil {
			return err
		}
		return comp.emitReduction(dst, src, ModePopcnt, env, lhs)
	}
	if strings.HasPrefix(rhs, "any_zero(") {
		inner := strings.TrimSuffix(strings.TrimPrefix(rhs, "any_zero("), ")")
		inner = strings.TrimSpace(inner)
		src, err := resolveRef(comp.lay, inner)
		if err != nil {
			return err
		}
		return comp.emitReduction(dst, src, ModeAnyZero, env, lhs)
	}

	callName, callArgs, isCall := parseFunctionCall(rhs)
	if isCall {
		return comp.lowerCall(dst, lhs, callName, callArgs, env, isWrite, fold)
	}

	lit, ok := parseLiteral(rhs, comp.lay)
	if ok {
		return comp.emitCopyImm(dst, lit, env, lhs)
	}

	src, err := resolveRef(comp.lay, rhs)
	if err != nil {
		return err
	}
	return comp.emitCopyRef(dst, src, env, lhs)
}

func (comp *compiler) lowerCall(dst resolvedRef, lhs, name string, args []string, env compileEnv, isWrite bool, fold bool) error {
	_ = isWrite
	switch name {
	case "or", "xor", "and", "and_not", "truth_arrow":
		if len(args) != 2 {
			return fmt.Errorf("firmware: %s expects 2 args", name)
		}
		aRef, err := resolveRef(comp.lay, strings.TrimSpace(args[0]))
		if err != nil {
			return err
		}
		bRef, err := resolveRef(comp.lay, strings.TrimSpace(args[1]))
		if err != nil {
			return err
		}
		opName := map[string]string{
			"or":          "or",
			"xor":         "xor",
			"and":         "and",
			"and_not":     "aandnotb",
			"truth_arrow": "ifathenb",
		}[name]
		op := comp.ops[opName]
		return comp.emitTruth(dst, aRef, bRef, op, ModeTruth, TopologySelf, fold, env, lhs)

	case "shift_left":
		return fmt.Errorf("firmware: shift_left is not implemented (requires rotate semantics in the sweep)")

	default:
		return fmt.Errorf("firmware: unknown call %q", name)
	}
}

func (comp *compiler) emitTruth(
	dst, aRef, bRef resolvedRef,
	opcode, mode, topology uint64,
	fold bool,
	env compileEnv,
	lhs string,
) error {
	topo := topology
	if fold {
		topo = TopologyFold
	}
	predStart, predCond := comp.predFromEnv(env)
	flags := instrTargetFlags(lhs)
	aStart, aSpan := aRef.start, aRef.span
	bStart, bSpan := bRef.start, bRef.span
	bType := InstrBTypeDirect

	if env.targetB {
		flags |= InstrFlagTargetB
		if aRef.actor == actorB {
			flags |= InstrFlagAFromB
		}
		if bRef.actor == actorA {
			flags |= InstrFlagBFromA
		}
	}

	if env.emitSpawn {
		topo = TopologySpawn
		mode = ModeEmit
		if aRef.actor == actorA {
			flags |= InstrFlagBFromA
		}
	}

	enc := EncodeInstruction(
		aStart, aSpan, bStart, bSpan, dst.start, dst.span,
		opcode, mode, topo,
		uint64(predStart), predCond, 0, bType,
	) | flags

	comp.out = append(comp.out, enc)
	return nil
}

func instrTargetFlags(lhs string) uint64 {
	// A.* always names the program owner in gossip; route writes to owner, not the executing lane.
	if strings.HasPrefix(strings.TrimSpace(lhs), "A.") {
		return InstrFlagTargetOwner
	}
	return 0
}

func (comp *compiler) emitCopyRef(dst, src resolvedRef, env compileEnv, lhs string) error {
	op := comp.ops["a"]
	predStart, predCond := comp.predFromEnv(env)
	flags := instrTargetFlags(lhs)
	mode := ModeTruth
	topo := TopologySelf
	if env.emitSpawn {
		mode = ModeEmit
		topo = TopologySpawn
	}
	if env.targetB {
		flags |= InstrFlagTargetB
		if src.actor == actorB {
			flags |= InstrFlagAFromB
		}
	}
	if env.emitSpawn && src.actor == actorA {
		flags |= InstrFlagBFromA
	}

	enc := EncodeInstruction(
		src.start, src.span, 0, 1, dst.start, dst.span,
		op, mode, topo,
		uint64(predStart), predCond, 0, InstrBTypeImmediate,
	) | flags

	comp.out = append(comp.out, enc)
	return nil
}

func (comp *compiler) emitCopyImm(dst resolvedRef, imm uint64, env compileEnv, lhs string) error {
	op := comp.ops["b"]
	predStart, predCond := comp.predFromEnv(env)
	flags := instrTargetFlags(lhs)
	mode := ModeTruth
	topo := TopologySelf
	if env.emitSpawn {
		mode = ModeEmit
		topo = TopologySpawn
	}
	if env.targetB {
		flags |= InstrFlagTargetB
	}
	bStart := int(imm & 0x7f)
	bSpan := 1
	enc := EncodeInstruction(
		0, 1, bStart, bSpan, dst.start, dst.span,
		op, mode, topo,
		uint64(predStart), predCond, 0, InstrBTypeImmediate,
	) | flags
	comp.out = append(comp.out, enc)
	return nil
}

func (comp *compiler) emitReduction(dst, src resolvedRef, mode uint64, env compileEnv, lhs string) error {
	op := comp.ops["a"]
	predStart, predCond := comp.predFromEnv(env)
	flags := instrTargetFlags(lhs)
	if env.targetB {
		flags |= InstrFlagTargetB
		flags |= InstrFlagAFromB
	}
	if env.emitSpawn {
		flags |= InstrFlagBFromA
	}
	topo := TopologySelf
	if env.emitSpawn {
		topo = TopologySpawn
	}
	enc := EncodeInstruction(
		src.start, src.span, 0, 1, dst.start, dst.span,
		op, mode, topo,
		uint64(predStart), predCond, 0, InstrBTypeImmediate,
	) | flags
	comp.out = append(comp.out, enc)
	return nil
}

func (comp *compiler) predFromEnv(env compileEnv) (predStart int, predCond uint64) {
	if env.whenActive && env.hamActive {
		kind := PredKindHammingLTAndScalarEq0
		if env.whenCondNE {
			kind = PredKindHammingLTAndScalarNE0
		}
		key := fmt.Sprintf("ham|scalar|%d|%d|%d|%d|%d", env.hamStart, env.hamSpan, env.hamThresh, env.whenWord, kind)
		slot := comp.allocPred(key, PredicateDeviceSpec{
			Kind:      kind,
			Start:     uint64(env.hamStart),
			Span:      uint64(env.hamSpan),
			Threshold: env.hamThresh,
			AndWord:   uint64(env.whenWord),
		})

		return slot, 3
	}
	if env.whenActive {
		if env.whenCondNE {
			return env.whenWord, 1
		}
		return env.whenWord, 2
	}
	if env.hamActive {
		return env.hamSlot, 3
	}
	if env.maskPopActive {
		return env.maskPopSlot, 3
	}

	return 0, 0
}

func (comp *compiler) allocPred(key string, spec PredicateDeviceSpec) int {
	if slot, ok := comp.predKeySlot[key]; ok {
		SetPredicateSpecSlot(slot, spec)
		return slot
	}
	slot := comp.nextPredSlot
	if slot >= 127 {
		panic("firmware: predicate slot overflow")
	}
	comp.nextPredSlot++
	comp.predKeySlot[key] = slot
	SetPredicateSpecSlot(slot, spec)
	return slot
}

func (comp *compiler) allocHammingLine(whereLine string) (slot int, start, span int, th uint64, err error) {
	aPath, bPath, thresh, err := parseHammingLine(whereLine)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	aRef, err := resolveRef(comp.lay, aPath)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	bRef, err := resolveRef(comp.lay, bPath)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	if aRef.start != bRef.start || aRef.span != bRef.span {
		return 0, 0, 0, 0, fmt.Errorf("firmware: hamming spans must match")
	}
	key := fmt.Sprintf("ham:%d:%d:%d", aRef.start, aRef.span, thresh)
	slot = comp.allocPred(key, PredicateDeviceSpec{
		Kind:      PredKindHammingLT,
		Start:     uint64(aRef.start),
		Span:      uint64(aRef.span),
		Threshold: thresh,
	})

	return slot, aRef.start, aRef.span, thresh, nil
}

func (comp *compiler) parseScalarCompare(cond string) (word int, ne bool, err error) {
	cond = strings.TrimSpace(cond)
	if strings.HasSuffix(cond, "!= 0") {
		prefix := strings.TrimSpace(strings.TrimSuffix(cond, "!= 0"))
		ref, err := resolveRef(comp.lay, prefix)
		if err != nil {
			return 0, false, err
		}
		if ref.span != 1 {
			return 0, false, fmt.Errorf("firmware: scalar compare needs span 1")
		}
		return ref.start, true, nil
	}
	if strings.HasSuffix(cond, "== 0") {
		prefix := strings.TrimSpace(strings.TrimSuffix(cond, "== 0"))
		ref, err := resolveRef(comp.lay, prefix)
		if err != nil {
			return 0, false, err
		}
		if ref.span != 1 {
			return 0, false, fmt.Errorf("firmware: scalar compare needs span 1")
		}
		return ref.start, false, nil
	}
	return 0, false, fmt.Errorf("firmware: unsupported when %q", cond)
}

func parseLiteral(rhs string, lay Layout) (uint64, bool) {
	rhs = strings.TrimSpace(rhs)
	if n, err := strconv.ParseUint(rhs, 10, 64); err == nil {
		return n, true
	}
	if lay.StatusValue != nil {
		if v, ok := lay.StatusValue[strings.ToUpper(rhs)]; ok {
			return v, true
		}
		if v, ok := lay.StatusValue[rhs]; ok {
			return v, true
		}
	}
	return 0, false
}

func parseFunctionCall(rhs string) (name string, args []string, ok bool) {
	rhs = strings.TrimSpace(rhs)
	idx := strings.IndexByte(rhs, '(')
	if idx < 0 || !strings.HasSuffix(rhs, ")") {
		return "", nil, false
	}
	name = strings.TrimSpace(rhs[:idx])
	inner := strings.TrimSpace(rhs[idx+1 : len(rhs)-1])
	if inner == "" {
		return name, nil, true
	}
	parts := splitArgs(inner)
	return name, parts, true
}

func splitArgs(inner string) []string {
	var out []string
	parenDepth := 0
	bracketDepth := 0
	start := 0
	for idx, r := range inner {
		switch r {
		case '(':
			parenDepth++
		case ')':
			parenDepth--
		case '[':
			bracketDepth++
		case ']':
			bracketDepth--
		case ',':
			if parenDepth == 0 && bracketDepth == 0 {
				out = append(out, strings.TrimSpace(inner[start:idx]))
				start = idx + 1
			}
		}
	}
	out = append(out, strings.TrimSpace(inner[start:]))
	return out
}

func parseCallArgs(s string) (arg string, cmpTail string, err error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "(") {
		return "", "", fmt.Errorf("firmware: expected (")
	}
	depth := 0
	for idx := 0; idx < len(s); idx++ {
		switch s[idx] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				arg = strings.TrimSpace(s[1:idx])
				cmpTail = strings.TrimSpace(s[idx+1:])
				return arg, cmpTail, nil
			}
		}
	}
	return "", "", fmt.Errorf("firmware: unbalanced paren in %q", s)
}

func parseCmpThreshold(tail string) (uint64, error) {
	tail = strings.TrimSpace(tail)
	if strings.HasPrefix(tail, "<") {
		n, err := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(tail, "<")), 10, 64)
		return n, err
	}
	return 0, fmt.Errorf("firmware: expected comparison tail, got %q", tail)
}

func parseCmpThresholdRel(tail string) (threshold uint64, strictLess bool, err error) {
	tail = strings.TrimSpace(tail)
	if strings.HasPrefix(tail, "<=") {
		n, perr := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(tail, "<=")), 10, 64)
		return n, false, perr
	}
	if strings.HasPrefix(tail, "<") {
		n, perr := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(tail, "<")), 10, 64)
		return n, true, perr
	}
	return 0, false, fmt.Errorf("firmware: expected < or <= threshold, got %q", tail)
}
