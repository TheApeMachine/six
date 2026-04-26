package program

import (
	"fmt"

	"github.com/theapemachine/six/pkg/compute/program/ts"
)

type feedSite struct {
	emit       bool
	compact    []ts.Term
	operations []ts.Operation
}

type feedAtom struct {
	owner     string
	ref       string
	imm       bool
	bare      bool
	synthetic bool
}

type feedExpr struct {
	left     feedAtom
	right    feedAtom
	dst      feedAtom
	op       string
	mode     uint64
	topology uint64
	implicit bool
}

func compileFeedSite(site feedSite, inheritedPredStart, inheritedPredCond uint64, incoming []feedAtom, lay Layout) ([]uint64, []feedAtom, bool, error) {
	if len(site.operations) == 0 {
		return compileFeedSingle(site.emit, site.compact, inheritedPredStart, inheritedPredCond, incoming, lay)
	}

	return compileFeedOperations(site.emit, site.operations, inheritedPredStart, inheritedPredCond, incoming, lay)
}

func compileFeedSingle(emit bool, terms []ts.Term, inheritedPredStart, inheritedPredCond uint64, incoming []feedAtom, lay Layout) ([]uint64, []feedAtom, bool, error) {
	if len(terms) == 0 {
		return nil, nil, false, nil
	}

	if emit {
		instr, ok, err := compileEmitSite(inheritedPredStart, inheritedPredCond, lay)
		if !ok || err != nil {
			return nil, nil, ok, err
		}

		return []uint64{instr}, nil, true, nil
	}

	instr, ok, err := compileOperationSite(terms, inheritedPredStart, inheritedPredCond, incomingFeedAtom(incoming, 0), lay)
	if !ok || err != nil {
		return nil, nil, ok, err
	}

	return []uint64{instr}, nil, true, nil
}

func feedOpTerms(operation ts.Operation) (terms []ts.Term, skip bool, err error) {
	if operationIsPredicate(operation) {
		return nil, true, nil
	}

	terms = operation.Terms
	if len(terms) == 0 {
		return nil, false, fmt.Errorf("empty operation")
	}

	return terms, false, nil
}

func compileFeedOperations(emit bool, operations []ts.Operation, inheritedPredStart, inheritedPredCond uint64, incoming []feedAtom, lay Layout) ([]uint64, []feedAtom, bool, error) {
	var predStart, predCond uint64
	if inheritedPredCond != 0 {
		predStart = inheritedPredStart
		predCond = inheritedPredCond
	}

	operationsWork := operations
	if len(operationsWork) > 1 && operationIsPredicate(operationsWork[1]) {
		pred, err := parseFeedPredicateTerms(operationsWork[1].Terms)
		if err != nil {
			return nil, nil, false, err
		}

		predStart, predCond, err = compilePredicate(pred, lay)
		if err != nil {
			return nil, nil, false, err
		}

		operationsWork = operationsWork[:1]
	}

	words := make([]uint64, 0, len(operationsWork))
	result := make([]feedAtom, 0, len(operationsWork))

	for opIdx, operation := range operationsWork {
		terms, skip, err := feedOpTerms(operation)
		if err != nil {
			return nil, nil, false, err
		}
		if skip {
			continue
		}

		var instr uint64
		var ok bool
		var opErr error

		if emit {
			instr, ok, opErr = compileEmitOperationSite(terms, predStart, predCond, incomingFeedAtom(incoming, opIdx), lay)
		} else {
			instr, ok, opErr = compileOperationSite(terms, predStart, predCond, incomingFeedAtom(incoming, opIdx), lay)
		}

		if opErr != nil {
			return nil, nil, false, opErr
		}

		if ok {
			words = append(words, instr)
			if expr, exprErr := parseFeedExpr(terms); exprErr == nil {
				result = append(result, feedAtom{ref: feedDestination(expr), synthetic: true})
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

func feedSiteGate(site feedSite, lay Layout) (uint64, uint64, bool, error) {
	if len(site.operations) != 1 || !operationIsPredicate(site.operations[0]) {
		return 0, 0, false, nil
	}

	pred, err := parseFeedPredicateTerms(site.operations[0].Terms)
	if err != nil {
		return 0, 0, false, err
	}

	predStart, predCond, err := compilePredicate(pred, lay)
	if err != nil {
		return 0, 0, false, err
	}

	return predStart, predCond, true, nil
}

func operationIsPredicate(operation ts.Operation) bool {
	return len(operation.Terms) > 0 && isQuestionTerm(operation.Terms[len(operation.Terms)-1])
}

func parseFeedPredicateTerms(terms []ts.Term) (*PredicateNode, error) {
	if len(terms) == 2 && isQuestionTerm(terms[1]) {
		if terms[0].Kind != ts.TermOperation {
			return nil, fmt.Errorf("invalid gate %v", feedTermTokens(terms))
		}

		return parseFeedPredicateTerms(terms[0].Terms)
	}

	if len(terms) == 3 && isQuestionTerm(terms[2]) {
		if terms[0].Kind == ts.TermOperation && len(terms[0].Terms) == 2 && isFeedReducerTerm(terms[0].Terms[1]) {
			left, err := parseFeedAtom(terms[0].Terms[0])
			if err != nil {
				return nil, err
			}
			if err := requireExplicitFeedOwner(left); err != nil {
				return nil, err
			}

			return &PredicateNode{
				IsPopcnt:  true,
				Region:    left.ref,
				Op:        "<",
				Threshold: feedTermText(terms[1]),
			}, nil
		}
	}

	if len(terms) == 3 {
		left, err := parseFeedAtom(terms[0])
		if err != nil {
			return nil, err
		}
		if err := requireExplicitFeedOwner(left); err != nil {
			return nil, err
		}

		return &PredicateNode{
			Region: left.ref,
			Op:     feedTermText(terms[2]),
			Value:  feedTermText(terms[1]),
		}, nil
	}

	if len(terms) == 4 && isFeedReducerTerm(terms[1]) && isQuestionTerm(terms[3]) {
		left, err := parseFeedAtom(terms[0])
		if err != nil {
			return nil, err
		}
		if err := requireExplicitFeedOwner(left); err != nil {
			return nil, err
		}

		return &PredicateNode{
			IsPopcnt:  true,
			Region:    left.ref,
			Op:        "<",
			Threshold: feedTermText(terms[2]),
		}, nil
	}

	return nil, fmt.Errorf("invalid gate %v", feedTermTokens(terms))
}

func compileEmitSite(predStart, predCond uint64, lay Layout) (uint64, bool, error) {
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

func compileEmitOperationSite(terms []ts.Term, predStart, predCond uint64, incoming feedAtom, lay Layout) (uint64, bool, error) {
	expr, err := parseFeedExpr(terms)
	if err != nil {
		return 0, false, err
	}

	expr = bindImplicitFeed(expr, incoming)
	expr.mode = ModeEmit

	return compileFeedExprWithTopology(expr, predStart, predCond, TopologySpawn, lay)
}

func compileOperationSite(terms []ts.Term, predStart, predCond uint64, incoming feedAtom, lay Layout) (uint64, bool, error) {
	expr, err := parseFeedExpr(terms)
	if err != nil {
		return 0, false, err
	}

	expr = bindImplicitFeed(expr, incoming)

	return compileFeedExpr(expr, predStart, predCond, lay)
}
