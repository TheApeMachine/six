package cortex

import (
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/resonance"
)

const (
	toolPromotionThreshold   = 2
	toolCompositionThreshold = 3
)

/*
NodeRole classifies how a cortex node participates in the graph.

Core nodes are the standing compute fabric.
Registers are prompt-local forked workspaces.
Tools are reusable compiled logical behaviors that can survive prompt resets.
*/
type NodeRole byte

const (
	RoleCore NodeRole = iota
	RoleRegister
	RoleTool
)

func (role NodeRole) String() string {
	switch role {
	case RoleCore:
		return "core"
	case RoleRegister:
		return "register"
	case RoleTool:
		return "tool"
	default:
		return "core"
	}
}

type toolKey struct {
	Input   data.Chord
	Output  data.Chord
	Program data.Chord
}

type toolPairKey struct {
	LeftID  int
	RightID int
}

func newToolPairKey(left, right *Node) toolPairKey {
	if left.ID > right.ID {
		left, right = right, left
	}

	return toolPairKey{LeftID: left.ID, RightID: right.ID}
}

func chordFace(chord data.Chord) int {
	face := chord.IntrinsicFace()
	if face == 256 {
		face = data.ChordBin(&chord)
	}
	return face
}

func resonate(left, right data.Chord) bool {
	if right.ActiveCount() == 0 {
		return false
	}

	threshold := 0.5
	if right.ActiveCount() >= 12 {
		threshold = 0.4
	}

	return resonance.OverlapScore(&left, &right) >= threshold
}

func (node *Node) candidateToolKey() toolKey {
	input := node.Interface
	if input.ActiveCount() == 0 {
		input = node.SearchChord()
	}

	output := node.Payload
	if output.ActiveCount() == 0 {
		output = input
	}

	program := node.Program
	if program.ActiveCount() == 0 {
		program = output
	}

	return toolKey{Input: input, Output: output, Program: program}
}

func (node *Node) matchesInterface(chord data.Chord) bool {
	if node.Interface.ActiveCount() == 0 {
		return false
	}

	return resonate(chord, node.Interface)
}

func (node *Node) noteFork(chord, program data.Chord) {
	if chord.ActiveCount() > 0 {
		if node.Interface.ActiveCount() == 0 || chord.ActiveCount() >= node.Interface.ActiveCount() {
			node.Interface = chord
		}
		node.Payload = data.ChordOR(&node.Payload, &chord)
	}

	if program.ActiveCount() > 0 {
		node.Program = data.ChordOR(&node.Program, &program)
	}

	node.Support++
	node.forkPending = true
}

func (node *Node) seedSpecialization() {
	key := node.candidateToolKey()
	if key.Input.ActiveCount() == 0 && key.Output.ActiveCount() == 0 && key.Program.ActiveCount() == 0 {
		return
	}

	carrier := key.Program
	if carrier.ActiveCount() == 0 {
		carrier = key.Output
	}
	if carrier.ActiveCount() == 0 {
		carrier = key.Input
	}

	if key.Input.ActiveCount() > 0 {
		node.Cube.Inject(chordFace(key.Input), key.Input, carrier, node.Rot)
	}

	if key.Output.ActiveCount() > 0 {
		node.Cube.Inject(chordFace(key.Output), key.Output, carrier, node.Rot)
	}

	if key.Program.ActiveCount() > 0 {
		node.Cube.InjectControl(key.Program, carrier, node.Rot)
	}

	node.InvalidateChordCache()
}

func (node *Node) invokeTool(tok Token) bool {
	if node.Role != RoleTool {
		return false
	}

	score := resonance.AffineScore(&tok.Chord, &node.Interface, &tok.Program, &node.Program)
	if node.Interface.ActiveCount() > 0 && score < 0.5 {
		return false
	}

	key := node.candidateToolKey()
	output := key.Output
	if output.ActiveCount() == 0 {
		output = tok.Chord
	}

	if key.Program.ActiveCount() > 0 {
		node.Rot = node.Rot.Compose(geometry.RotationForChord(key.Program))
	}

	node.Support++

	logicalFace := chordFace(output)
	carrier := key.Program
	if carrier.ActiveCount() == 0 {
		carrier = tok.Program
	}

	node.Cube.Inject(logicalFace, output, carrier, node.Rot)
	node.InvalidateChordCache()

	responseMask := key.Input
	if responseMask.ActiveCount() == 0 {
		responseMask = output
	}

	response := NewSignalToken(output, responseMask, node.ID)
	response.TTL = max(tok.TTL, defaultTTL)
	response.Program = key.Program

	for _, edge := range node.edges {
		neighbor := edge.A
		if neighbor == node {
			neighbor = edge.B
		}
		if neighbor.ID == tok.Origin {
			continue
		}
		neighbor.Send(response)
	}

	return true
}

