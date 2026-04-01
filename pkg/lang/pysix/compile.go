package pysix

import (
	"encoding/json"
	"fmt"

	"github.com/theapemachine/six/pkg/compute/stepwise"
)

const (
	slotZero uint8 = 126
	slotOnes uint8 = 127
	tempLo   uint8 = 64
	tempHi   uint8 = 71
	localLo  uint8 = 80
	spillLo  uint8 = 92
	spillHi  uint8 = 125
)

/*
Compiler lowers exported Python JSON (map from Parse) into stepwise descriptors.
*/
type Compiler struct {
	prog       []uint64
	locals     map[string]uint8
	nextLocal  uint8
	tempCursor uint8
	nextSpill  uint8
}

/*
Compile returns a program slice to pass to stepwise.RunScalar and a map of
Python local names to word indices into the frame.
*/
func Compile(mod map[string]interface{}) ([]uint64, map[string]uint8, error) {

	if k, ok := mod["kind"].(string); !ok || k != "Module" {
		return nil, nil, fmt.Errorf("pysix.Compile: root must be Module")
	}

	body, ok := mod["body"].([]interface{})

	if !ok {
		return nil, nil, fmt.Errorf("pysix.Compile: module body missing")
	}

	compiler := &Compiler{
		locals:    make(map[string]uint8),
		nextLocal: localLo,
		nextSpill: spillLo,
	}

	compiler.emitPrologue()

	for _, raw := range body {
		stmt, ok := raw.(map[string]interface{})

		if !ok {
			return nil, nil, fmt.Errorf("pysix.Compile: bad stmt node")
		}

		if err := compiler.compileStmt(stmt); err != nil {
			return nil, nil, err
		}
	}

	outLocals := make(map[string]uint8, len(compiler.locals))

	for name, slot := range compiler.locals {
		outLocals[name] = slot
	}

	return compiler.prog, outLocals, nil
}

/*
CompileSource parses Python text and compiles it.
*/
func CompileSource(python string) ([]uint64, map[string]uint8, error) {

	mod, err := Parse(python)

	if err != nil {
		return nil, nil, err
	}

	return Compile(mod)
}

func (compiler *Compiler) emitPrologue() {

	compiler.prog = append(compiler.prog, stepwise.EncodeImm(slotZero, 0))
	compiler.prog = append(compiler.prog, stepwise.EncodeStep(0x0C, slotZero, slotZero, slotOnes))
}

func (compiler *Compiler) resetTemps() {

	compiler.tempCursor = tempLo
	compiler.nextSpill = spillLo
}

func (compiler *Compiler) allocTemp() (uint8, error) {

	if compiler.tempCursor <= tempHi {
		compiler.tempCursor++

		return compiler.tempCursor - 1, nil
	}

	if compiler.nextSpill > spillHi {
		return 0, fmt.Errorf("pysix: out of spill slots")
	}

	s := compiler.nextSpill
	compiler.nextSpill++

	return s, nil
}

func (compiler *Compiler) localOf(name string) (uint8, error) {

	slot, ok := compiler.locals[name]

	if !ok {
		if compiler.nextLocal > spillLo-1 {
			return 0, fmt.Errorf("pysix: too many distinct locals")
		}

		slot = compiler.nextLocal
		compiler.nextLocal++
		compiler.locals[name] = slot
	}

	return slot, nil
}

func (compiler *Compiler) compileStmt(stmt map[string]interface{}) error {

	compiler.resetTemps()

	kind, ok := stmt["kind"].(string)

	if !ok {
		return fmt.Errorf("pysix: stmt without kind")
	}

	switch kind {

	case "Pass", "FuncDef":
		return nil

	case "ExprStmt":
		val, ok := stmt["value"].(map[string]interface{})

		if !ok {
			return fmt.Errorf("pysix: ExprStmt missing value")
		}

		_, err := compiler.evalExpr(val)

		return err

	case "Assign":
		return compiler.compileAssign(stmt)

	case "AugAssign":
		return compiler.compileAugAssign(stmt)

	case "If":
		return compiler.compileIfRestricted(stmt)

	case "ForRange":
		return compiler.compileForRange(stmt)

	case "While":
		return fmt.Errorf("pysix: while not supported (use for _ in range(n) with constant n)")

	default:
		return fmt.Errorf("pysix: unsupported stmt %q", kind)
	}
}

