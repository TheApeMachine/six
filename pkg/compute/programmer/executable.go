package programmer

import (
	"fmt"
	"math/bits"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Executable binds compilation to execution: a Compiler plus optional ingress
Values and an optional finalizer that can emit follow-on Values.

Inputs carry the starting Value wire into the run: every emitted Value starts
as a copy of inputs[0] when present, then operand bands, the program region,
and the writeback results overwrite specific word ranges. Additional inputs
are reserved for multi-operand staging.
*/
type Executable struct {
	compiler  *Compiler
	inputs    []*primitive.Value
	finalizer func(*primitive.Value) ([]*primitive.Value, error)
}

func NewExecutable(
	compiler *Compiler,
	finalizer func(*primitive.Value) ([]*primitive.Value, error),
) *Executable {
	return &Executable{compiler: compiler, finalizer: finalizer}
}

/*
Inputs returns the ingress slice (may be nil). Callers own the Values.
*/
func (executable *Executable) Inputs() []*primitive.Value {
	return executable.inputs
}

/*
Execute compiles and materializes one unsafe.Pointer per compiled frame:
each pointer is the base of a full primitive.Value. When inputs[0] is
non-nil, its full wire frame is copied into each output before operand
bands and the program region are filled. Scheduling from the optional
continuation is written on the last emitted Value (word 117).

This path is the low-level materialization used by callers that manage
their own substrate dispatch; use Run to drive stage/execute/writeback
for a full program on a single Value.
*/
func (executable *Executable) Execute(target CompilerTarget) ([]unsafe.Pointer, error) {
	frames, err := executable.compiler.Compile(target)

	if err != nil {
		return nil, err
	}

	tokens := executable.compiler.Tokens()

	if len(tokens) != len(frames) {
		return nil, fmt.Errorf("programmer: token/frame count mismatch: %d tokens, %d frames",
			len(tokens), len(frames))
	}

	out := make([]unsafe.Pointer, 0, len(frames))
	cont := executable.compiler.Continuation()
	bands := newOperandBands()

	for idx := range frames {
		value := executable.valueForFrame()

		bands.stage(value, tokens[idx])
		frames[idx].writeIntoProgramRegion(value)

		if idx == len(frames)-1 && cont != nil {
			cont.ApplyScheduling(value)
		}

		out = append(out, unsafe.Pointer(&(*value)[0]))
	}

	return out, nil
}

/*
Run is the full stage-execute-writeback pipeline: one Value is cloned from
inputs[0] (or minted empty), every frame stages its operand bands, the
substrate runs one pass, and the writeback unpacks signals back into the
frame's dst slice. Successive frames chain because they all mutate the
same Value in place — that is what makes accumulate across lines mean
anything.

The caller supplies a kernel.Substrate; this keeps programmer free of a
compile-time cpu/metal/cuda dependency and matches the interface every
backend already implements.
*/
func (executable *Executable) Run(
	target CompilerTarget,
	substrate kernel.Substrate,
) (*primitive.Value, error) {
	if substrate == nil {
		return nil, fmt.Errorf("programmer: Run requires a substrate")
	}

	frames, err := executable.compiler.Compile(target)

	if err != nil {
		return nil, err
	}

	tokens := executable.compiler.Tokens()

	if len(tokens) != len(frames) {
		return nil, fmt.Errorf("programmer: token/frame count mismatch: %d tokens, %d frames",
			len(tokens), len(frames))
	}

	value := executable.valueForFrame()
	cont := executable.compiler.Continuation()
	bands := newOperandBands()

	ptr := []unsafe.Pointer{unsafe.Pointer(&(*value)[0])}

	for idx := range frames {
		bands.stage(value, tokens[idx])
		frames[idx].writeIntoProgramRegion(value)
		bands.clearSignals(value)

		if err := substrate.Execute(ptr); err != nil {
			return nil, err
		}

		bands.writeback(value, tokens[idx])
	}

	if cont != nil {
		cont.ApplyScheduling(value)
	}

	return value, nil
}

/*
Finalize runs the finalizer on one post-execution Value. When finalizer is
nil, returns a single-element slice containing that Value.
*/
func (executable *Executable) Finalize(out *primitive.Value) ([]*primitive.Value, error) {
	if executable.finalizer == nil {
		return []*primitive.Value{out}, nil
	}

	return executable.finalizer(out)
}

/*
WithInputs attaches Values whose wire frames seed emitted Values before the
compiled program is written. Returns the same Executable for chaining.
*/
func (executable *Executable) WithInputs(inputs []*primitive.Value) *Executable {
	executable.inputs = inputs

	return executable
}

/*
valueForFrame mints a Value wire for one frame: a copy of inputs[0] when
set, otherwise a zero wire.
*/
func (executable *Executable) valueForFrame() *primitive.Value {
	if len(executable.inputs) > 0 && executable.inputs[0] != nil {
		v := *executable.inputs[0]

		return &v
	}

	out := primitive.Value{}

	return &out
}

/*
operandBands maps a Token's RegionRefs into the operand lanes
universalBitwiseV2 reads, and unpacks the signals it writes back into the
dst region. The substrate has fixed operand positions (A at words 0..3, B
rotations at words 32..95, signals at words 24..31) so the DSL's arbitrary
region slices are bridged by a stage/writeback memcpy wrapper rather than
by teaching the assembly new offsets.
*/
type operandBands struct{}

func newOperandBands() *operandBands {
	return &operandBands{}
}

/*
aWordBase is the first word of the substrate's pinned A operand (query).
*/
const aWordBase = 0

/*
aWordCount is the number of words the substrate reads as A.
*/
const aWordCount = 4

/*
bWordBase is the first word of the substrate's 16-rotation B operand.
*/
const bWordBase = 32

/*
bRotationWords is the width of one B rotation; the substrate reads 16 of
these back-to-back starting at bWordBase.
*/
const bRotationWords = 4

/*
stage copies srcA and srcB region slices out of value into the substrate's
pinned A and B operand lanes. Both source slices are snapshotted first so
that writing the A lanes (which always live at words 0..3) cannot clobber
any srcB word that happens to land in the same region (e.g. tokens[2,2]
while srcA is tokens[0,2]). srcA folds via XOR when the slice is wider
than aWordCount; srcB tiles cyclically across the 16 rotations so every
rotation sees a different window into the slice.
*/
func (bands *operandBands) stage(value *primitive.Value, token Token) {
	if value == nil {
		return
	}

	aLanes := bands.readA(value, token.SrcARef)
	bWords := bands.readB(value, token.SrcBRef)

	bands.writeA(value, aLanes)
	bands.writeB(value, bWords, token.SrcBRef.Span)
}

/*
readA reads srcA.Span words from the referenced region and folds them into
the 4 A-lane values. When Span > 4 the overflow words XOR back into the
first 4, so a 16-word fold covers every input bit. When Span < 4 the
unused A lanes stay zero.
*/
func (bands *operandBands) readA(value *primitive.Value, ref RegionRef) [aWordCount]uint64 {
	var lanes [aWordCount]uint64

	base := ref.AbsStart()

	for idx := 0; idx < ref.Span; idx++ {
		lanes[idx%aWordCount] ^= (*value)[base+idx]
	}

	return lanes
}

/*
readB snapshots srcB.Span words into a local slice so downstream writes to
the A lanes or the B rotation band cannot clobber the source data before
the rotations are materialized.
*/
func (bands *operandBands) readB(value *primitive.Value, ref RegionRef) []uint64 {
	span := ref.Span

	if span <= 0 {
		return nil
	}

	base := ref.AbsStart()
	snapshot := make([]uint64, span)

	for idx := 0; idx < span; idx++ {
		snapshot[idx] = (*value)[base+idx]
	}

	return snapshot
}

/*
writeA pins the folded A-lane words into the substrate's query operand
(words 0..3).
*/
func (bands *operandBands) writeA(value *primitive.Value, lanes [aWordCount]uint64) {
	for idx := 0; idx < aWordCount; idx++ {
		value.Set(aWordBase+idx, lanes[idx])
	}
}

/*
writeB fills the 16-rotation B lane (64 words starting at bWordBase) from
the srcB snapshot. Each rotation uses a cyclic 4-word window into the
srcB span so the substrate sweeps genuinely distinct rotations even when
srcB is short. A single-word span is broadcast across all rotations.
*/
func (bands *operandBands) writeB(value *primitive.Value, words []uint64, span int) {
	if span <= 0 || len(words) == 0 {
		return
	}

	numRotations := core.Cfg.Value.NumRotations

	for rotation := 0; rotation < numRotations; rotation++ {
		for lane := 0; lane < bRotationWords; lane++ {
			src := (rotation*bRotationWords + lane) % span
			value.Set(bWordBase+rotation*bRotationWords+lane, words[src])
		}
	}
}

/*
clearSignals zeroes the 8 signal words so each frame's substrate pass
starts from a blank rotation-signature accumulator. The assembly OR-s new
bytes into existing signal words within a single pass; leaving stale bits
between frames would let earlier projections leak into later ones.
*/
func (bands *operandBands) clearSignals(value *primitive.Value) {
	start := core.Cfg.Value.Region.Signals.Start
	words := int((core.Cfg.Value.Region.Signals.Bits + 63) / 64)

	for idx := 0; idx < words; idx++ {
		value.Set(start+idx, 0)
	}
}

/*
writeback unpacks the signals region produced by one substrate pass into
the token's dst region slice. Mode controls the fold: Accumulate XORs
each signal word into the corresponding dst word; Reduce popcounts the
whole signals region (respecting the affinity Fermat tail when dst is
affinity) and writes the total into dst[0]. Reduce leaves the rest of
the dst span untouched so callers can layer reductions without clobbering
previously-accumulated state.
*/
func (bands *operandBands) writeback(value *primitive.Value, token Token) {
	if value == nil {
		return
	}

	dst := token.DstRef
	sigStart := core.Cfg.Value.Region.Signals.Start
	sigWords := int((core.Cfg.Value.Region.Signals.Bits + 63) / 64)

	if token.ModeBit == ModeReduce {
		total := uint64(0)

		for idx := 0; idx < sigWords; idx++ {
			total += uint64(bits.OnesCount64((*value)[sigStart+idx]))
		}

		value.Set(dst.AbsStart(), total)

		return
	}

	base := dst.AbsStart()
	span := dst.Span

	for idx := 0; idx < span && idx < sigWords; idx++ {
		current := (*value)[base+idx]
		value.Set(base+idx, current^(*value)[sigStart+idx])
	}
}
