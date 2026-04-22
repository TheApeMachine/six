package compute

import (
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Post-ALU hook for the README "Signals" algorithm.

Every Value executes fold_substrate on its first ALU pass (see config.yml
and pkg/primitive/value.go's default install). That program fills two
disjoint signal halves on the same frame:

	signals[0,4]  XOR( tokens[0,8] , tokens[8,8] )   ← cancel sweep
	signals[4,4]  AND( tokens[0,8] , tokens[8,8] )   ← merge sweep

The longest zero-run in the cancel half is the cancel decision (the
README's "is in the"). The longest one-run in the merge half is the
merge decision (the README's "[Kitchen]"). The substrate decides what
is structural purely by where the runs land and how long they are; no
Go-side magnitude gate.

After the hook emits the cancel and merge Associations, the source's
program is swapped from fold_substrate to unsupervised_learn so the
value transitions from "structural extraction" into ongoing learning.
The post-exec hook itself is single-shot per source: re-entry is
already gated by Epoch != 0 (lifecycle.evaluateResult bumps epoch
before the next dispatch), and Associations skip the hook by role.
*/

// Emitter publishes a freshly-minted Value back into the substrate's
// routing layer. The orchestrator implements this by writing the wire
// frame into its root mesh.Field, which routes the Association into the
// community whose affinity matches the surviving bit-pattern.
//
// Implementations take ownership of the passed Value: they must call
// primitive.FreeValue (directly or transitively via Field.Write copying
// the bytes) before returning. The hook does not retain a reference.
type Emitter interface {
	Emit(*primitive.Value) error
}

// signalsHalves returns the two 4-word slices used by fold_substrate:
// the cancel half (signals[0,4], 256 bits, scanned for the longest zero
// run) and the merge half (signals[4,4], 256 bits, scanned for the
// longest one run).
func signalsHalves(value *primitive.Value) (cancelHalf, mergeHalf []uint64) {
	if value == nil {
		return nil, nil
	}

	start, words := core.Cfg.Value.Region.Signals.WordExtent()
	if words < 8 {
		return nil, nil
	}

	frame := (*[128]uint64)(value)[:]

	cancelHalf = frame[start : start+4]
	mergeHalf = frame[start+4 : start+8]

	return cancelHalf, mergeHalf
}

// emitAssociation builds an Association Value whose token region carries
// the surviving bit-pattern from `source` at the [startBit, startBit+length)
// span, and whose Prev links the source so the recall path can walk back.
// The label slot in properties[0] is stamped with geometry.RunLabel of
// the run coordinates so two associations born at the same structural
// offset hash to the same 16-bit label — that is what lets later
// encounters route through the same chain.
func emitAssociation(source *primitive.Value, startBit, length int) *primitive.Value {
	if source == nil || length <= 0 {
		return nil
	}

	tokenStart, tokenWords := core.Cfg.Value.Region.Tokens.WordExtent()
	if tokenWords <= 0 {
		return nil
	}

	srcFrame := (*[128]uint64)(source)[:]
	srcTokens := srcFrame[tokenStart : tokenStart+tokenWords]

	// Carve the surviving bit-region out of the source tokens. The
	// extracted span is what cancelled (or merged), so it is the
	// structural payload of the new Association.
	carved := carveBits(srcTokens, startBit, length, tokenWords)

	// Propagate the source Value's class label (LABELS slot) into the
	// Association so a downstream recall walk that lands on this hop
	// has a class index ready to copy into a Readout. Sources without
	// a label (zero) leave it zero on the Association too — the gate
	// in classLabelStringForExperiment rejects label==0 explicitly.
	srcLabel, _ := source.Property(primitive.LABELS)

	// The Geometry-derived RunLabel hash carries the structural
	// fingerprint (start/length of the surviving run); we stash it in
	// REFERENCE so two associations born at the same offset can be
	// compared without colliding with the class-label slot.
	runHash := uint64(geometry.RunLabel(startBit, length))

	assoc := primitive.Emit(
		primitive.WithRole(uint64(primitive.ValueRoleAssociation)),
		primitive.WithStatus(uint64(primitive.READY)),
		primitive.WithLabels(srcLabel),
		primitive.WithFirmware(core.AFFINITY),
	)

	if assoc == nil {
		return nil
	}

	dstFrame := (*[128]uint64)(assoc)[:]
	for idx := 0; idx < tokenWords && idx < len(carved); idx++ {
		dstFrame[tokenStart+idx] = carved[idx]
	}

	prevStart := core.Cfg.Value.Region.Prev.Start
	dstFrame[prevStart] = source.ID()

	// Stamp the structural fingerprint into REFERENCE (NOT the class
	// label slot) so the recall path can bucket associations by run
	// coordinates without overwriting a real label.
	assoc.SetProperty(primitive.REFERENCE, runHash)

	return assoc
}

// carveBits copies a [startBit, startBit+length) slice out of `src` into
// a fresh slab of the same width as `src`. Bits outside the carve are
// zero. The result keeps the run sitting at its original coordinates so
// affinity routing of the new Value naturally targets the same community
// the original substring lived in (its Morton-coded bytes occupy the
// same slot positions and therefore fold into the same affinity bins).
func carveBits(src []uint64, startBit, length, dstWords int) []uint64 {
	out := make([]uint64, dstWords)
	if startBit < 0 || length <= 0 {
		return out
	}

	endBit := startBit + length
	maxBit := dstWords * 64
	if endBit > maxBit {
		endBit = maxBit
	}

	for bit := startBit; bit < endBit; bit++ {
		srcWord := bit >> 6
		if srcWord >= len(src) {
			break
		}

		bitInWord := uint(bit & 63)
		if (src[srcWord]>>bitInWord)&1 != 0 {
			out[srcWord] |= 1 << bitInWord
		}
	}

	return out
}

// runStructuralPostExec runs after every kernel dispatch. It looks at the
// signal halves the fold_substrate program just wrote, scans the longest
// runs, and emits at most one cancel Association and one merge Association
// per source Value's first dispatch.
//
// Returns true when an emission happened so the backend can record the
// fact (telemetry / counters) without us reaching back through it here.
func runStructuralPostExec(value *primitive.Value, emitter Emitter) bool {
	if value == nil || emitter == nil {
		return false
	}

	// Skip Associations themselves so the cascade is bounded: an Association
	// landing in the substrate folds its own halves and would keep emitting
	// sub-associations forever otherwise.
	if value.Role() == primitive.ValueRoleAssociation {
		return false
	}

	// Fire only on first dispatch so a Value being recycled by `next self`
	// or recall passes does not keep re-emitting from the same folded
	// signature.
	if value.Epoch() != 0 {
		return false
	}

	cancelHalf, mergeHalf := signalsHalves(value)
	if cancelHalf == nil || mergeHalf == nil {
		return false
	}

	emitted := false

	zeroStart, zeroLen := geometry.ScanZeroRun(cancelHalf)
	if assoc := emitAssociation(value, zeroStart, zeroLen); assoc != nil {
		if err := emitter.Emit(assoc); err != nil {
			primitive.FreeValue(assoc)
		} else {
			emitted = true
		}
	}

	oneStart, oneLen := geometry.ScanOneRun(mergeHalf)
	if assoc := emitAssociation(value, oneStart, oneLen); assoc != nil {
		// Merge linkage is reversed: the surviving bit-pattern is
		// a hub; its Next points back at the source so successive
		// encounters at this hub can be discovered by walking
		// Next from the Association.
		dstFrame := (*[128]uint64)(assoc)[:]
		nextStart := core.Cfg.Value.Region.Next.Start
		prevStart := core.Cfg.Value.Region.Prev.Start
		dstFrame[nextStart] = value.ID()
		dstFrame[prevStart] = 0

		if err := emitter.Emit(assoc); err != nil {
			primitive.FreeValue(assoc)
		} else {
			emitted = true
		}
	}

	// Structural extraction is single-shot per source: swap the program
	// from fold_substrate to unsupervised_learn so the next dispatch
	// does ongoing learning instead of re-running the same cancel/merge
	// sweep. WriteProgramWords zeroes the tail so the new (shorter)
	// instruction stream halts cleanly without leftover sweeps firing.
	value.InstallFirmware(core.UNSUPERVISED_LEARN)

	return emitted
}