func (compiler *Compiler) compileAssign(stmt map[string]interface{}) error {

	target, ok := stmt["target"].(map[string]interface{})

	if !ok {
		return fmt.Errorf("pysix: Assign target")
	}

	tKind, _ := target["kind"].(string)

	if tKind != "Name" {
		return fmt.Errorf("pysix: assign target must be a Name")
	}

	name, _ := target["id"].(string)

	if name == "" {
		return fmt.Errorf("pysix: empty target name")
	}

	val, ok := stmt["value"].(map[string]interface{})

	if !ok {
		return fmt.Errorf("pysix: Assign value")
	}

	dst, err := compiler.localOf(name)

	if err != nil {
		return err
	}

	return compiler.evalExprInto(val, dst)
}

func (compiler *Compiler) compileAugAssign(stmt map[string]interface{}) error {

	name, _ := stmt["target_id"].(string)

	if name == "" {
		return fmt.Errorf("pysix: AugAssign name")
	}

	op, _ := stmt["op"].(string)

	val, ok := stmt["value"].(map[string]interface{})

	if !ok {
		return fmt.Errorf("pysix: AugAssign value")
	}

	cur, err := compiler.localOf(name)

	if err != nil {
		return err
	}

	rhsSlot, err := compiler.evalExpr(val)

	if err != nil {
		return err
	}

	switch op {

	case "Add":
		compiler.prog = append(compiler.prog, stepwise.EncodeStep(0x13, cur, rhsSlot, cur))

	case "Sub":
		if err := compiler.emitSubInto(cur, rhsSlot, cur); err != nil {
			return err
		}

	case "Mult":
		k, err := compiler.smallPositiveConst(val)

		if err != nil {
			return fmt.Errorf("pysix: *= requires small positive literal on rhs")
		}

		if k == 0 {
			compiler.prog = append(compiler.prog, stepwise.EncodeImm(cur, 0))

			return nil
		}

		base, err := compiler.allocTemp()

		if err != nil {
			return err
		}

		compiler.prog = append(compiler.prog, stepwise.EncodeStep(0x3, cur, cur, base))

		for i := uint64(1); i < k; i++ {
			compiler.prog = append(compiler.prog, stepwise.EncodeStep(0x13, cur, base, cur))
		}

	default:
		return fmt.Errorf("pysix: aug op %q", op)
	}

	return nil
}

func (compiler *Compiler) smallPositiveConst(expr map[string]interface{}) (uint64, error) {

	if k, ok := expr["kind"].(string); !ok || k != "Num" {
		return 0, fmt.Errorf("not a constant")
	}

	return asUint64(expr["n"])
}

/*
compileIfRestricted requires then and orelse to each be a single Assign to the same Name.
*/
func (compiler *Compiler) compileIfRestricted(stmt map[string]interface{}) error {

	test, ok := stmt["test"].(map[string]interface{})

	if !ok {
		return fmt.Errorf("pysix: If test")
	}

	body, ok := stmt["body"].([]interface{})

	if !ok {
		return fmt.Errorf("pysix: If body")
	}

	orelse, ok := stmt["orelse"].([]interface{})

	if !ok {
		return fmt.Errorf("pysix: If orelse")
	}

	if len(body) != 1 || len(orelse) != 1 {
		return fmt.Errorf("pysix: if/else v1 needs exactly one statement per branch")
	}

	th0, ok := body[0].(map[string]interface{})

	if !ok || th0["kind"] != "Assign" {
		return fmt.Errorf("pysix: then branch must be a single assignment")
	}

	el0, ok := orelse[0].(map[string]interface{})

	if !ok || el0["kind"] != "Assign" {
		return fmt.Errorf("pysix: else branch must be a single assignment")
	}

	tName, err := singleAssignName(th0)

	if err != nil {
		return err
	}

	eName, err := singleAssignName(el0)

	if err != nil {
		return err
	}

	if tName != eName {
		return fmt.Errorf("pysix: if branches must assign the same variable")
	}

	condMask, err := compiler.evalCompareAsMask(test)

	if err != nil {
		return err
	}

	dst, err := compiler.localOf(tName)

	if err != nil {
		return err
	}

	tThen, err := compiler.allocTemp()

	if err != nil {
		return err
	}

	tElse, err := compiler.allocTemp()

	if err != nil {
		return err
	}

	if err := compiler.evalExprInto(th0["value"].(map[string]interface{}), tThen); err != nil {
		return err
	}

	if err := compiler.evalExprInto(el0["value"].(map[string]interface{}), tElse); err != nil {
		return err
	}

	return compiler.emitSelect(condMask, tThen, tElse, dst)
}

