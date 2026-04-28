package compiler

/*
Truth-table opcodes. Each is the 4-bit selector the kernel feeds to its
branchless ALU; the comment shows the boolean function on the (A,B) pair.
*/
const (
	OpFalse    = 0b0000 // 0
	OpAnd      = 0b0001 // A & B
	OpAandNotB = 0b0010 // A & ~B  (alias: and_not)
	OpCopyA    = 0b0011 // A
	OpNotAandB = 0b0100 // ~A & B
	OpCopyB    = 0b0101 // B
	OpXor      = 0b0110 // A ^ B
	OpOr       = 0b0111 // A | B
	OpNor      = 0b1000 // ~(A | B)
	OpXnor     = 0b1001 // ~(A ^ B)
	OpNotB     = 0b1010 // ~B
	OpIfBthenA = 0b1011 // A | ~B  (alias: truth_arrow)
	OpNotA     = 0b1100 // ~A
	OpIfAthenB = 0b1101 // ~A | B
	OpNand     = 0b1110 // ~(A & B)
	OpTrue     = 0b1111 // 1
)

const (
	OpReduceArgMinNonZero = OpAnd
	OpReduceModeEq        = OpAandNotB
)

// Topologies
const (
	TopoLocal     = 0b00
	TopoPopQueue  = 0b01
	TopoHypercube = 0b10
)

/*
Predicate condition codes (popcount(A) cmp threshold OR reduction). Must
mirror the kernel's cpu.Pred* constants verbatim — the wire format is the
contract between the compiler and the ALU.
*/
const (
	PredLT          = 0
	PredLE          = 1
	PredGT          = 2
	PredGE          = 3
	PredEQ          = 4
	PredNE          = 5
	PredStorePopcnt = 6
	PredAnyZero     = 7
)

// Region defines a physical memory location on the 1KB organism.
type Region struct {
	Start uint64
	Span  uint64
}

/*
Frame layout (128 uint64 words = 1 KiB). Sourced from the canonical
primitive layout — keep these in sync with primitive.layout_generated.go
so a programmer-authored offset and a runtime-resolved offset always
land on the same word.
*/
const (
	tokensStart     = 0
	tokensWords     = 16
	programStart    = 16
	programWords    = 16
	signalsStart    = 32
	signalsWords    = 8
	contextStart    = 40
	contextWords    = 8
	gradientStart   = 48
	gradientWords   = 8
	propertiesStart = 56
	assetStart      = 72
	assetWords      = 48
	prevStart       = 120
	nextStart       = 121
	idStart         = 122
	affinityStart   = 123
	affinityWords   = 5

	maskTrueWord      = 72 // first word of the asset region; reserved
	assetConstStart   = 73 // compiler-allocated constants begin here
	spawnRegisterWord = 70
)

var (
	Tokens     = Region{Start: tokensStart, Span: tokensWords}
	Program    = Region{Start: programStart, Span: programWords}
	Signals    = Region{Start: signalsStart, Span: signalsWords}
	Context    = Region{Start: contextStart, Span: contextWords}
	Gradient   = Region{Start: gradientStart, Span: gradientWords}
	Properties = Region{Start: propertiesStart, Span: 16}
	Asset      = Region{Start: assetStart, Span: assetWords}
	Prev       = Region{Start: prevStart, Span: 1}
	Next       = Region{Start: nextStart, Span: 1}
	ID         = Region{Start: idStart, Span: 1}
	Affinity   = Region{Start: affinityStart, Span: affinityWords}

	MaskTrue = Region{Start: maskTrueWord, Span: 1}
)

/*
PropertyOffsets maps the canonical property names to absolute frame
words. The order mirrors primitive.PropertyType so a config rewrite
can use property names freely without re-deriving offsets.
*/
var PropertyOffsets = map[string]uint64{
	"labels":          propertiesStart + 0,
	"confidence":      propertiesStart + 1,
	"epoch":           propertiesStart + 2,
	"ttl":             propertiesStart + 3,
	"temperature":     propertiesStart + 4,
	"status":          propertiesStart + 5,
	"noise":           propertiesStart + 6,
	"program_id":      propertiesStart + 7,
	"community":       propertiesStart + 8,
	"target":          propertiesStart + 9,
	"role":            propertiesStart + 10,
	"reference":       propertiesStart + 11,
	"surprisal":       propertiesStart + 12,
	"prev_surprisal":  propertiesStart + 13,
	"delta_surprisal": propertiesStart + 14,
	"continuation":    propertiesStart + 15,
}

// StatusEnum mirrors the StatusType constants in primitive.
var StatusEnum = map[string]uint64{
	"PENDING":  0,
	"READY":    1,
	"BUSY":     2,
	"WAITING":  3,
	"DONE":     4,
	"RESOLVED": 5,
	"ERROR":    6,
}

// RoleEnum mirrors ValueRole in primitive.
var RoleEnum = map[string]uint64{
	"NONE":        0,
	"None":        0,
	"PROGRAMMER":  1,
	"Programmer":  1,
	"LEARNER":     2,
	"Learner":     2,
	"READOUT":     3,
	"Readout":     3,
	"ASSOCIATION": 4,
	"Association": 4,
	"PROMPT":      5,
	"Prompt":      5,
}

/*
ConstantInit pairs a frame word offset with the literal value the
compiler reserved there. The host must stage these into the asset
region before dispatch — without this the predicate primitive reads
its threshold as zero.
*/
type ConstantInit struct {
	Offset uint64
	Value  uint64
}

/*
Compiled is the artifact of a single program lowering: 16 packed ALU
words, the constants that must be staged in the asset region, and the
canonical MaskTrue word offset.
*/
type Compiled struct {
	Words        [16]uint64
	Constants    []ConstantInit
	MaskTrueWord uint64
}