/*
ToolNodes returns the current reusable tool nodes.
*/
func (graph *Graph) ToolNodes() []*Node {
	var tools []*Node

	for _, node := range graph.nodes {
		if node.Role == RoleTool {
			tools = append(tools, node)
		}
	}

	return tools
}

/*
RegisterNodes returns the current forked temporary registers.
*/
func (graph *Graph) RegisterNodes() []*Node {
	var registers []*Node

	for _, node := range graph.nodes {
		if node.Role == RoleRegister {
			registers = append(registers, node)
		}
	}

	return registers
}

func (graph *Graph) spawnSpecializedNode(parent *Node, role NodeRole, input, payload, program data.Chord) *Node {
	child := NewNode(graph.nextID, graph.tick)
	child.Rot = parent.Rot
	child.Role = role
	child.Interface = input
	child.Payload = payload
	child.Program = program
	child.Support = 1
	graph.nextID++
	graph.nodes = append(graph.nodes, child)
	graph.mitosisEvents++

	parent.Connect(child)
	child.Connect(parent)

	searchChord := input
	if searchChord.ActiveCount() == 0 {
		searchChord = payload
	}
	if searchChord.ActiveCount() == 0 {
		searchChord = parent.CubeChord()
	}

	child.seedSpecialization()

	var bestNode *Node
	bestSim := 0

	for _, candidate := range graph.nodes {
		if candidate == child || candidate == parent {
			continue
		}

		candidateSummary := candidate.CubeChord()
		sim := data.ChordSimilarity(&searchChord, &candidateSummary)
		if candidate.Role == RoleTool && candidate.Interface.ActiveCount() > 0 {
			sim += data.ChordSimilarity(&searchChord, &candidate.Interface)
		}

		if sim > bestSim {
			bestSim = sim
			bestNode = candidate
		}
	}

	if bestNode != nil {
		child.Connect(bestNode)
		bestNode.Connect(child)
	}

	return child
}

/*
SpawnRegister materializes a volatile fork register focused on one local
subproblem and control-plane program.
*/
func (graph *Graph) SpawnRegister(parent *Node, input, payload, program data.Chord) *Node {
	return graph.spawnSpecializedNode(parent, RoleRegister, input, payload, program)
}

func (graph *Graph) materializeFork(node *Node) {
	node.forkPending = false
	key := node.candidateToolKey()
	if key.Input.ActiveCount() == 0 {
		return
	}

	if tool, ok := graph.toolCatalog[key]; ok && tool != node {
		node.Connect(tool)
		tool.Connect(node)
		tool.Support++
		return
	}

	graph.toolVotes[key]++

	if node.Role == RoleRegister || node.Role == RoleTool {
		node.Support++
		return
	}

	child := graph.SpawnRegister(node, key.Input, key.Output, key.Program)
	child.Support = max(child.Support, node.Support)
}

func (graph *Graph) promoteRegister(node *Node, key toolKey, votes int) {
	node.Role = RoleTool
	node.Interface = key.Input
	node.Payload = key.Output
	node.Program = key.Program
	node.Support = max(node.Support, votes)

	if node.Program.ActiveCount() > 0 {
		node.Rot = node.Rot.Compose(geometry.RotationForChord(node.Program))
	}

	node.seedSpecialization()
	graph.attachSpecialNode(node)
	graph.toolCatalog[key] = node
}

func (graph *Graph) promoteTools() {
	for key, votes := range graph.toolVotes {
		if votes < toolPromotionThreshold {
			continue
		}

		if existing, ok := graph.toolCatalog[key]; ok {
			existing.Support = max(existing.Support, votes)
			continue
		}

		var best *Node
		bestScore := -1

		for _, node := range graph.nodes {
			if node.Role != RoleRegister || node.candidateToolKey() != key {
				continue
			}

			score := node.Support*8 + key.Output.ActiveCount() + key.Program.ActiveCount()
			if score > bestScore {
				best = node
				bestScore = score
			}
		}

		if best != nil {
			graph.promoteRegister(best, key, votes)
		}
	}
}