func singleAssignName(assign map[string]interface{}) (string, error) {

	target, ok := assign["target"].(map[string]interface{})

	if !ok || target["kind"] != "Name" {
		return "", fmt.Errorf("pysix: assign target must be Name")
	}

	name, _ := target["id"].(string)

	if name == "" {
		return "", fmt.Errorf("pysix: empty name")
	}

	return name, nil
}

func (compiler *Compiler) compileForRange(stmt map[string]interface{}) error {

	vname, _ := stmt["var"].(string)

	if vname == "" {
		return fmt.Errorf("pysix: ForRange var")
	}

	stopExpr, ok := stmt["stop"].(map[string]interface{})

	if !ok {
		return fmt.Errorf("pysix: ForRange stop")
	}

	n, err := constUint(stopExpr)

	if err != nil {
		return fmt.Errorf("pysix: range(stop) must be constant: %w", err)
	}

	body, ok := stmt["body"].([]interface{})

	if !ok {
		return fmt.Errorf("pysix: ForRange body")
	}

	if n > 4096 {
		return fmt.Errorf("pysix: range too large (%d)", n)
	}

	vslot, err := compiler.localOf(vname)

	if err != nil {
		return err
	}

	var k uint64

	for k = 0; k < n; k++ {
		compiler.resetTemps()

		if err := compiler.emitLoadUInt64(k, vslot); err != nil {
			return err
		}

		for _, raw := range body {
			st, ok := raw.(map[string]interface{})

			if !ok {
				return fmt.Errorf("pysix: bad for-body stmt")
			}

			if err := compiler.compileStmt(st); err != nil {
				return err
			}
		}
	}

	return nil
}

func constUint(expr map[string]interface{}) (uint64, error) {

	if k, ok := expr["kind"].(string); !ok || k != "Num" {
		return 0, fmt.Errorf("not a numeric constant")
	}

	return asUint64(expr["n"])
}

func (compiler *Compiler) evalCompareAsMask(test map[string]interface{}) (uint8, error) {

	kind, _ := test["kind"].(string)

	if kind != "Compare" {
		return 0, fmt.Errorf("pysix: if test must be a Compare for v1")
	}

	op, _ := test["op"].(string)

	left, ok := test["left"].(map[string]interface{})

	if !ok {
		return 0, fmt.Errorf("compare left")
	}

	right, ok := test["right"].(map[string]interface{})

	if !ok {
		return 0, fmt.Errorf("compare right")
	}

	a, err := compiler.evalExpr(left)

	if err != nil {
		return 0, err
	}

	b, err := compiler.evalExpr(right)

	if err != nil {
		return 0, err
	}

	diff, err := compiler.allocTemp()

	if err != nil {
		return 0, err
	}

	compiler.prog = append(compiler.prog, stepwise.EncodeStep(0x6, a, b, diff))

	switch op {

	case "Eq":
		return compiler.popEqZeroMask(diff)

	case "NotEq":
		m, err := compiler.popEqZeroMask(diff)

		if err != nil {
			return 0, err
		}

		one, err := compiler.allocTemp()

		if err != nil {
			return 0, err
		}

		compiler.prog = append(compiler.prog, stepwise.EncodeImm(one, 1))
		compiler.prog = append(compiler.prog, stepwise.EncodeStep(0x6, m, one, m))

		return m, nil

	default:
		return 0, fmt.Errorf("pysix: compare op %q (only Eq/NotEq)", op)
	}
}

/*
popEqZeroMask sets dst to 1 iff diff slot is bitwise zero, else 0.
*/
func (compiler *Compiler) popEqZeroMask(diff uint8) (uint8, error) {

	pop, err := compiler.allocTemp()

	if err != nil {
		return 0, err
	}

	compiler.prog = append(compiler.prog, stepwise.EncodeStep(0x10, diff, slotZero, pop))

	nz, err := compiler.emitNonZeroBit(pop)

	if err != nil {
		return 0, err
	}

	one, err := compiler.allocTemp()

	if err != nil {
		return 0, err
	}

	compiler.prog = append(compiler.prog, stepwise.EncodeImm(one, 1))

	if err := compiler.emitSubInto(one, nz, nz); err != nil {
		return 0, err
	}

	return nz, nil
}