// Builder tracks emitted instructions and constant allocations.
type Builder struct {
	instructions [16]uint64
	pc           int
	assetOffset  uint64
	constants    []ConstantInit
	// overflow records the count of instructions the program would
	// have emitted past the 16-word budget. Compile surfaces it as a
	// real error so config init can skip oversized programs without
	// the runtime crashing.
	overflow int

	currentMask     Region
	currentTopo     uint64
	currentTarget   uint64 // 0 for A, 1 for B
	srcAFromB       uint64 // 1 = read SrcA from popped B (target=B blocks)
	emitFlag        uint64
	predicateEnable uint64
	predCond        uint64
	bRotate         uint64 // truth-table instructions: rotate SrcB span by N bytes before the op
	stageFlag       uint64 // 1 = push currentB into backend.staging[A.properties.reference]
	popEndFlag      uint64 // 1 = last instruction of a pop(B) body; kernel rewinds to body start if more Bs remain
}

func NewBuilder() *Builder {
	return &Builder{
		currentMask: MaskTrue,
		assetOffset: assetConstStart,
	}
}

/*
Pack writes one 64-bit instruction with the bit layout the kernel
decodes. The full set of fields — predicate, srcAFromB, emit, topology,
target — is composed from the Builder's transient state so callers
don't need to thread every option through Pack.
*/
func (builder *Builder) Pack(op uint64, a, bReg, dst Region) {
	if builder.pc >= 16 {
		builder.overflow++
		return
	}

	var instr uint64
	instr |= op & 0xF
	instr |= (a.Start & 0x7F) << 4
	instr |= ((a.Span - 1) & 0x7F) << 11
	instr |= (bReg.Start & 0x7F) << 18
	instr |= ((bReg.Span - 1) & 0x7F) << 25
	instr |= (dst.Start & 0x7F) << 32
	instr |= ((dst.Span - 1) & 0x7F) << 39
	instr |= (builder.currentMask.Start & 0x7F) << 46
	instr |= (builder.currentTarget & 1) << 53
	instr |= (builder.emitFlag & 1) << 54
	instr |= (builder.currentTopo & 3) << 55
	instr |= (builder.predicateEnable & 1) << 57
	instr |= (builder.bRotate & 7) << 58
	if builder.predicateEnable == 1 {
		instr &^= uint64(7) << 58
		instr |= (builder.predCond & 7) << 58
	}
	instr |= (builder.srcAFromB & 1) << 61
	instr |= (builder.stageFlag & 1) << 62
	instr |= (builder.popEndFlag & 1) << 63

	builder.instructions[builder.pc] = instr
	builder.pc++
}

/*
Predicate emits a single popcount-driven instruction. cond selects the
comparison/reduction; threshold is the 1-word comparand region (ignored
when cond is StorePopcnt or AnyZero); dst is where the result lands.
*/
func (builder *Builder) Predicate(cond uint64, region, threshold, dst Region, srcAFromB uint64) {
	prevPred := builder.predicateEnable
	prevCond := builder.predCond
	prevSrcA := builder.srcAFromB

	builder.predicateEnable = 1
	builder.predCond = cond
	builder.srcAFromB = srcAFromB

	builder.Pack(OpFalse, region, threshold, dst)

	builder.predicateEnable = prevPred
	builder.predCond = prevCond
	builder.srcAFromB = prevSrcA
}

/*
Reduce emits a lane-level categorical reducer. SrcA is the per-peer value
region, SrcB is the per-peer key region, dst is the owner output, and match is
an owner-side key used by reducers that need an equality filter.
*/
func (builder *Builder) Reduce(op uint64, value, key, match, dst Region) {
	prevPred := builder.predicateEnable
	prevCond := builder.predCond
	prevMask := builder.currentMask
	prevTopo := builder.currentTopo
	prevSrcA := builder.srcAFromB

	builder.predicateEnable = 1
	builder.predCond = PredEQ
	builder.currentMask = match
	builder.currentTopo = TopoHypercube
	builder.srcAFromB = 1

	builder.Pack(op, value, key, dst)

	builder.predicateEnable = prevPred
	builder.predCond = prevCond
	builder.currentMask = prevMask
	builder.currentTopo = prevTopo
	builder.srcAFromB = prevSrcA
}

/*
AllocConstant reserves a 1-word slot in the asset region for a literal
and records the (offset, value) pair so the host can stage it before
the kernel runs.
*/
func (builder *Builder) AllocConstant(val uint64) Region {
	reg := Region{Start: builder.assetOffset, Span: 1}

	builder.constants = append(builder.constants, ConstantInit{
		Offset: builder.assetOffset,
		Value:  val,
	})

	builder.assetOffset++

	return reg
}

/*
AllocScratch reserves transient asset words for compiler-lowered expressions.
The host does not initialize them; each sweep writes the scratch span before
any predicate reads it.
*/
func (builder *Builder) AllocScratch(span uint64) Region {
	if span == 0 {
		span = 1
	}

	reg := Region{Start: builder.assetOffset, Span: span}
	builder.assetOffset += span

	return reg
}

// Compile returns the raw 16-word binary program.
func (builder *Builder) Compile() [16]uint64 {
	return builder.instructions
}

// Overflow returns how many instructions the lowering would have
// produced past the 16-word budget. Zero means the program fits.
func (builder *Builder) Overflow() int {
	return builder.overflow
}

// Constants returns the staged literal initializers as a fresh slice.
func (builder *Builder) Constants() []ConstantInit {
	out := make([]ConstantInit, len(builder.constants))
	copy(out, builder.constants)

	return out
}
