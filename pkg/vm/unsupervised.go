package vm

import (
	"math"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/programmer"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
crystallizationFloor is the coverage score below which a community field
triggers the unsupervised labeling pass. Fields above this threshold have
enough labeled Values that the pass would produce diminishing returns.
*/
const crystallizationFloor = 0.35

/*
minZeroRunBits is the minimum zero-run length (in bits) for a zero-run to be
counted as signal. Short runs are noise from incidental bit matches rather
than genuine shared structure.
*/
const minZeroRunBits = 16

/*
labelQuorum is the minimum number of Value pairs that must agree on the same
label candidate for it to be applied to unlabeled Values. This prevents
single-pair noise from polluting the label vocabulary.
*/
const labelQuorum = 2

/*
Crystallization captures the structural-identity metrics of a community field
derived from its live Value population's label words (Properties word 48).

Coverage is the fraction of Values with at least one non-zero label slot.
Consensus measures label agreement across the field: 1 = total agreement,
0 = maximum entropy.
LabelDensity is the mean fraction of the four available slots that are filled.
Score is the composite metric used by the global trigger: Coverage × Consensus × LabelDensity.
*/
type Crystallization struct {
	Coverage     float64
	Consensus    float64
	LabelDensity float64
	Score        float64
}

/*
measureCrystallization computes the Crystallization of a community field from
its live Value population. Returns a zero-value Crystallization when the field
has no Values.
*/
func measureCrystallization(community *geometry.Field) Crystallization {
	if community == nil || len(community.Values) == 0 {
		return Crystallization{}
	}

	propertiesStart := 48
	total := 0
	labeled := 0
	slotSum := 0

	labelCounts := make(map[uint16]int)

	for _, value := range community.Values {
		if value == nil {
			continue
		}

		total++

		slots := kernel.UnpackClassificationLabelSlots((*value)[propertiesStart])
		nonZero := 0

		for _, slot := range slots {
			if slot != 0 {
				nonZero++
				labelCounts[slot]++
			}
		}

		if nonZero > 0 {
			labeled++
		}

		slotSum += nonZero
	}

	if total == 0 {
		return Crystallization{}
	}

	coverage := float64(labeled) / float64(total)
	density := float64(slotSum) / float64(total*4)

	entropy := 0.0
	totalVotes := 0

	for _, count := range labelCounts {
		totalVotes += count
	}

	for _, count := range labelCounts {
		p := float64(count) / float64(totalVotes)
		if p > 0 {
			entropy -= p * math.Log2(p)
		}
	}

	consensus := 1.0

	if len(labelCounts) > 1 {
		maxEntropy := math.Log2(float64(len(labelCounts)))
		if maxEntropy > 0 {
			consensus = 1.0 - (entropy / maxEntropy)
		}
	}

	score := coverage * consensus * density

	return Crystallization{
		Coverage:     coverage,
		Consensus:    consensus,
		LabelDensity: density,
		Score:        score,
	}
}

/*
Unsupervised runs one round of unsupervised soft-labeling across the
communities in a root Field whenever their crystallization drops below the
configured floor.

The pipeline for each under-crystallized community:

 1. Collect the subset of Values that carry no soft labels yet (slots 1–3 of
    Properties word 48 all zero). Only this set — call it U — enters the
    comparison loop. Values that already received a soft label in a previous
    pass are excluded: they voted in their pass and their label is stable.
 2. XOR each pair in U using the "unsupervised_learn" program (peer tokens
    staged into Reserved before ALU dispatch). ScanZeroRun on the resulting
    Signals finds the longest shared-structure run; RunLabel maps it to a
    deterministic 16-bit candidate.
 3. Candidates are voted on across all pairs in U. The winning candidate (if
    it meets quorum) is injected into slot 1 of each Value in U.

Complexity: O(U²) per community per cycle, where U is the count of
soft-unlabeled Values. After each pass U empties; on the next cycle U equals
only the newly arrived Values (one ingest batch, typically a handful of
segments). Work stays proportional to batch size, not community size — this
is the convergence property that prevents the labeling pass from growing
unbounded as the community accumulates Values.
*/
type Unsupervised struct {
	queue *pool.Queue
}

/*
NewUnsupervised creates an Unsupervised worker bound to the given Queue.
*/
func NewUnsupervised(queue *pool.Queue) *Unsupervised {
	return &Unsupervised{queue: queue}
}

/*
Cycle iterates every community in the root field and runs a labeling pass on
communities whose coverage is below crystallizationFloor.

The inner fast-path check (softUnlabeledCount) avoids the heavier
measureCrystallization scan on communities where every Value already carries
at least one soft label — the common case after the community has been
processed once.
*/
func (unsupervised *Unsupervised) Cycle(root *geometry.Field) {
	if root == nil {
		return
	}

	for _, community := range root.Fields {
		if community == nil || len(community.Values) < 2 {
			continue
		}

		if softUnlabeledCount(community) < 2 {
			continue
		}

		cr := measureCrystallization(community)
		if cr.Coverage >= crystallizationFloor {
			continue
		}

		unsupervised.labelCommunity(community)
	}
}

/*
softUnlabeledCount returns the number of Values in the community that carry no
soft labels yet (Properties word 48, bits 16–63 all zero). Used as a cheap
early-exit gate before the heavier measureCrystallization scan.
*/
func softUnlabeledCount(community *geometry.Field) int {
	count := 0

	for _, value := range community.Values {
		if value != nil && (*value)[48]>>16 == 0 {
			count++
		}
	}

	return count
}

/*
labelCommunity runs the XOR-vote-inject pipeline on one community field.

Only Values with no soft labels (Properties word 48, bits 16–63 all zero)
enter the comparison and injection loops. This keeps complexity at O(U²)
where U is the current unlabeled count rather than O(N²) over the entire
community. Because each pass labels the Values in U, U resets to only the
newest arrivals on the following cycle.
*/
func (unsupervised *Unsupervised) labelCommunity(community *geometry.Field) {
	unlabeled := make([]*primitive.Value, 0, len(community.Values))

	for _, value := range community.Values {
		if value != nil && (*value)[48]>>16 == 0 {
			unlabeled = append(unlabeled, value)
		}
	}

	if len(unlabeled) < 2 {
		return
	}

	votes := make(map[uint16]int)

	for idx := 0; idx < len(unlabeled)-1; idx++ {
		for jdx := idx + 1; jdx < len(unlabeled); jdx++ {
			label, ok := unsupervised.compareTokens(unlabeled[idx], unlabeled[jdx])
			if ok {
				votes[label]++
			}
		}
	}

	winner, winnerVotes := uint16(0), 0

	for label, count := range votes {
		if count > winnerVotes {
			winnerVotes = count
			winner = label
		}
	}

	if winnerVotes < labelQuorum || winner == 0 {
		return
	}

	for _, value := range unlabeled {
		unsupervised.injectLabel(value, winner)
	}
}

/*
compareTokens stages peer A and peer B's token regions (words 0–15, the full
16-word token span) into a worker Value's Reserved region before dispatch:

	reserved[0,16]  ← peerA tokens[0,15]  (absolute words 56–71)
	reserved[16,16] ← peerB tokens[0,15]  (absolute words 72–87)

The "unsupervised_learn" program then runs the XOR LSH sweep across those two
16-word spans and accumulates the 64-byte signature into signals[0,8].
ScanZeroRun on those 8 signal words finds the longest zero-run, which is the
fingerprint of the largest shared component between the two Values.

Returns the 16-bit label candidate and true when the run meets minZeroRunBits.
*/
func (unsupervised *Unsupervised) compareTokens(peerA, peerB *primitive.Value) (uint16, bool) {
	const (
		tokenStart    = 0
		tokenWords    = 16
		reservedStart = 56
	)

	worker := new(primitive.Value)

	for idx := 0; idx < tokenWords; idx++ {
		(*worker)[reservedStart+idx] = (*peerA)[tokenStart+idx]
		(*worker)[reservedStart+tokenWords+idx] = (*peerB)[tokenStart+idx]
	}

	installer := programmer.Installer{}

	if err := installer.InstallProgram(worker, "unsupervised_learn"); err != nil {
		return 0, false
	}

	unsupervised.queue.ExecuteInline( //nolint:errcheck
		[]unsafe.Pointer{unsafe.Pointer(worker)},
	)

	signalsStart := 24

	signals := make([]uint64, 8)
	for idx := range signals {
		signals[idx] = (*worker)[signalsStart+idx]
	}

	start, length := geometry.ScanZeroRun(signals)

	if length < minZeroRunBits {
		return 0, false
	}

	return geometry.RunLabel(start, length), true
}

/*
injectLabel writes a soft label into the first available slot (1–3) of
Properties word 48. Slot 0 is reserved for dataset-provided labels.
Values where all three soft-label slots are already filled are skipped.

Why Go-side and not firmware: the ALU kernel (universalBitwiseV2) is a
16-rotation LSH sweep engine. It computes a byte-level signature from two
operand spans and XOR-accumulates or reduces that signature into a dst span.
This is not equivalent to a clean 64-bit bitwise OR of two full words — the
byte-level lane packing and rotation cycling mean the result in dst is an LSH
artifact, not the expected label value.

Injecting a pre-shifted 16-bit label into a specific bit-lane of a 64-bit
word requires a clean word-level OR, which would need an ALU extension beyond
the current rotation-sweep contract. Until that extension is added, the
orchestrator performs the write directly — this is a deliberate, documented
exception to the firmware-first principle, not an oversight.
*/
func (unsupervised *Unsupervised) injectLabel(value *primitive.Value, label uint16) {
	const propertiesStart = 48

	word := (*value)[propertiesStart]
	slots := kernel.UnpackClassificationLabelSlots(word)

	for slotIdx := 1; slotIdx <= 3; slotIdx++ {
		if slots[slotIdx] != 0 {
			continue
		}

		(*value)[propertiesStart] = word | (uint64(label) << (slotIdx * 16))

		return
	}
}