/*
emitNonZeroBit reduces x to (x!=0) as 0/1 in the same or new slot — uses OR-shift ladder on copy.
*/
func (compiler *Compiler) emitNonZeroBit(x uint8) (uint8, error) {

	cp, err := compiler.allocTemp()

	if err != nil {
		return 0, err
	}

	compiler.prog = append(compiler.prog, stepwise.EncodeStep(0x3, x, x, cp))

	cur := cp

	for _, sh := range []uint16{32, 16, 8, 4, 2, 1} {
		tSh, err := compiler.allocTemp()

		if err != nil {
			return 0, err
		}

		compiler.prog = append(compiler.prog, stepwise.EncodeImm(tSh, sh))

		tShr, err := compiler.allocTemp()

		if err != nil {
			return 0, err
		}

		compiler.prog = append(compiler.prog, stepwise.EncodeStep(0x12, tSh, cur, tShr))
		compiler.prog = append(compiler.prog, stepwise.EncodeStep(0x7, cur, tShr, cur))
	}

	one, err := compiler.allocTemp()

	if err != nil {
		return 0, err
	}

	compiler.prog = append(compiler.prog, stepwise.EncodeImm(one, 1))
	compiler.prog = append(compiler.prog, stepwise.EncodeStep(0x1, cur, one, cur))

	return cur, nil
}

/*
emitSelect writes (if cond 0/1 then a else b) into dst using masks — cond must be 0 or all-ones style.
Actually cond is 0 or 1 only.
*/
func (compiler *Compiler) emitSelect(condSlot, aSlot, bSlot, dst uint8) error {

	negMask, err := compiler.allocTemp()

	if err != nil {
		return err
	}

	if err := compiler.emitSubInto(slotZero, condSlot, negMask); err != nil {
		return err
	}

	tT, err := compiler.allocTemp()

	if err != nil {
		return err
	}

	compiler.prog = append(compiler.prog, stepwise.EncodeStep(0x1, aSlot, negMask, tT))

	notMask, err := compiler.allocTemp()

	if err != nil {
		return err
	}

	compiler.prog = append(compiler.prog, stepwise.EncodeStep(0x6, negMask, slotOnes, notMask))

	tF, err := compiler.allocTemp()

	if err != nil {
		return err
	}

	compiler.prog = append(compiler.prog, stepwise.EncodeStep(0x1, bSlot, notMask, tF))
	compiler.prog = append(compiler.prog, stepwise.EncodeStep(0x7, tT, tF, dst))

	return nil
}

func (compiler *Compiler) evalExpr(expr map[string]interface{}) (uint8, error) {

	slot, err := compiler.allocTemp()

	if err != nil {
		return 0, err
	}

	if err := compiler.evalExprInto(expr, slot); err != nil {
		return 0, err
	}

	return slot, nil
}

func (compiler *Compiler) evalExprInto(expr map[string]interface{}, dst uint8) error {

	kind, ok := expr["kind"].(string)

	if !ok {
		return fmt.Errorf("pysix: expr kind")
	}

	switch kind {

	case "Num":
		n, err := asUint64(expr["n"])

		if err != nil {
			return err
		}

		return compiler.emitLoadUInt64(n, dst)

	case "Name":
		id, _ := expr["id"].(string)

		if id == "" {
			return fmt.Errorf("pysix: empty Name")
		}

		src, err := compiler.localOf(id)

		if err != nil {
			return err
		}

		compiler.prog = append(compiler.prog, stepwise.EncodeStep(0x3, src, src, dst))

		return nil

	case "UnaryOp":
		return compiler.evalUnary(expr, dst)

	case "BinOp":
		return compiler.evalBinOp(expr, dst)

	default:
		return fmt.Errorf("pysix: expr %q", kind)
	}
}

func (compiler *Compiler) evalUnary(expr map[string]interface{}, dst uint8) error {

	op, _ := expr["op"].(string)

	sub, ok := expr["operand"].(map[string]interface{})

	if !ok {
		return fmt.Errorf("unary operand")
	}

	inner, err := compiler.evalExpr(sub)

	if err != nil {
		return err
	}

	switch op {

	case "USub":
		return compiler.emitSubInto(slotZero, inner, dst)

	default:
		return fmt.Errorf("pysix: unary %q", op)
	}
}

