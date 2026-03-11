package cortex

import (
	"sort"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/resonance"
)

const (
	minLogicCircuitSteps = 3
	maxLogicCircuitSteps = 5
	maxCompiledCircuits  = 16
)

type logicArc struct {
	To      *Node
	Program data.Chord
	Support int
}

func (graph *Graph) collectRuleIndex(includeRegisters bool) map[*Node]LogicRule {
	if graph == nil {
		return nil
	}

	ruleIndex := make(map[*Node]LogicRule, len(graph.nodes))

	appendNode := func(node *Node) {
		if node == nil {
			return
		}

		key := node.candidateToolKey()
		if key.Input.ActiveCount() == 0 && key.Output.ActiveCount() == 0 {
			return
		}

		ruleIndex[node] = LogicRule{
			Interface: key.Input,
			Payload:   key.Output,
			Program:   key.Program,
			Support:   max(node.Support, 1),
			Role:      node.Role,
		}
	}

	for _, node := range graph.ToolNodes() {
		appendNode(node)
	}

	if includeRegisters {
		for _, node := range graph.RegisterNodes() {
			if node.Support <= 0 {
				continue
			}
			appendNode(node)
		}
	}

	return ruleIndex
}

func (graph *Graph) buildLogicAdjacency(ruleIndex map[*Node]LogicRule) map[*Node][]logicArc {
	if len(ruleIndex) < 2 {
		return nil
	}

	adjacency := make(map[*Node][]logicArc, len(ruleIndex))

	for _, edge := range graph.Edges() {
		left, leftOK := ruleIndex[edge.A]
		right, rightOK := ruleIndex[edge.B]
		if !leftOK || !rightOK {
			continue
		}

		forward := resonance.OverlapScore(&left.Payload, &right.Interface)
		reverse := resonance.OverlapScore(&right.Payload, &left.Interface)
		if edge.ComposeHits == 0 && forward < 0.35 && reverse < 0.35 {
			continue
		}

		fromNode := edge.A
		toNode := edge.B
		fromRule := left
		toRule := right
		overlap := forward
		if reverse > forward {
			fromNode, toNode = toNode, fromNode
			fromRule, toRule = toRule, fromRule
			overlap = reverse
		}

		program := data.ChordOR(&fromRule.Program, &toRule.Program)
		if edge.Program.ActiveCount() > 0 {
			program = data.ChordOR(&program, &edge.Program)
		}

		support := edge.ComposeHits
		if support <= 0 {
			support = min(max(fromRule.Support, 1), max(toRule.Support, 1)) + int(overlap*4.0)
		}
		if support <= 0 {
			continue
		}

		adjacency[fromNode] = append(adjacency[fromNode], logicArc{
			To:      toNode,
			Program: program,
			Support: support,
		})
	}

	for node := range adjacency {
		sort.Slice(adjacency[node], func(i, j int) bool {
			if adjacency[node][i].Support == adjacency[node][j].Support {
				leftProgram := adjacency[node][i].Program.ActiveCount()
				rightProgram := adjacency[node][j].Program.ActiveCount()
				return leftProgram > rightProgram
			}
			return adjacency[node][i].Support > adjacency[node][j].Support
		})
	}

	return adjacency
}

func (graph *Graph) exportLogicCircuits(ruleIndex map[*Node]LogicRule) []LogicCircuit {
	if len(ruleIndex) < minLogicCircuitSteps {
		return nil
	}

	adjacency := graph.buildLogicAdjacency(ruleIndex)
	if len(adjacency) == 0 {
		return nil
	}

	circuits := make([]LogicCircuit, 0, maxCompiledCircuits)
	seen := make(map[string]struct{}, maxCompiledCircuits)

	containsNode := func(path []*Node, candidate *Node) bool {
		for _, node := range path {
			if node == candidate {
				return true
			}
		}
		return false
	}

	var walk func(pathNodes []*Node, pathRules []LogicRule, program data.Chord, support int)
	walk = func(pathNodes []*Node, pathRules []LogicRule, program data.Chord, support int) {
		if len(pathRules) >= minLogicCircuitSteps {
			circuit := LogicCircuit{
				Steps:   copyLogicRules(pathRules),
				Program: program,
				Support: support,
			}
			key := circuit.Signature()
			if key != "" {
				if _, ok := seen[key]; !ok {
					seen[key] = struct{}{}
					circuits = append(circuits, circuit)
				}
			}
		}

		if len(pathRules) >= maxLogicCircuitSteps {
			return
		}

		current := pathNodes[len(pathNodes)-1]
		for _, arc := range adjacency[current] {
			if containsNode(pathNodes, arc.To) {
				continue
			}

			nextRule, ok := ruleIndex[arc.To]
			if !ok {
				continue
			}
			if nextRule.Payload.ActiveCount() == 0 && nextRule.Interface.ActiveCount() == 0 {
				continue
			}

			nextProgram := program
			if nextProgram.ActiveCount() == 0 {
				nextProgram = nextRule.Program
			} else if arc.Program.ActiveCount() > 0 {
				nextProgram = data.ChordOR(&nextProgram, &arc.Program)
			}
			if nextRule.Program.ActiveCount() > 0 {
				nextProgram = data.ChordOR(&nextProgram, &nextRule.Program)
			}

			nextSupport := min(support, max(nextRule.Support, 1), max(arc.Support, 1))
			overlap := resonance.OverlapScore(&pathRules[len(pathRules)-1].Payload, &nextRule.Interface)
			nextSupport += int(overlap * 2.0)
			if nextSupport <= 0 {
				continue
			}

			walk(
				append(append([]*Node(nil), pathNodes...), arc.To),
				append(append([]LogicRule(nil), pathRules...), nextRule),
				nextProgram,
				nextSupport,
			)
		}
	}

	for node, rule := range ruleIndex {
		if _, ok := adjacency[node]; !ok {
			continue
		}
		if rule.Payload.ActiveCount() == 0 {
			continue
		}

		walk([]*Node{node}, []LogicRule{rule}, rule.Program, max(rule.Support, 1))
	}

	sort.Slice(circuits, func(i, j int) bool {
		leftWeight := circuits[i].Weight()
		rightWeight := circuits[j].Weight()
		if leftWeight == rightWeight {
			return circuits[i].Len() > circuits[j].Len()
		}
		return leftWeight > rightWeight
	})

	if len(circuits) > maxCompiledCircuits {
		circuits = circuits[:maxCompiledCircuits]
	}

	return circuits
}

