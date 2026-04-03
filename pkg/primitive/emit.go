package primitive

import (
	"math/rand"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/compute/firmware"
	"github.com/theapemachine/six/pkg/core"
)

/*
minSignalBits is the minimum signal length (in bits) required before the
substrate will emit child Values. Below this threshold the agreement is
indistinguishable from noise and emitting would flood the substrate with
junk frames.
*/
const minSignalBits = 32

/*
EmitFromSignals implements the substrate-level emission rules. It scans
signals between two Values, picks the strongest (longest run across all
strides and operations), and emits child Values according to signal kind:

  - Cancel (XOR longest zero-run): emit shared component, left residue,
    right residue — three children linked into a chain.
  - Merge (AND longest one-run): emit a single consolidation frame.

Children inherit Learn firmware and receive HIE-blended programs from
their parents so evolutionary progress is not lost.

These are the RULES of the substrate. The BEHAVIOR — which bitwise
operations produce useful signals — is for the evolved programs to
discover.
*/
func EmitFromSignals(a, b *Value, rng *rand.Rand) []*Value {
	if a == nil || b == nil {
		return nil
	}

	tokenBits := int(core.Cfg.Value.Region.Tokens.Bits)
	tokenWords := int((tokenBits + 63) / 64)
	baseIdx := core.Cfg.Value.Region.Tokens.Start

	if tokenWords <= 0 || tokenBits <= 0 {
		return nil
	}

	signals := ScanSignals(a, b, tokenWords, baseIdx)
	if len(signals) == 0 {
		return nil
	}

	// Strongest signal = first (already sorted by length descending).
	winner := signals[0]

	if winner.Length < minSignalBits {
		return nil
	}

	switch winner.Kind {
	case SignalCancel:
		return emitCancel(a, b, winner, tokenWords, baseIdx, tokenBits, rng)
	case SignalMerge:
		return emitMerge(a, b, winner, tokenWords, baseIdx, tokenBits, rng)
	default:
		return nil
	}
}

/*
emitCancel creates up to three child Values from a cancel signal:

  - Shared: the bit-span where A and B agree (XOR zero-run).
  - Left residue: A's token region with the shared span zeroed out.
  - Right residue: B's token region with the shared span zeroed out.

Children are chained via NextID: shared → left → right.
All three get Prev-linked to parent A so the graph is traceable.
*/
func emitCancel(a, b *Value, sig Signal, tokenWords, baseIdx, tokenBits int, rng *rand.Rand) []*Value {
	aTokens := make([]uint64, tokenWords)
	bTokens := make([]uint64, tokenWords)
	copy(aTokens, a[baseIdx:baseIdx+tokenWords])
	copy(bTokens, b[baseIdx:baseIdx+tokenWords])

	// Shared component: bits from A at the signal span (= B at that span).
	sharedBits := ExtractSpanForSignal(aTokens, sig, tokenBits)

	// Left residue: A with the shared span cleared.
	leftBits := make([]uint64, tokenWords)
	copy(leftBits, aTokens)
	clearSignalSpan(leftBits, sig, tokenBits)

	// Right residue: B with the shared span cleared.
	rightBits := make([]uint64, tokenWords)
	copy(rightBits, bTokens)
	clearSignalSpan(rightBits, sig, tokenBits)

	children := make([]*Value, 0, 3)

	if shared := newChildValue(sharedBits, tokenWords, baseIdx); shared != nil {
		children = append(children, shared)
	}
	if left := newChildValue(leftBits, tokenWords, baseIdx); left != nil {
		children = append(children, left)
	}
	if right := newChildValue(rightBits, tokenWords, baseIdx); right != nil {
		children = append(children, right)
	}

	if len(children) == 0 {
		return nil
	}

	linkAndBlend(a, b, children, rng)
	return children
}

