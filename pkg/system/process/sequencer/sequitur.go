package sequencer

import (
	"math/bits"
	"sync"
)

/*
Symbol represents either a raw byte (0–255) or a Rule ID (≥256).
*/
type Symbol int

/*
Node is a doubly-linked element in the Sequitur sequence.
Prev and Next are int32 arena indices; -1 is the nil sentinel.
*/
type Node struct {
	Val        Symbol
	Prev, Next int32
}

/*
Rule is a grammar production discovered by Sequitur.
Head is an arena index to a circular sentinel whose body nodes are
Head.Next … until Head again.
*/
type Rule struct {
	ID    int
	Count int
	Head  int32
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
	nextID          int
	rules           map[int]*Rule
	digrams         map[[2]Symbol]int32
	sentinel        int32
	arena           *NodeArena
	length          int
	pendingLen      int
	approxTolerance int
}

/*
NewSequitur creates a new Sequitur backed by a pre-allocated node arena.
*/
func NewSequitur() *Sequitur {
	arena := NewNodeArena(4096)
	sentinel := arena.Alloc(-1)

	node := arena.Get(sentinel)
	node.Next = sentinel
	node.Prev = sentinel

	return &Sequitur{
		nextID:          256,
		rules:           make(map[int]*Rule),
		digrams:         make(map[[2]Symbol]int32),
		sentinel:        sentinel,
		arena:           arena,
		approxTolerance: 2,
	}
}

/*
Analyze appends a byte to the grammar, runs digram replacement,
and returns the byte unchanged plus whether a boundary fired.
A boundary fires when a new Rule is created (the digram was seen before).
*/
func (sequitur *Sequitur) Analyze(pos uint32, byteVal byte) (byte, bool) {
	sequitur.mu.Lock()

	newIdx := sequitur.arena.Alloc(Symbol(byteVal))
	sequitur.appendNode(newIdx)
	sequitur.length++
	sequitur.pendingLen++

	prevIdx := sequitur.arena.Get(newIdx).Prev
	ruleCreated := sequitur.check(prevIdx)

	if ruleCreated {
		sequitur.pendingLen = 0
	}

	sequitur.mu.Unlock()

	return byteVal, ruleCreated
}

