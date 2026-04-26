package program

import (
	"fmt"
	"strings"

	"github.com/theapemachine/six/pkg/compute/program/ts"
)

func parseFeedExpr(terms []ts.Term) (feedExpr, error) {
	topology := uint64(TopologySelf)
	if len(terms) > 0 {
		if candidate, ok := Topologies[strings.ToLower(feedTermText(terms[len(terms)-1]))]; ok {
			topology = candidate
			terms = terms[:len(terms)-1]
		}
	}

	if expr, ok, err := parseFeedDestinationExpr(terms, topology); ok || err != nil {
		return expr, err
	}

	stack := make([]feedExpr, 0, len(terms))

	for _, term := range terms {
		if isFeedReducerTerm(term) {
			if len(stack) < 1 {
				return feedExpr{}, fmt.Errorf("reducer %q needs one operand", feedTermText(term))
			}

			src := stack[len(stack)-1]
			stack[len(stack)-1] = feedExpr{
				left:     src.left,
				op:       "A",
				mode:     feedReducerMode(feedTermText(term)),
				topology: topology,
			}
			continue
		}

		if feedTermIsOperator(term, len(stack)) {
			if len(stack) < 2 {
				return feedExpr{}, fmt.Errorf("operator %q needs two operands", feedTermText(term))
			}

			right := stack[len(stack)-1]
			left := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			stack = append(stack, feedExpr{
				left:     left.left,
				right:    right.left,
				op:       feedTermText(term),
				mode:     ModeTruth,
				topology: topology,
			})
			continue
		}

		atom, err := parseFeedAtom(term)
		if err != nil {
			return feedExpr{}, err
		}

		stack = append(stack, feedExpr{left: atom, op: "A", mode: ModeTruth})
	}

	if len(stack) == 1 {
		if len(terms) == 1 {
			stack[0].implicit = true
		}

		stack[0].topology = topology

		return stack[0], nil
	}

	if len(stack) == 2 {
		return feedExpr{left: stack[0].left, right: stack[1].left, op: "B", mode: stack[1].mode, topology: topology}, nil
	}

	return feedExpr{}, fmt.Errorf("operation leaves %d stack values: %v", len(stack), feedTermTokens(terms))
}

func parseFeedDestinationExpr(terms []ts.Term, topology uint64) (feedExpr, bool, error) {
	if len(terms) == 0 {
		return feedExpr{}, false, nil
	}

	dst, err := parseFeedAtom(terms[0])
	if err != nil {
		return feedExpr{}, false, err
	}
	if !isExplicitFeedDestination(dst) {
		return feedExpr{}, false, nil
	}

	if len(terms) == 1 {
		return feedExpr{
			left:     dst,
			op:       "A",
			mode:     ModeTruth,
			topology: topology,
			implicit: true,
		}, true, nil
	}

	if len(terms) == 2 {
		right, err := parseFeedAtom(terms[1])
		if err != nil {
			return feedExpr{}, true, err
		}

		return feedExpr{
			left:     dst,
			right:    right,
			dst:      dst,
			op:       "B",
			mode:     ModeTruth,
			topology: topology,
		}, true, nil
	}

	op := feedTermText(terms[len(terms)-1])
	if isFeedReducerTerm(terms[len(terms)-1]) {
		switch {
		case len(terms) == 3:
			left, err := parseFeedAtom(terms[1])
			if err != nil {
				return feedExpr{}, true, err
			}

			return feedExpr{
				left:     left,
				dst:      dst,
				op:       "A",
				mode:     feedReducerMode(op),
				topology: topology,
			}, true, nil

		case len(terms) == 5 && feedTermIsOperator(terms[3], 2):
			left, err := parseFeedAtom(terms[1])
			if err != nil {
				return feedExpr{}, true, err
			}

			right, err := parseFeedAtom(terms[2])
			if err != nil {
				return feedExpr{}, true, err
			}

			return feedExpr{
				left:     left,
				right:    right,
				dst:      dst,
				op:       feedTermText(terms[3]),
				mode:     feedReducerMode(op),
				topology: topology,
			}, true, nil

		default:
			return feedExpr{}, true, fmt.Errorf("reducer %q expects destination with one source or one binary source expression", op)
		}
	}

	if !feedTermIsOperator(terms[len(terms)-1], 2) {
		return feedExpr{}, false, nil
	}

	if len(terms) == 3 {
		right, err := parseFeedAtom(terms[1])
		if err != nil {
			return feedExpr{}, true, err
		}

		return feedExpr{
			left:     dst,
			right:    right,
			dst:      dst,
			op:       op,
			mode:     ModeTruth,
			topology: topology,
		}, true, nil
	}

	if len(terms) == 4 {
		left, err := parseFeedAtom(terms[1])
		if err != nil {
			return feedExpr{}, true, err
		}

		right, err := parseFeedAtom(terms[2])
		if err != nil {
			return feedExpr{}, true, err
		}

		return feedExpr{
			left:     left,
			right:    right,
			dst:      dst,
			op:       op,
			mode:     ModeTruth,
			topology: topology,
		}, true, nil
	}

	return feedExpr{}, true, fmt.Errorf("destination operation has too many expression tokens: %v", feedTermTokens(terms))
}

func isExplicitFeedDestination(atom feedAtom) bool {
	return atom.owner != "" && !atom.imm && !atom.bare && atom.ref != ""
}

func bindImplicitFeed(expr feedExpr, incoming feedAtom) feedExpr {
	if !expr.implicit || incoming.ref == "" {
		return expr
	}

	expr.right = incoming
	expr.op = "B"

	return expr
}

func isQuestionTerm(term ts.Term) bool {
	return term.Kind == ts.TermQuestion || term.Text == "?"
}

