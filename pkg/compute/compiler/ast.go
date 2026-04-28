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
}

func (assign AssignNode) Walk(b *Builder) {
	prevTarget := b.currentTarget
	prevSrcA := b.srcAFromB

	b.currentTarget = assign.Target
	b.srcAFromB = assign.SrcAFromB

	b.Pack(assign.Opcode, assign.SrcA, assign.SrcB, assign.Dst)

	b.currentTarget = prevTarget
	b.srcAFromB = prevSrcA
}

/*
PredicateAssignNode lowers `set X <- popcnt(Y)` and `set X <- any_zero(Y)`
into a single predicate instruction whose result lands directly in X.
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
ReduceAssignNode lowers lane-level categorical reducers such as
argmin_nonzero(value, key) and mode_eq(value, key, match). These reducers are
generic ALU operations over region references; they do not know what the
regions mean.
*/
type ReduceAssignNode struct {
	Dst   Region
	Op    uint64
	Value Region
	Key   Region
	Match Region
}

func (node ReduceAssignNode) Walk(b *Builder) {
	b.Reduce(node.Op, node.Value, node.Key, node.Match, node.Dst)
}

/*
IfNode emits a real popcount-compare instruction whose result becomes
the per-block predication mask. Body writes are gated by that mask
all-or-nothing.
*/
type IfNode struct {
	Cond       uint64
	Prelude    BlockNode
	Region     Region
	RegionSide byte
	Threshold  Region
	Mask       Region
	Body       BlockNode
}

func (ifStmt IfNode) Walk(b *Builder) {
	ifStmt.Prelude.Walk(b)
	b.Predicate(ifStmt.Cond, ifStmt.Region, ifStmt.Threshold, ifStmt.Mask, srcAFromSide(ifStmt.RegionSide))

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
PopBNode emits a single dedicated pop-seed instruction (topology=Pop on
an identity write) so the kernel binds currentB once. Body instructions
then run topology=Local and inherit that pointer, which is what the
recruit-style `pop(B) { ... }` semantics need.
*/
type PopBNode struct {
	Body BlockNode
}

func (pop PopBNode) Walk(b *Builder) {
	prevTopo := b.currentTopo

	b.currentTopo = TopoPopQueue
	b.Pack(OpCopyA, ID, ID, ID)

	b.currentTopo = TopoLocal
	bodyStart := b.pc
	pop.Body.Walk(b)

	// Mark the last body instruction so the kernel knows where the pop
	// loop ends and can rewind to bodyStart for the next B in the lane.
	if b.pc > bodyStart {
		b.instructions[b.pc-1] |= uint64(1) << 63
	}

	b.currentTopo = prevTopo
}

/*
GossipNode lowers `gossip(B) { ... }`. Every body instruction runs with
topology=Hypercube so the kernel rebinds currentB to the dim-d peer at
each pc (d = pc % dimCount). No seed instruction is needed because the
peer binding is pc-positional, not queue-popping.
*/
type GossipNode struct {
	Body BlockNode
}

func (gossip GossipNode) Walk(b *Builder) {
	prevTopo := b.currentTopo
	b.currentTopo = TopoHypercube

	gossip.Body.Walk(b)

	b.currentTopo = prevTopo
}

/*
StageNode lowers `stage(B)`: a single instruction that, for the currently
bound popped B, pushes that B into the backend's staging lane keyed by
A.properties.reference. The kernel observes the stage bit and short-
circuits the truth-table body; the instruction is purely a host-visible
side effect on the staging map. Programs use this to declare "the next
program belongs to whoever's id is in my reference word, and these are
its inputs" entirely in-band, without any Go-side scanning.
*/
type StageNode struct{}

func (stage StageNode) Walk(b *Builder) {
	prevStage := b.stageFlag
	b.stageFlag = 1

	// Identity write under the current pop topology so the kernel binds
	// currentB before observing the stage bit. Region operands are placeholders
	// — the kernel ignores them when stage=1.
	b.Pack(OpFalse, ID, ID, ID)

	b.stageFlag = prevStage
}

/*
EmitNode runs its body and marks the final body instruction as the spawn
edge. This keeps `emit { ... }` as one child allocation no matter how many
assignments are needed to shape the emitted frame.
*/
type EmitNode struct {
	Body BlockNode
}

func (emit EmitNode) Walk(b *Builder) {
	start := b.pc
	emit.Body.Walk(b)

	if b.pc > start {
		b.instructions[b.pc-1] |= uint64(1) << 54
	}
}
