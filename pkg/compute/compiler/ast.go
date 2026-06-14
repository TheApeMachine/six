package compiler

type ASTNode interface {
	Walk(b *Builder)
}

// BlockNode is a sequence of statements that share the surrounding
// builder state (mask, topology, target).
type BlockNode struct {
	Statements []ASTNode
}

func (block BlockNode) Walk(b *Builder) {
	for _, stmt := range block.Statements {
		stmt.Walk(b)
	}
}

/*
AssignNode lowers a single `set` / `write` statement into one ALU
instruction. The Target bit chooses owner vs popped destination, and
SrcAFromB asserts the kernel's "operate entirely on the popped frame"
mode for `target B { ... }` blocks.
*/
type AssignNode struct {
	Dst       Region
	SrcA      Region
	SrcB      Region
	Opcode    uint64
	Target    uint64 // 0 for A, 1 for B
	SrcAFromB uint64 // 1 = read SrcA from popped (target-B blocks)
	BRotate   uint64 // truth-table SrcB byte rotation, 0..7
}

func (assign AssignNode) Walk(b *Builder) {
	prevTarget := b.currentTarget
	prevSrcA := b.srcAFromB
	prevBRotate := b.bRotate

	b.currentTarget = assign.Target
	b.srcAFromB = assign.SrcAFromB
	b.bRotate = assign.BRotate

	b.Pack(assign.Opcode, assign.SrcA, assign.SrcB, assign.Dst)

	b.currentTarget = prevTarget
	b.srcAFromB = prevSrcA
	b.bRotate = prevBRotate
}

/*
PredicateAssignNode lowers `set X <- popcnt(Y)` into a single predicate
instruction whose result lands directly in X.
*/
type PredicateAssignNode struct {
	Dst        Region
	Cond       uint64
	Region     Region
	RegionSide byte
	Target     uint64
}

func (node PredicateAssignNode) Walk(b *Builder) {
	prevTarget := b.currentTarget
	b.currentTarget = node.Target

	// Threshold operand is unused for store / any-zero kinds. Pass dst
	// as a harmless filler so the bStart bits at least decode to a
	// real word.
	b.Predicate(node.Cond, node.Region, node.Dst, node.Dst, srcAFromSide(node.RegionSide))

	b.currentTarget = prevTarget
}

/*
ScalarAssignNode lowers `shiftl`, `shiftr`, `rotl`, and `rotr` into the
scalar sublane. It stays first-order: value and amount are ordinary regions,
and the kernel runs the operation inline with the same mask and target routing
as a truth-table write.
*/
type ScalarAssignNode struct {
	Dst        Region
	Op         uint64
	Value      Region
	ValueSide  byte
	Amount     Region
	AmountSide byte
	Target     uint64
}

func (node ScalarAssignNode) Walk(b *Builder) {
	prevTarget := b.currentTarget
	b.currentTarget = node.Target

	srcAFromB := uint64(0)
	if node.ValueSide == 'B' {
		srcAFromB = 1
	}

	b.Scalar(node.Op, node.Value, node.Amount, node.Dst, srcAFromB)

	b.currentTarget = prevTarget
}

/*
GeometricNode lowers a resident PGA operation into a raw geometric slot.
The slot is intentionally not a boolean instruction: the opcode byte is the
whole word, so the runtime dispatches it directly to the geometric lane.
*/
type GeometricNode struct {
	Op uint64
}

func (node GeometricNode) Walk(b *Builder) {
	b.Geometric(node.Op)
}

/*
IfNode emits a real popcount-compare instruction whose result becomes
the per-block predication mask. Body writes are gated by that mask.

Inside a TopoHypercubePerPeer block (a `gossip(B) { (...) { ... } }`
shape) the predicate fires per peer and the mask lives on the peer
frame; outside such a block the mask is owner-side and the predicate
fires once per instruction.

The two *IndirectAddr fields carry the indirection addresses for source
operands written as `B.{{addr}}` / `{{addr}}` in the source. A non-zero
value means "patch this operand's 7-bit field at install time with the
low 7 bits of the word at addr on the Value frame". Zero means the
operand is direct and the encoder leaves the packed bits alone.
*/
type IfNode struct {
	Cond                  uint64
	Prelude               BlockNode
	Region                Region
	RegionSide            byte
	RegionIndirectAddr    uint64
	Threshold             Region
	ThresholdIndirectAddr uint64
	Mask                  Region
	Body                  BlockNode
}

func (ifStmt IfNode) Walk(b *Builder) {
	ifStmt.Prelude.Walk(b)
	b.Predicate(ifStmt.Cond, ifStmt.Region, ifStmt.Threshold, ifStmt.Mask, srcAFromSide(ifStmt.RegionSide))

	if ifStmt.RegionIndirectAddr != 0 {
		b.RecordSubstitution(SubstAStartShift, ifStmt.RegionIndirectAddr)
	}

	if ifStmt.ThresholdIndirectAddr != 0 {
		b.RecordSubstitution(SubstBStartShift, ifStmt.ThresholdIndirectAddr)
	}

	prevMask := b.currentMask
	b.currentMask = ifStmt.Mask

	ifStmt.Body.Walk(b)

	b.currentMask = prevMask
}

func srcAFromSide(side byte) uint64 {
	if side == 'B' {
		return 1
	}

	return 0
}

/*
GossipNode lowers `gossip(B) { ... }`. The topology depends on whether
the body wraps an IfNode: a plain gossip block stays on TopoHypercube
(owner-side mask, broadcast write per peer); a predicated gossip block
switches to TopoHypercubePerPeer so the kernel evaluates the predicate
per peer, writes the per-peer mask to the peer frame, and gates body
broadcasts on it. The compiler also rewrites the predicate's mask
target into the per-peer scratch slot so the kernel knows where to
read the mask from.
*/
type GossipNode struct {
	Body BlockNode
}

func (gossip GossipNode) Walk(b *Builder) {
	prevTopo := b.currentTopo

	if gossipHasPredicate(gossip.Body) {
		b.currentTopo = TopoHypercubePerPeer
		retargetPredicateMasks(gossip.Body, Region{Start: PerPeerMaskWord, Span: 1})
	} else {
		b.currentTopo = TopoHypercube
	}

	gossip.Body.Walk(b)

	b.currentTopo = prevTopo
}

func gossipHasPredicate(block BlockNode) bool {
	for _, stmt := range block.Statements {
		if _, ok := stmt.(IfNode); ok {
			return true
		}
	}

	return false
}

/*
retargetPredicateMasks rewrites every IfNode mask in this gossip body
to point at the per-peer scratch slot. The owner-side mask the parser
allocated stays in the asset region but goes unused — the per-peer
flow reads and writes the dedicated peer frame slot instead.
*/
func retargetPredicateMasks(block BlockNode, mask Region) {
	for idx, stmt := range block.Statements {
		ifNode, ok := stmt.(IfNode)
		if !ok {
			continue
		}

		ifNode.Mask = mask
		block.Statements[idx] = ifNode
	}
}

/*
EmitNode runs its body against the child target selected by the parser for
bare destinations. The ALU emits one child when at least one child-target
write survives the active predicate mask.
*/
type EmitNode struct {
	Body BlockNode
}

func (emit EmitNode) Walk(b *Builder) {
	bodyStart := b.pc
	emit.Body.Walk(b)

	if b.pc > bodyStart {
		b.instructions[b.pc-1] |= uint64(1) << 62
	}
}