func isFeedReducerTerm(term ts.Term) bool {
	return term.Kind == ts.TermReducer || isFeedReducer(feedTermText(term))
}

func feedTermIsOperator(term ts.Term, depth int) bool {
	text := feedTermText(term)
	if term.Kind == ts.TermNumber || strings.EqualFold(text, "done") || strings.EqualFold(text, "clear") {
		return false
	}

	if (text == "A" || text == "B") && depth < 2 {
		return false
	}

	_, ok := Opcodes[text]

	return ok
}

func feedTermText(term ts.Term) string {
	if term.Kind == ts.TermCall && term.Text == "" {
		return term.Owner + "(" + term.Ref + ")"
	}
	if term.Kind == ts.TermOperation {
		return "{" + strings.Join(feedTermTokens(term.Terms), " ") + "}"
	}

	return term.Text
}

func feedTermTokens(terms []ts.Term) []string {
	tokens := make([]string, len(terms))

	for idx := range terms {
		tokens[idx] = feedTermText(terms[idx])
	}

	return tokens
}

func compileFeedExpr(expr feedExpr, predStart, predCond uint64, lay Layout) (uint64, bool, error) {
	return compileFeedExprWithTopology(expr, predStart, predCond, expr.topology, lay)
}

func compileFeedExprWithTopology(expr feedExpr, predStart, predCond, topology uint64, lay Layout) (uint64, bool, error) {
	if err := requireExplicitFeedOwner(expr.dst); err != nil {
		return 0, false, fmt.Errorf("target: %w", err)
	}
	if err := requireExplicitFeedOwner(expr.left); err != nil {
		return 0, false, fmt.Errorf("left: %w", err)
	}
	if err := requireExplicitFeedOwner(expr.right); err != nil {
		return 0, false, fmt.Errorf("right: %w", err)
	}

	aStart, aSpan, aInd, err := parseFeedOperand(expr.left, lay)
	if err != nil {
		return 0, false, fmt.Errorf("left: %w", err)
	}

	bStart, bSpan, bType, err := compileFeedRight(expr.right, lay)
	if err != nil {
		return 0, false, fmt.Errorf("right: %w", err)
	}

	dstRef := feedDestination(expr)
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
	if topology == TopologyFold && !isFoldOpcode(opcode) {
		return 0, false, fmt.Errorf("fold topology requires associative/commutative operators, got opcode 0x%x", opcode)
	}

	leftOwner := feedOwner(expr.left, "A")
	rightOwner := feedOwner(expr.right, leftOwner)
	targetOwner := leftOwner
	if expr.dst.ref != "" {
		targetOwner = feedOwner(expr.dst, leftOwner)
	}

	// Bare A/B notation means "map resident A over every B" rather than
	// "write every mapped result back into A".
	implicitBMap := expr.dst.ref == "" && expr.left.bare && leftOwner == "A" && rightOwner == "B"

	flags := uint64(0)
	if leftOwner == "B" {
		flags |= InstrFlagAFromB
	}
	if targetOwner == "B" {
		flags |= InstrFlagTargetB
	}
	if implicitBMap {
		flags |= InstrFlagTargetB
	}
	if targetOwner == "A" && !implicitBMap {
		flags |= InstrFlagTargetOwner
	}
	if rightOwner == "A" && bType != InstrBTypeImmediate {
		flags |= InstrFlagBFromA
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

func feedDestination(expr feedExpr) string {
	if expr.dst.ref != "" {
		return expr.dst.ref
	}
	if expr.left.ref == "" {
		return "signals[0,8]"
	}

	return expr.left.ref
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

func requireExplicitFeedOwner(atom feedAtom) error {
	if atom.ref == "" || atom.imm || atom.bare || atom.synthetic {
		return nil
	}
	if atom.owner != "" {
		return nil
	}

	return fmt.Errorf("ambiguous operand %q; use A(%s) or B(%s)", atom.ref, atom.ref, atom.ref)
}

func parseFeedAtom(term ts.Term) (feedAtom, error) {
	raw := strings.TrimSpace(feedTermText(term))
	if raw == "" {
		return feedAtom{}, fmt.Errorf("empty operand")
	}

	if term.Kind == ts.TermNumber || strings.EqualFold(raw, "done") || strings.EqualFold(raw, "clear") {
		return feedAtom{ref: strings.ToLower(raw), imm: true}, nil
	}

	switch term.Kind {
	case ts.TermCall:
		ref := term.Ref
		if term.Rotate != 0 {
			rotated, err := rotateFeedRef(ref, term.Rotate)
			if err != nil {
				return feedAtom{}, err
			}

			ref = rotated
		}

		return feedAtom{owner: strings.ToUpper(term.Owner), ref: ref}, nil

	case ts.TermOwner:
		if strings.EqualFold(raw, "A") || strings.EqualFold(raw, "B") {
			return feedAtom{owner: strings.ToUpper(raw), ref: "signals[0,8]", bare: true}, nil
		}

	case ts.TermRef:
		if strings.Contains(raw, "[") || isKnownFeedRegion(raw) {
			return feedAtom{ref: raw}, nil
		}

		if isBareFeedRef(raw) {
			return feedAtom{ref: raw}, nil
		}

	case ts.TermOperation:
		expr, err := parseFeedExpr(term.Terms)
		if err != nil {
			return feedAtom{}, err
		}

		if expr.op != "A" || expr.mode != ModeTruth || expr.right != (feedAtom{}) || expr.dst != (feedAtom{}) {
			return feedAtom{}, fmt.Errorf("nested operand %q must collapse to one atom", raw)
		}

		return expr.left, nil
	}

	return feedAtom{}, fmt.Errorf("operand %q must be A(region), B(region), region, clear, done, or a number", raw)
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
