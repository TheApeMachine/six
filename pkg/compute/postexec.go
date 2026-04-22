package compute

import (
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Post-ALU hooks for the README "Signals" algorithm and unsupervised
label propagation.

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
program is swapped from fold_substrate to vote_swarm so the value
keeps cycling through the dispatch loop (`next self`) while gossip
stages new peers into its asset region on every pass. The post-exec
structural hook itself is single-shot per source: re-entry is gated
by Epoch != 0 (lifecycle.evaluateResult bumps epoch before the next
dispatch), and Associations skip the hook by role.

While vote_swarm is resident, runLabelPropagation runs after every
dispatch. It is the in-band lifecycle hook that decides "the gossip
substrate has placed a labeled peer next to this value; commit the
peer's class label and surface this value to the prompt resolver."
The decision is structurally simple — peer is labeled, host is not —
because affinity routing has already done the unsupervised clustering
work upstream: a peer only ends up in the host's asset region because
the field's routing layer placed them in adjacent communities. No
similarity computation here would add information the routing layer
did not already encode.

The two hooks split responsibilities cleanly:

  - runStructuralPostExec is a one-shot emission hook: turns folded
    signal halves into Association Values and rewires the source's
    program for the ongoing-learning phase.
  - runLabelPropagation is a per-dispatch state-transition hook: copies
    LABELS across gossip-staged neighbors and stamps ROLE=Readout +
    STATUS=RESOLVED so the orchestrator surfaces resolved Prompts to
    the pipeline scorer.
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
	// from fold_substrate to vote_swarm so the next dispatch keeps the
	// value resident in the kernel loop (`next self`) while gossip
	// stages new peers into asset[] on each pass. The label-propagation
	// hook is what eventually terminates this loop by stamping
	// ROLE=Readout + STATUS=RESOLVED once a labeled peer arrives.
	// WriteProgramWords zeroes the tail so the new (shorter) instruction
	// stream halts cleanly without leftover sweeps firing.
	value.InstallFirmware(core.VOTE_SWARM)

	return emitted
}

// runLabelPropagation is the per-dispatch lifecycle hook that copies
// a class label from a gossip-staged peer into the host Value's LABELS
// slot and, when the host is a Prompt, marks it as a Readout the
// orchestrator should return to the pipeline scorer.
//
// Layout note: Value.Write stages the visiting peer's
// signals+context+gradient+properties block (40 words starting at the
// peer's signalsStart) into the host's asset region (also 40 words,
// starting at the host's assetStart). The peer's properties begin
// (propertiesStart - signalsStart) words into that staged block, so
// peer.LABELS lives at host word
//
//	assetStart + (propertiesStart - signalsStart) + LABELS_index
//
// Reading from there is just the kernel-side primitive of "look at the
// freshly staged peer state without disturbing the host's own working
// state" — exactly the asset-region contract Value.Write was built
// around.
//
// Decisions:
//
//   - peer.LABELS == 0 OR host.LABELS != 0 → no-op. The peer either
//     has no class label to share, or the host already committed one
//     and we must not overwrite it (would corrupt scoring on values
//     that already converged).
//   - host.LABELS becomes peer.LABELS otherwise. The label space is
//     1-indexed (huggingface dataset stamps LabelInt+1, see
//     experiment/data/huggingface/dataset.go), which matches the
//     scorer in experiment/task/pipeline.go that interprets
//     LABELS-1 as the class names index.
//   - When the host carries ROLE=Prompt and now has a non-zero label,
//     stamp ROLE=Readout + STATUS=RESOLVED + RequestEmit. That is the
//     only path in the system that promotes a Prompt to a Readout;
//     without it Prompts cycle until shouldContinue's epoch cap and
//     never appear in evaluateResult's resolved set.
//   - Associations are skipped: their LABELS slot is propagated by
//     emitAssociation as part of the structural cascade, and they have
//     their own role-based in-band-return path through the lifecycle.
func runLabelPropagation(value *primitive.Value) {
	if value == nil {
		return
	}

	if value.Role() == primitive.ValueRoleAssociation {
		return
	}

	signalsStart := core.Cfg.Value.Region.Signals.Start
	propertiesStart := core.Cfg.Value.Region.Properties.Start
	assetStart := core.Cfg.Value.Region.Asset.Start

	peerLabelWord := assetStart + (propertiesStart - signalsStart) + int(primitive.LABELS)

	frame := (*[128]uint64)(value)[:]
	if peerLabelWord < 0 || peerLabelWord >= len(frame) {
		return
	}

	peerLabel := frame[peerLabelWord]
	hostLabel, _ := value.Property(primitive.LABELS)

	if hostLabel == 0 && peerLabel != 0 {
		value.SetProperty(primitive.LABELS, peerLabel)
		hostLabel = peerLabel
	}

	if hostLabel != 0 && value.Role() == primitive.ValueRolePrompt {
		value.SetProperty(primitive.ROLE, uint64(primitive.ValueRoleReadout))
		value.SetStatus(primitive.RESOLVED)
		value.RequestEmit()
		// vote_swarm uses `next self` to keep the value resident in the
		// priority queue while it waits for a labeled neighbor to land
		// in asset[]. Now that we have a label, kill the continuation
		// so the priority queue stops re-receiving this frame; the
		// lifecycle.evaluateResult clear is a belt-and-braces mirror
		// for any path that bypasses this hook (gap closure, manual
		// emit) but doing it here too keeps the synchronous post-ALU
		// frame consistent — the dispatched copy that lands in the
		// orchestrator queue already has SchedulingNext=0.
		value.SetSchedulingNext(0)
	}
}
