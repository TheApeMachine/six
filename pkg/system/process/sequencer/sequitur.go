package sequencer

import (
	"math/bits"
	"sync"

	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/system/console"
	"github.com/theapemachine/six/pkg/system/process"
)

/*
Symbol represents either a raw byte (0–255) or a Rule ID (≥256).
*/
type Symbol int

/*
Node is a doubly-linked element in the Sequitur sequence.
*/
type Node struct {
	Val        Symbol
	Prev, Next *Node
}

/*
Rule is a grammar production discovered by Sequitur.
Head is a circular sentinel whose body nodes are Head.Next … until Head again.
*/
type Rule struct {
	ID    int
	Count int
	Head  *Node
}

/*
Sequitur discovers hierarchical structure in a symbol stream via
online digram replacement. Exact repeated digrams are promoted
immediately; near-repeated raw-byte digrams may also promote when
the residual bit flips are still cheaper than a second raw encoding.
Rule IDs start at 256 so they never collide with raw bytes.
*/
type Sequitur struct {
	mu              sync.Mutex
	calc            *numeric.Calculus
	nextID          int
	lastRuleID      int
	rules           map[int]*Rule
	digrams         map[[2]Symbol]*Node
	sentinel        *Node
	length          int
	pending         []byte
	pendingPos      uint32
	approxTolerance int
}

/*
NewSequitur creates a new Sequitur.
*/
func NewSequitur() *Sequitur {
	sentinel := &Node{Val: -1}
	sentinel.Next = sentinel
	sentinel.Prev = sentinel

	return &Sequitur{
		calc:            numeric.NewCalculus(),
		nextID:          256,
		rules:           make(map[int]*Rule),
		digrams:         make(map[[2]Symbol]*Node),
		sentinel:        sentinel,
		approxTolerance: 2,
	}
}

/*
Analyze appends a byte to the grammar, runs digram replacement,
and returns boundary information compatible with the Sequencer interface.
A boundary fires whenever a new Rule is created (the digram was seen
before), signalling a structural repetition — the Sequitur equivalent
of an MDL boundary.
*/
func (sequitur *Sequitur) Analyze(
	pos uint32, byteVal byte,
) (bool, int, []int, data.Value) {
	sequitur.mu.Lock()

	sequitur.pending = append(sequitur.pending, byteVal)

	newNode := &Node{Val: Symbol(byteVal)}
	sequitur.appendNode(newNode)
	sequitur.length++

	ruleCreated := sequitur.check(newNode.Prev)

	if !ruleCreated {
		sequitur.mu.Unlock()
		return false, 0, nil, data.Value{}
	}

	pendingCopy := append([]byte(nil), sequitur.pending...)
	emitK := len(pendingCopy)
	lastRuleID := sequitur.lastRuleID
	calc := sequitur.calc

	sequitur.pending = nil
	sequitur.pendingPos = pos + 1

	sequitur.mu.Unlock()

	console.Trace("sequence", "bytes", string(pendingCopy))

	meta := ruleMetaValue(lastRuleID, calc)

	return true, emitK, []int{process.EventPhaseInversion}, meta
}

/*
Flush drains any remaining pending bytes as a final boundary.
*/
func (sequitur *Sequitur) Flush() (bool, int, []int, data.Value) {
	sequitur.mu.Lock()
	defer sequitur.mu.Unlock()

	if len(sequitur.pending) == 0 {
		return false, 0, nil, data.Value{}
	}

	emitK := len(sequitur.pending)
	sequitur.pending = nil

	meta := ruleMetaValue(sequitur.lastRuleID, sequitur.calc)

	return true, emitK, []int{
		process.EventPhaseInversion,
		process.EventLowVarianceFlux,
		process.EventDensitySpike,
		process.EventDensityTrough,
	}, meta
}

/*
RuleCount returns the number of discovered grammar rules.
*/
func (sequitur *Sequitur) RuleCount() int {
	sequitur.mu.Lock()
	defer sequitur.mu.Unlock()

	return len(sequitur.rules)
}

/*
appendNode inserts n before the sentinel (i.e. at the tail of the sequence).
*/
func (sequitur *Sequitur) appendNode(n *Node) {
	last := sequitur.sentinel.Prev

	n.Next = sequitur.sentinel
	n.Prev = last
	last.Next = n
	sequitur.sentinel.Prev = n
}

/*
check tests whether the digram ending at n forms a repeated pair.
If a match is found (non-overlapping), reduce fires. Returns true
when a new rule was created.
*/
func (sequitur *Sequitur) check(n *Node) bool {
	if n == nil || n.Next == nil || n.Val == -1 || n.Next.Val == -1 {
		return false
	}

	digram := [2]Symbol{n.Val, n.Next.Val}

	if match, exists := sequitur.digrams[digram]; exists {
		if match != n && match.Next != n && match.Next != n.Next &&
			match.Next != nil && match.Val == digram[0] && match.Next.Val == digram[1] {
			sequitur.reduce(n, match, digram)
			return true
		}

		return false
	}

	if match, ok := sequitur.findApproximateMatch(digram, n); ok {
		if match.Next != n.Next {
			sequitur.reduce(n, match, digram)
			return true
		}

		return false
	}

	sequitur.digrams[digram] = n

	return false
}