/*
emitMerge creates a single consolidation Value from a merge signal: the
bit-span where both A and B have dense agreement (AND one-run).
*/
func emitMerge(a, b *Value, sig Signal, tokenWords, baseIdx, tokenBits int, rng *rand.Rand) []*Value {
	aTokens := make([]uint64, tokenWords)
	bTokens := make([]uint64, tokenWords)
	copy(aTokens, a[baseIdx:baseIdx+tokenWords])
	copy(bTokens, b[baseIdx:baseIdx+tokenWords])

	// AND the two token regions, then extract the winning span.
	andWords := make([]uint64, tokenWords)
	for i := 0; i < tokenWords; i++ {
		andWords[i] = aTokens[i] & bTokens[i]
	}
	mergeBits := ExtractSpanForSignal(andWords, sig, tokenBits)

	consolidation := newChildValue(mergeBits, tokenWords, baseIdx)
	if consolidation == nil {
		return nil
	}

	children := []*Value{consolidation}
	linkAndBlend(a, b, children, rng)
	return children
}

/*
newChildValue creates a fresh Value from the pool, mints a unique ID,
copies the provided token bits into the token region, installs Learn
firmware, and inserts introns into the program region. Returns nil if
the token bits are entirely zero (nothing to represent).
*/
func newChildValue(tokenBits []uint64, tokenWords, baseIdx int) *Value {
	// Don't emit empty children.
	allZero := true
	for _, w := range tokenBits {
		if w != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return nil
	}

	value := valuePool.Get().(*Value)

	// Zero the entire frame first.
	for i := range value {
		value[i] = 0
	}

	// Mint a fresh ValueID.
	value[core.Cfg.Value.Region.ID.Start] = atomic.AddUint64(
		&globalValueIDCounter, 1,
	)

	// Copy token bits into the token region. The extracted bits may be
	// shorter than the full region (ExtractSpan packs from bit 0); pad
	// with zeros is implicit from the zeroed frame.
	limit := tokenWords
	if len(tokenBits) < limit {
		limit = len(tokenBits)
	}
	for i := 0; i < limit; i++ {
		idx := baseIdx + i
		if idx < len(*value) {
			value[idx] = tokenBits[i]
		}
	}

	// Compute affinity from the token region (SimHash LSH).
	value.ComputeAffinityLSH()

	// Install Learn firmware so the child participates in evolution.
	clearProgramWords(value)
	if err := value.InstallFirmware(core.FirmwareTypeLearn); err != nil {
		// On firmware failure, return the value to the pool and skip.
		valuePool.Put(value)
		return nil
	}
	firmware.InsertIntrons((*[128]uint64)(value), 8)

	// Set fw register so handleFollowUp will re-queue this child.
	value[core.Cfg.Value.Region.Registers.FW] = core.FirmwareRegisterLearn

	return value
}

/*
linkAndBlend wires parent links and NextID chains on children, then
HIE-blends the parents' programs into each child so evolutionary
progress carries forward.
*/
func linkAndBlend(a, b *Value, children []*Value, rng *rand.Rand) {
	idWord := core.Cfg.Value.Region.ID.Start
	aID := a[idWord]

	// All children get Prev-linked to parent A.
	for _, child := range children {
		child[core.Cfg.Value.Region.Prev.Start] = aID
	}

	// Chain children via NextID.
	for i := 0; i < len(children)-1; i++ {
		children[i][core.Cfg.Value.Region.Next.Start] = children[i+1][idWord]
	}

	// HIE-blend programs from parents into children.
	for _, child := range children {
		childVal := Value(*child)
		parentBias := SubstrateExploitScore(a, &childVal)
		firmware.HolographicCrossover(
			(*[128]uint64)(child),
			(*[128]uint64)(a),
			(*[128]uint64)(b),
			rng,
			parentBias,
		)
	}
}

/*
clearSignalSpan zeroes the bits described by a Signal within a token word
slice. Handles both linear and affine-stride signals.
*/
func clearSignalSpan(words []uint64, sig Signal, tokenBits int) {
	for i := 0; i < sig.Length; i++ {
		var phys int
		if sig.ScanStride == 0 {
			phys = sig.StartBit + i
		} else {
			step := sig.StartBit + i
			phys = int((sig.ScanStride*uint64(step))%signalAffineRing) % tokenBits
		}

		w := phys / 64
		b := uint(phys % 64)
		if w < len(words) {
			words[w] &^= 1 << b
		}
	}
}
