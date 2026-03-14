package process

import (
	"sync"

	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/system/console"
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
online digram replacement. Each repeated digram is promoted to a
grammar rule, building a lossless context-free grammar over the
input. Rule IDs start at 256 so they never collide with raw bytes.
*/
type Sequitur struct {
	mu         sync.Mutex
	calc       *numeric.Calculus
	nextID     int
	lastRuleID int
	rules      map[int]*Rule
	digrams    map[[2]Symbol]*Node
	sentinel   *Node
	length     int
	pending    []byte
	pendingPos uint32
}

/*
NewSequitur creates a new Sequitur.
*/
func NewSequitur() *Sequitur {
	sentinel := &Node{Val: -1}
	sentinel.Next = sentinel
	sentinel.Prev = sentinel

	return &Sequitur{
		calc:     numeric.NewCalculus(),
		nextID:   256,
		rules:    make(map[int]*Rule),
		digrams:  make(map[[2]Symbol]*Node),
		sentinel: sentinel,
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
) (bool, int, []int, data.Chord) {
	sequitur.mu.Lock()
	defer sequitur.mu.Unlock()

	sequitur.pending = append(sequitur.pending, byteVal)

	newNode := &Node{Val: Symbol(byteVal)}
	sequitur.appendNode(newNode)
	sequitur.length++

	ruleCreated := sequitur.check(newNode.Prev)

	if ruleCreated {
		console.Trace("sequence", "bytes", sequitur.pending)
		emitK := len(sequitur.pending)
		sequitur.pending = nil
		sequitur.pendingPos = pos + 1

		meta := ruleMetaChord(sequitur.lastRuleID, sequitur.calc)

		return true, emitK, []int{EventPhaseInversion}, meta
	}

	return false, 0, nil, data.Chord{}
}

/*
Flush drains any remaining pending bytes as a final boundary.
*/
func (sequitur *Sequitur) Flush() (bool, int, []int, data.Chord) {
	sequitur.mu.Lock()
	defer sequitur.mu.Unlock()

	if len(sequitur.pending) == 0 {
		return false, 0, nil, data.Chord{}
	}

	emitK := len(sequitur.pending)
	sequitur.pending = nil

	meta := ruleMetaChord(sequitur.lastRuleID, sequitur.calc)

	return true, emitK, []int{EventPhaseInversion}, meta
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
	if n.Val == -1 || n.Next.Val == -1 {
		return false
	}

	digram := [2]Symbol{n.Val, n.Next.Val}

	if match, exists := sequitur.digrams[digram]; exists {
		if match != n && match.Next != n {
			sequitur.reduce(n, match, digram)
			return true
		}

		return false
	}

	sequitur.digrams[digram] = n

	return false
}

/*
reduce creates a new grammar rule for the repeated digram and replaces
both occurrences with the rule's non-terminal symbol.
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
}

/*
substitute replaces a two-node digram starting at n with a single
non-terminal node, then re-checks the new adjacencies for fresh digrams.
*/
func (sequitur *Sequitur) substitute(n *Node, id int) {
	victim := n.Next

	n.Val = Symbol(id)
	n.Next = victim.Next
	victim.Next.Prev = n

	victim.Prev = nil
	victim.Next = nil

	if n.Prev.Val != -1 {
		sequitur.check(n.Prev)
	}

	if n.Next.Val != -1 {
		sequitur.check(n)
	}
}

/*
ruleMetaChord encodes a grammar rule's structural identity as a chord.
The rule ID is projected into GF(257) via the primitive root (3^ruleID mod 257),
and the resulting phase is set as a single active bit. This gives each
discovered rule a unique rotational signature without touching raw bytes.
*/
func ruleMetaChord(ruleID int, calc *numeric.Calculus) data.Chord {
	phase := calc.Power(
		numeric.Phase(numeric.FermatPrimitive), uint32(ruleID),
	)

	chord := data.MustNewChord()
	chord.Set(int(phase))

	return chord
}