func (compiler *Compiler) evalBinOp(expr map[string]interface{}, dst uint8) error {

	op, _ := expr["op"].(string)

	left, ok := expr["left"].(map[string]interface{})

	if !ok {
		return fmt.Errorf("pysix: binop left")
	}

	right, ok := expr["right"].(map[string]interface{})

	if !ok {
		return fmt.Errorf("pysix: binop right")
	}

	ls, err := compiler.evalExpr(left)

	if err != nil {
		return err
	}

	rs, err := compiler.evalExpr(right)

	if err != nil {
		return err
	}

	switch op {

	case "Add":
		compiler.prog = append(compiler.prog, stepwise.EncodeStep(0x13, ls, rs, dst))

	case "Sub":
		if err := compiler.emitSubInto(ls, rs, dst); err != nil {
			return err
		}

	case "Mult":
		k, err := constUint(right)

		if err != nil {
			return fmt.Errorf("pysix: multiplication needs literal right-hand side (<=16)")
		}

		if k > 16 {
			return fmt.Errorf("pysix: multiplier too large (max 16)")
		}

		if k == 0 {
			return compiler.emitLoadUInt64(0, dst)
		}

		compiler.prog = append(compiler.prog, stepwise.EncodeStep(0x3, ls, ls, dst))

		for i := uint64(1); i < k; i++ {
			compiler.prog = append(compiler.prog, stepwise.EncodeStep(0x13, dst, ls, dst))
		}

	default:
		return fmt.Errorf("pysix: binop %q", op)
	}

	return nil
}

func (compiler *Compiler) emitSubInto(a, b, dst uint8) error {

	tNot, err := compiler.allocTemp()

	if err != nil {
		return err
	}

	compiler.prog = append(compiler.prog, stepwise.EncodeStep(0x0A, slotZero, b, tNot))

	tOne, err := compiler.allocTemp()

	if err != nil {
		return err
	}

	compiler.prog = append(compiler.prog, stepwise.EncodeImm(tOne, 1))

	tNegB, err := compiler.allocTemp()

	if err != nil {
		return err
	}

	compiler.prog = append(compiler.prog, stepwise.EncodeStep(0x13, tNot, tOne, tNegB))
	compiler.prog = append(compiler.prog, stepwise.EncodeStep(0x13, a, tNegB, dst))

	return nil
}

func (compiler *Compiler) emitLoadUInt64(val uint64, target uint8) error {

	if val <= 0xffff {
		compiler.prog = append(compiler.prog, stepwise.EncodeImm(target, uint16(val)))

		return nil
	}

	cur := target

	compiler.prog = append(compiler.prog, stepwise.EncodeImm(cur, uint16(val&0xffff)))

	val >>= 16
	shift := 16

	for val != 0 {
		limb, err := compiler.allocTemp()

		if err != nil {
			return err
		}

		compiler.prog = append(compiler.prog, stepwise.EncodeImm(limb, uint16(val&0xffff)))

		tSh, err := compiler.allocTemp()

		if err != nil {
			return err
		}

		compiler.prog = append(compiler.prog, stepwise.EncodeImm(tSh, uint16(shift)))

		tShVal, err := compiler.allocTemp()

		if err != nil {
			return err
		}

		compiler.prog = append(compiler.prog, stepwise.EncodeStep(0x11, tSh, limb, tShVal))

		tNext, err := compiler.allocTemp()

		if err != nil {
			return err
		}

		compiler.prog = append(compiler.prog, stepwise.EncodeStep(0x13, cur, tShVal, tNext))
		cur = tNext
		val >>= 16
		shift += 16
	}

	if cur != target {
		compiler.prog = append(compiler.prog, stepwise.EncodeStep(0x3, cur, cur, target))
	}

	return nil
}

func asUint64(v interface{}) (uint64, error) {

	switch x := v.(type) {

	case json.Number:
		i, err := x.Int64()

		if err != nil {
			return 0, err
		}

		if i < 0 {
			return 0, fmt.Errorf("negative literal")
		}

		return uint64(i), nil

	case float64:
		if x < 0 || x > float64(^uint64(0)) {
			return 0, fmt.Errorf("literal out of range")
		}

		return uint64(x), nil

	default:
		return 0, fmt.Errorf("pysix: not a number")
	}
}
