package cortex

import (
	"math"
	"sort"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/resonance"
)

/*
LogicRule is an exported view of one reusable cortex behavior.

Interface is the condition or input signature.
Payload is the response or output tendency.
Program is the control-plane carrier that shaped the behavior.
Support is the accumulated evidence that this rule is worth reusing.
*/
type LogicRule struct {
	Interface data.Chord
	Payload   data.Chord
	Program   data.Chord
	Support   int
	Role      NodeRole
}

/*
LogicChain is a reusable two-step logical motif exported from cortex.
It captures that one reusable behavior reliably feeds another through a
composed edge, which is valuable for interior span constraints.
*/
type LogicChain struct {
	Left    LogicRule
	Right   LogicRule
	Bridge  data.Chord
	Program data.Chord
	Support int
}

/*
LogicSnapshot is the portable logic surface exported from cortex to downstream
field solvers. It contains reusable rules, multi-step chains, compiled tool
circuits, and high-support signal residues from the current prompt cycle.
*/
type LogicSnapshot struct {
	Rules    []LogicRule
	Chains   []LogicChain
	Circuits []LogicCircuit
	Signals  []data.Chord
}

/*
Empty reports whether the snapshot carries any usable logic.
*/
func (snapshot LogicSnapshot) Empty() bool {
	return len(snapshot.Rules) == 0 && len(snapshot.Chains) == 0 && len(snapshot.Circuits) == 0 && len(snapshot.Signals) == 0
}

/*
SnapshotLogic exports the graph's current reusable logic as a compact snapshot.
Tool nodes provide durable rules; strong register nodes and sink signals provide
prompt-local pressure that can still guide non-autoregressive span solving.
Edges between reusable nodes are lifted into LogicChain constraints when they
show stable compositional support.
*/
func (graph *Graph) SnapshotLogic() LogicSnapshot {
	if graph == nil {
		return LogicSnapshot{}
	}

	ruleIndex := graph.collectRuleIndex(true)
	snapshot := LogicSnapshot{
		Rules:   make([]LogicRule, 0, len(ruleIndex)),
		Chains:  make([]LogicChain, 0, len(ruleIndex)),
		Signals: graph.extractResults(),
	}

	for _, rule := range ruleIndex {
		snapshot.Rules = append(snapshot.Rules, rule)
	}

	for _, edge := range graph.Edges() {
		left, leftOK := ruleIndex[edge.A]
		right, rightOK := ruleIndex[edge.B]
		if !leftOK || !rightOK {
			continue
		}

		chain, ok := exportLogicChain(edge, left, right)
		if ok {
			snapshot.Chains = append(snapshot.Chains, chain)
		}
	}

	snapshot.Circuits = mergeLogicCircuits(
		graph.exportLogicCircuits(ruleIndex),
		graph.catalogCircuits(),
	)

	sort.Slice(snapshot.Rules, func(i, j int) bool {
		if snapshot.Rules[i].Support == snapshot.Rules[j].Support {
			leftMass := snapshot.Rules[i].Payload.ActiveCount() + snapshot.Rules[i].Program.ActiveCount()
			rightMass := snapshot.Rules[j].Payload.ActiveCount() + snapshot.Rules[j].Program.ActiveCount()
			return leftMass > rightMass
		}
		return snapshot.Rules[i].Support > snapshot.Rules[j].Support
	})

	sort.Slice(snapshot.Chains, func(i, j int) bool {
		if snapshot.Chains[i].Support == snapshot.Chains[j].Support {
			leftMass := snapshot.Chains[i].Bridge.ActiveCount() + snapshot.Chains[i].Program.ActiveCount()
			rightMass := snapshot.Chains[j].Bridge.ActiveCount() + snapshot.Chains[j].Program.ActiveCount()
			return leftMass > rightMass
		}
		return snapshot.Chains[i].Support > snapshot.Chains[j].Support
	})

	sort.Slice(snapshot.Circuits, func(i, j int) bool {
		leftWeight := snapshot.Circuits[i].Weight()
		rightWeight := snapshot.Circuits[j].Weight()
		if leftWeight == rightWeight {
			return snapshot.Circuits[i].Len() > snapshot.Circuits[j].Len()
		}
		return leftWeight > rightWeight
	})

	if len(snapshot.Rules) > 24 {
		snapshot.Rules = snapshot.Rules[:24]
	}

	if len(snapshot.Chains) > 16 {
		snapshot.Chains = snapshot.Chains[:16]
	}

	if len(snapshot.Circuits) > 12 {
		snapshot.Circuits = snapshot.Circuits[:12]
	}

	if len(snapshot.Signals) > 16 {
		snapshot.Signals = snapshot.Signals[:16]
	}

	return snapshot
}

func exportLogicChain(edge *Edge, left, right LogicRule) (LogicChain, bool) {
	forward := resonance.OverlapScore(&left.Payload, &right.Interface)
	reverse := resonance.OverlapScore(&right.Payload, &left.Interface)

	if forward == 0 && reverse == 0 && edge.ComposeHits == 0 {
		return LogicChain{}, false
	}

	support := edge.ComposeHits
	if support == 0 {
		support = min(left.Support, right.Support)
	}
	if support <= 0 {
		return LogicChain{}, false
	}

	if reverse > forward {
		left, right = right, left
	}

	bridge := data.ChordAND(&left.Payload, &right.Interface)
	if bridge.ActiveCount() == 0 {
		bridge = data.ChordOR(&left.Payload, &right.Interface)
	}

	program := data.ChordOR(&left.Program, &right.Program)
	if program.ActiveCount() == 0 && edge.Program.ActiveCount() > 0 {
		program = edge.Program
	}

	return LogicChain{
		Left:    left,
		Right:   right,
		Bridge:  bridge,
		Program: program,
		Support: support,
	}, true
}

/*
Weight returns a smooth score for the rule's evidence mass.
*/
func (rule LogicRule) Weight() float64 {
	if rule.Support <= 0 {
		return 0
	}
	return math.Log1p(float64(rule.Support))
}

/*
Weight returns a smooth score for the chain's evidence mass.
*/
func (chain LogicChain) Weight() float64 {
	if chain.Support <= 0 {
		return 0
	}
	return math.Log1p(float64(chain.Support))
}