/*
Flush reports whether there are bytes since the last boundary.
The caller should treat true as a flush boundary and emit their buffer.
*/
func (sequitur *Sequitur) Flush() bool {
	sequitur.mu.Lock()
	defer sequitur.mu.Unlock()

	hasPending := sequitur.pendingLen > 0
	sequitur.pendingLen = 0

	return hasPending
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
appendNode inserts the node at idx before the sentinel (tail of the sequence).
*/
func (sequitur *Sequitur) appendNode(idx int32) {
	sentPrev := sequitur.arena.Get(sequitur.sentinel).Prev

	node := sequitur.arena.Get(idx)
	node.Next = sequitur.sentinel
	node.Prev = sentPrev

	sequitur.arena.Get(sentPrev).Next = idx
	sequitur.arena.Get(sequitur.sentinel).Prev = idx
}

/*
check tests whether the digram ending at idx forms a repeated pair.
If a match is found (non-overlapping), reduce fires. Returns true
when a new rule was created.
*/
func (sequitur *Sequitur) check(idx int32) bool {
	if idx == -1 {
		return false
	}

	nodeVal := sequitur.arena.Get(idx).Val
	nodeNext := sequitur.arena.Get(idx).Next

	if nodeNext == -1 || nodeVal == -1 {
		return false
	}

	nextVal := sequitur.arena.Get(nodeNext).Val
	if nextVal == -1 {
		return false
	}

	digram := [2]Symbol{nodeVal, nextVal}

	if matchIdx, exists := sequitur.digrams[digram]; exists {
		if !sequitur.isValidDigramStart(matchIdx, digram) {
			delete(sequitur.digrams, digram)
			sequitur.digrams[digram] = idx
			return false
		}

		matchNode := sequitur.arena.Get(matchIdx)
		if matchIdx != idx && matchNode.Next != idx && matchNode.Next != nodeNext {
			sequitur.reduce(idx, matchIdx, digram)
			return true
		}

		return false
	}

	if matchIdx, ok := sequitur.findApproximateMatch(digram, idx); ok {
		if sequitur.arena.Get(matchIdx).Next != nodeNext {
			sequitur.reduce(idx, matchIdx, digram)
			return true
		}

		return false
	}

	sequitur.digrams[digram] = idx

	return false
}

/*
findApproximateMatch scans previously seen digrams for a near-match whose
residual description is still cheaper than treating the new digram as raw data.
This is the lossy MDL track described in SEQUENCING.md.
*/
func (sequitur *Sequitur) findApproximateMatch(digram [2]Symbol, current int32) (int32, bool) {
	bestDistance := sequitur.approxTolerance + 1
	bestIdx := int32(-1)
	currentNext := sequitur.arena.Get(current).Next

	for candidateDigram, candidateIdx := range sequitur.digrams {
		if candidateIdx == current {
			continue
		}

		if !sequitur.isValidDigramStart(candidateIdx, candidateDigram) {
			delete(sequitur.digrams, candidateDigram)
			continue
		}

		candidateNext := sequitur.arena.Get(candidateIdx).Next
		if candidateNext == current || candidateNext == currentNext {
			continue
		}

		distance, comparable := digramDistance(digram, candidateDigram)
		if !comparable || !sequitur.acceptApproximateDigram(distance) {
			continue
		}

		if distance < bestDistance {
			bestDistance = distance
			bestIdx = candidateIdx
		}
	}

	return bestIdx, bestIdx != -1
}

/*
isValidDigramStart verifies that idx still anchors an active two-symbol digram
with the expected values. Stale digram entries must be rejected before reduce.
*/
func (sequitur *Sequitur) isValidDigramStart(idx int32, digram [2]Symbol) bool {
	if idx == -1 || idx == sequitur.sentinel {
		return false
	}

	node := sequitur.arena.Get(idx)
	if node.Val != digram[0] {
		return false
	}

	nextIdx := node.Next
	if nextIdx == -1 || nextIdx == sequitur.sentinel {
		return false
	}

	nextNode := sequitur.arena.Get(nextIdx)
	return nextNode.Val == digram[1]
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
func (sequitur *Sequitur) reduce(newOccur, firstOccur int32, d [2]Symbol) {
	ruleID := sequitur.nextID
	sequitur.nextID++

	head := sequitur.arena.Alloc(-1)
	n1 := sequitur.arena.Alloc(d[0])
	n2 := sequitur.arena.Alloc(d[1])

	headNode := sequitur.arena.Get(head)
	headNode.Next = n1
	headNode.Prev = n2

	n1Node := sequitur.arena.Get(n1)
	n1Node.Prev = head
	n1Node.Next = n2

	n2Node := sequitur.arena.Get(n2)
	n2Node.Prev = n1
	n2Node.Next = head

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
substitute replaces a two-node digram starting at idx with a single
non-terminal node. The victim node is freed back to the arena.
Does not recurse; caller must check adjacencies.
*/
func (sequitur *Sequitur) substitute(idx int32, id int) {
	if idx == -1 {
		return
	}

	victimIdx := sequitur.arena.Get(idx).Next
	if victimIdx == -1 {
		return
	}

	victimNext := sequitur.arena.Get(victimIdx).Next
	if victimNext == -1 {
		return
	}

	node := sequitur.arena.Get(idx)
	node.Val = Symbol(id)
	node.Next = victimNext

	sequitur.arena.Get(victimNext).Prev = idx

	sequitur.arena.Free(victimIdx)
}

/*
checkAdjacencies re-checks the nodes around both substituted positions
for fresh digrams. Called after both substitutions in reduce to avoid
nested reduce invalidating the second substitution.
Pointers are re-obtained after each check call because recursive reduce
may grow the arena slice.
*/
func (sequitur *Sequitur) checkAdjacencies(aIdx, bIdx int32) {
	if aIdx != -1 {
		aPrev := sequitur.arena.Get(aIdx).Prev
		if aPrev != -1 && sequitur.arena.Get(aPrev).Val != -1 {
			sequitur.check(aPrev)
		}
	}

	if aIdx != -1 && sequitur.arena.Get(aIdx).Val != -1 {
		sequitur.check(aIdx)
	}

	if bIdx != -1 && bIdx != aIdx {
		bPrev := sequitur.arena.Get(bIdx).Prev
		if bPrev != -1 && sequitur.arena.Get(bPrev).Val != -1 {
			sequitur.check(bPrev)
		}
	}

	if bIdx != -1 && bIdx != aIdx && sequitur.arena.Get(bIdx).Val != -1 {
		sequitur.check(bIdx)
	}
}