func (graph *Graph) catalogCircuits() []LogicCircuit {
	if len(graph.circuitCatalog) == 0 {
		return nil
	}

	circuits := make([]LogicCircuit, 0, len(graph.circuitCatalog))
	for _, circuit := range graph.circuitCatalog {
		circuits = append(circuits, circuit)
	}

	sort.Slice(circuits, func(i, j int) bool {
		leftWeight := circuits[i].Weight()
		rightWeight := circuits[j].Weight()
		if leftWeight == rightWeight {
			return circuits[i].Len() > circuits[j].Len()
		}
		return leftWeight > rightWeight
	})

	if len(circuits) > maxCompiledCircuits {
		circuits = circuits[:maxCompiledCircuits]
	}

	return circuits
}

func mergeLogicCircuits(parts ...[]LogicCircuit) []LogicCircuit {
	merged := make([]LogicCircuit, 0, maxCompiledCircuits)
	seen := make(map[string]LogicCircuit, maxCompiledCircuits)

	for _, part := range parts {
		for _, circuit := range part {
			key := circuit.Signature()
			if key == "" {
				continue
			}

			existing, ok := seen[key]
			if ok {
				if circuit.Support > existing.Support {
					existing.Support = circuit.Support
					existing.Program = circuit.Program
					existing.Steps = copyLogicRules(circuit.Steps)
				} else if circuit.Program.ActiveCount() > 0 {
					existing.Program = data.ChordOR(&existing.Program, &circuit.Program)
				}
				seen[key] = existing
				continue
			}

			seen[key] = LogicCircuit{
				Steps:   copyLogicRules(circuit.Steps),
				Program: circuit.Program,
				Support: circuit.Support,
			}
		}
	}

	for _, circuit := range seen {
		merged = append(merged, circuit)
	}

	sort.Slice(merged, func(i, j int) bool {
		leftWeight := merged[i].Weight()
		rightWeight := merged[j].Weight()
		if leftWeight == rightWeight {
			return merged[i].Len() > merged[j].Len()
		}
		return leftWeight > rightWeight
	})

	if len(merged) > maxCompiledCircuits {
		merged = merged[:maxCompiledCircuits]
	}

	return merged
}

func (graph *Graph) compileCircuits() {
	if graph == nil {
		return
	}

	if graph.circuitCatalog == nil {
		graph.circuitCatalog = make(map[string]LogicCircuit)
	}

	circuits := graph.exportLogicCircuits(graph.collectRuleIndex(false))
	if len(circuits) == 0 {
		return
	}

	for _, circuit := range circuits {
		key := circuit.Signature()
		if key == "" {
			continue
		}

		existing, ok := graph.circuitCatalog[key]
		if ok {
			if circuit.Support > existing.Support {
				existing.Steps = copyLogicRules(circuit.Steps)
				existing.Program = circuit.Program
			}
			existing.Support = max(existing.Support, circuit.Support) + 1
			graph.circuitCatalog[key] = existing
			continue
		}

		graph.circuitCatalog[key] = LogicCircuit{
			Steps:   copyLogicRules(circuit.Steps),
			Program: circuit.Program,
			Support: circuit.Support,
		}
	}

	if len(graph.circuitCatalog) <= maxCompiledCircuits {
		return
	}

	ranked := graph.catalogCircuits()
	keep := make(map[string]LogicCircuit, len(ranked))
	for _, circuit := range ranked {
		key := circuit.Signature()
		if key == "" {
			continue
		}
		keep[key] = circuit
	}

	graph.circuitCatalog = keep
}