/*
findApproximateMatch scans previously seen digrams for a near-match whose
residual description is still cheaper than treating the new digram as raw data.
This is the lossy MDL track described in SEQUENCING.md.
*/
func (sequitur *Sequitur) findApproximateMatch(digram [2]Symbol, current *Node) (*Node, bool) {
	bestDistance := sequitur.approxTolerance + 1
	var best *Node

	for candidateDigram, candidateNode := range sequitur.digrams {
		if candidateNode == current || candidateNode.Next == current || candidateNode.Next == current.Next {
			continue
		}

		distance, comparable := digramDistance(digram, candidateDigram)
		if !comparable || !sequitur.acceptApproximateDigram(distance) {
			continue
		}

		if distance < bestDistance {
			bestDistance = distance
			best = candidateNode
		}
	}

	return best, best != nil
}

/*
acceptApproximateDigram applies a small MDL gate: a two-symbol rule only wins
when the residual bit flips are cheaper than encoding a second raw digram.
*/
func (sequitur *Sequitur) acceptApproximateDigram(distance int) bool {
	if distance < 0 || distance > sequitur.approxTolerance {
		return false
	}

	const rawDigramBits = 16
	const rulePointerBits = 8

	return rawDigramBits > rulePointerBits+distance
}

/*
digramDistance returns the total bit-flip distance between two raw-byte digrams.
Rule digrams are compared exactly only; approximate matching is reserved for raw
stream symbols so the grammar does not drift into nonsense.
*/
func digramDistance(left, right [2]Symbol) (int, bool) {
	if left[0] < 0 || left[0] > 255 || left[1] < 0 || left[1] > 255 {
		return 0, false
	}

	if right[0] < 0 || right[0] > 255 || right[1] < 0 || right[1] > 255 {
		return 0, false
	}

	return bits.OnesCount8(uint8(left[0]^right[0])) + bits.OnesCount8(uint8(left[1]^right[1])), true
}

/*
reduce creates a new grammar rule for the repeated digram and replaces
both occurrences with the rule's non-terminal symbol.
Both substitutions complete before any recursive check; otherwise a nested
reduce can invalidate the second substitution.
*/
func (sequitur *Sequitur) reduce(
	newOccur, firstOccur *Node, d [2]Symbol,
) {
	ruleID := sequitur.nextID
	sequitur.nextID++
	sequitur.lastRuleID = ruleID

	head := &Node{Val: -1}
	n1 := &Node{Val: d[0], Prev: head}
	n2 := &Node{Val: d[1], Prev: n1, Next: head}
	head.Next = n1
	head.Prev = n2
	n1.Next = n2

	sequitur.rules[ruleID] = &Rule{
		ID:    ruleID,
		Count: 2,
		Head:  head,
	}

	sequitur.substitute(firstOccur, ruleID)
	sequitur.substitute(newOccur, ruleID)

	delete(sequitur.digrams, d)

	sequitur.checkAdjacencies(firstOccur, newOccur)
}

/*
substitute replaces a two-node digram starting at n with a single
non-terminal node. Does not recurse; caller must check adjacencies.
*/
func (sequitur *Sequitur) substitute(n *Node, id int) {
	if n == nil || n.Next == nil || n.Next.Next == nil {
		return
	}

	victim := n.Next

	n.Val = Symbol(id)
	n.Next = victim.Next
	victim.Next.Prev = n

	victim.Prev = nil
	victim.Next = nil
}

/*
checkAdjacencies re-checks the nodes around both substituted positions
for fresh digrams. Called after both substitutions in reduce to avoid
nested reduce invalidating the second substitution.
*/
func (sequitur *Sequitur) checkAdjacencies(a, b *Node) {
	if a != nil && a.Prev != nil && a.Prev.Val != -1 {
		sequitur.check(a.Prev)
	}
	if a != nil && a.Val != -1 {
		sequitur.check(a)
	}
	if b != nil && b != a && b.Prev != nil && b.Prev.Val != -1 {
		sequitur.check(b.Prev)
	}
	if b != nil && b != a && b.Val != -1 {
		sequitur.check(b)
	}
}

/*
ruleMetaValue encodes a grammar rule's structural identity as a value.
The rule ID is projected into GF(257) via the primitive root (3^ruleID mod 257),
and the resulting phase is set as a single active bit. This gives each
discovered rule a unique rotational signature without touching raw bytes.
*/
func ruleMetaValue(ruleID int, calc *numeric.Calculus) data.Value {
	phase := calc.Power(
		numeric.Phase(numeric.FermatPrimitive), uint32(ruleID),
	)

	value := data.MustNewValue()
	value.Set(int(phase))

	return value
}
