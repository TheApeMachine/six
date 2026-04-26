package program

import (
	"fmt"
	"math/bits"
	"strconv"
	"sync"
)

const predExtended = 3

type predicateKind uint8

const (
	predicatePopcntLTE predicateKind = 1
	predicatePopcntLT  predicateKind = 2
)

type predicateSpec struct {
	kind      predicateKind
	start     int
	span      int
	threshold uint64
}

type PredicateNode struct {
	Region    string
	Op        string
	Value     string
	IsPopcnt  bool
	Threshold string
}

/*
PredicateDeviceSpec is the compact predicate table entry copied into native
GPU kernels so extended predicates keep the same meaning as the CPU map.
*/
type PredicateDeviceSpec struct {
	Kind      uint64
	Start     uint64
	Span      uint64
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

func compilePredicate(node *PredicateNode, lay Layout) (uint64, uint64, error) {
	if node.IsPopcnt {
		kind := predicatePopcntLTE
		switch node.Op {
		case "|":
			kind = predicatePopcntLTE
		case "<":
			kind = predicatePopcntLT
		default:
			return 0, 0, fmt.Errorf("popcnt predicate must use '| Threshold' or feed threshold gate")
		}

		threshold, err := strconv.ParseUint(node.Threshold, 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("popcnt predicate threshold: %w", err)
		}

		start, span, _, err := parseRef(node.Region, lay)
		if err != nil {
			return 0, 0, fmt.Errorf("popcnt predicate region: %w", err)
		}

		id, err := registerPredicate(predicateSpec{
			kind:      kind,
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
			count := predicatePopcnt(frame, spec.start, spec.span)

			return uint64(count) <= spec.threshold
		case predicatePopcntLT:
			count := predicatePopcnt(frame, spec.start, spec.span)

			return uint64(count) < spec.threshold
		default:
			return false
		}
	default:
		return false
	}
}

func predicatePopcnt(frame *[128]uint64, start, span int) int {
	count := 0

	for idx := start; idx < start+span && idx < len(frame); idx++ {
		count += bits.OnesCount64(frame[idx])
	}

	return count
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
			Kind:      uint64(spec.kind),
			Start:     uint64(spec.start),
			Span:      uint64(spec.span),
			Threshold: spec.threshold,
		}
	}

	return out
}