func (graph *Graph) composeTools() {
	for _, edge := range graph.Edges() {
		if edge.A.Role != RoleTool || edge.B.Role != RoleTool {
			continue
		}

		if edge.Op != OpCompose || edge.ComposeHits < toolCompositionThreshold {
			continue
		}

		pair := newToolPairKey(edge.A, edge.B)
		if _, ok := graph.compositeCatalog[pair]; ok {
			continue
		}

		input := data.ChordOR(&edge.A.Interface, &edge.B.Interface)
		output := data.ChordOR(&edge.A.Payload, &edge.B.Payload)
		program := data.ChordOR(&edge.A.Program, &edge.B.Program)

		if input.ActiveCount() == 0 {
			input = edge.Resonance
		}
		if output.ActiveCount() == 0 {
			output = edge.Program
		}
		if program.ActiveCount() == 0 {
			program = output
		}

		key := toolKey{Input: input, Output: output, Program: program}
		if existing, ok := graph.toolCatalog[key]; ok {
			existing.Support += edge.ComposeHits
			graph.compositeCatalog[pair] = existing
			continue
		}

		composite := graph.spawnSpecializedNode(edge.A, RoleTool, input, output, program)
		composite.Connect(edge.B)
		edge.B.Connect(composite)
		composite.Support = max(edge.ComposeHits, 1)
		graph.toolCatalog[composite.candidateToolKey()] = composite
		graph.compositeCatalog[pair] = composite
	}
}

func (graph *Graph) reindexToolCatalog() {
	graph.toolCatalog = make(map[toolKey]*Node)
	for _, node := range graph.nodes {
		if node.Role == RoleTool {
			graph.toolCatalog[node.candidateToolKey()] = node
		}
	}
}

func (graph *Graph) attachSpecialNode(node *Node) {
	if node == nil || graph.initialNodes == 0 || len(graph.nodes) == 0 {
		return
	}

	coreCount := graph.initialNodes
	if coreCount > len(graph.nodes) {
		coreCount = len(graph.nodes)
	}

	seed := node.Interface
	if seed.ActiveCount() == 0 {
		seed = node.Payload
	}

	var bestA *Node
	var bestB *Node
	bestAScore := -1
	bestBScore := -1

	for idx := 0; idx < coreCount; idx++ {
		candidate := graph.nodes[idx]
		if candidate == node {
			continue
		}

		summary := candidate.CubeChord()
		score := data.ChordSimilarity(&seed, &summary)
		if candidate == graph.source || candidate == graph.sink {
			score++
		}

		if score > bestAScore {
			bestB, bestBScore = bestA, bestAScore
			bestA, bestAScore = candidate, score
			continue
		}

		if candidate != bestA && score > bestBScore {
			bestB, bestBScore = candidate, score
		}
	}

	if bestA != nil {
		node.Connect(bestA)
		bestA.Connect(node)
	}
	if bestB != nil {
		node.Connect(bestB)
		bestB.Connect(node)
	}

	if node.EdgeCount() == 0 && graph.source != nil {
		node.Connect(graph.source)
		graph.source.Connect(node)
	}
	if node.EdgeCount() < 2 && graph.sink != nil && graph.sink != graph.source {
		node.Connect(graph.sink)
		graph.sink.Connect(node)
	}
}

func (graph *Graph) bindTarget(chord data.Chord) *Node {
	best := graph.source
	bestScore := 0

	for _, node := range graph.nodes {
		summary := node.CubeChord()
		score := data.ChordSimilarity(&chord, &summary)
		if node.Interface.ActiveCount() > 0 {
			score += 2 * data.ChordSimilarity(&chord, &node.Interface)
		}
		if node.Payload.ActiveCount() > 0 {
			score += data.ChordSimilarity(&chord, &node.Payload)
		}
		if node.Role == RoleTool && node.matchesInterface(chord) {
			score += max(node.Interface.ActiveCount(), 1) * 2
		}

		if score > bestScore {
			bestScore = score
			best = node
		}
	}

	if best == nil {
		return graph.source
	}

	return best
}
