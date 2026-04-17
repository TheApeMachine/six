package mesh

import (
	"math"
	"math/bits"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
FieldMetrics captures the crystallisation fingerprint of a leaf Field at
the last Cycle tick. It is the observable summary the README talks
about: Coverage × Consensus × LabelDensity gives the single
Crystallization number, while DominantRatio exposes how collapsed the
population is around its top eigenmode — a second, orthogonal measure
of whether the field has settled.

The struct is deliberately flat and numeric so it can be shipped as a
telemetry envelope without reflection gymnastics; the visualizer reads
these fields verbatim.

Zero-valued FieldMetrics (no members yet) leaves every field at zero
except MemberCount which is the ground truth. Downstream consumers
treat score==0 with members>0 as a legitimate "diffuse" state, not an
error.
*/
type FieldMetrics struct {
	MemberCount     int     // how many Values live in this field right now
	LabeledCount    int     // members with ≥1 non-zero label slot in properties[0]
	SlotSum         int     // total non-zero label slots across the population
	Coverage        float64 // LabeledCount / MemberCount
	Consensus       float64 // 1 − normalised Shannon entropy of label distribution
	LabelDensity    float64 // SlotSum / (MemberCount × 4)
	Crystallization float64 // Coverage × Consensus × LabelDensity
	DominantRatio   float64 // dominant eigenmode energy / total energy
	ModeCount       int     // partitioned eigenmode count (≥1 when populated)
	PressureMult    float64 // 1 − DominantRatio; drives carrier emission urgency
	Saturated       bool    // true when Crystallization ≥ crystallizationFloor
}

/*
crystallizationFloor matches README §Field Crystallization — communities
below this Coverage threshold emit labelling pressure. The same floor
is reused for the overall Crystallization saturation flag because the
two semantics degenerate once a field reaches high-consensus coverage.
*/
const crystallizationFloor = 0.35

/*
measureCrystallization scans the member population once and populates
Coverage, Consensus, LabelDensity, and the composed Crystallization
score. It never mutates the member Values — it works from a
snapshotValues copy so iteration is safe if AddValue or Write appends
concurrently.

The inner loop is Go-native rather than SIMD because the population
size per community is typically in the tens, not thousands, and the
work per member is a single word load plus four uint16 extractions. A
vectorised sweep only pays off past ~1 k members per community.
*/
func (field *Field) measureCrystallization() FieldMetrics {
	members := field.snapshotValues()

	metrics := FieldMetrics{MemberCount: len(members)}

	if metrics.MemberCount == 0 {
		return metrics
	}

	propsStart, _ := core.Cfg.Value.Region.Properties.WordExtent()
	labelsWord := propsStart

	// Histogram of distinct non-zero label slot values. Shannon entropy is
	// computed from the histogram at the end; a map keeps the code short
	// without penalising the inner loop noticeably at community scale.
	histogram := make(map[uint16]int, metrics.MemberCount*2)

	for _, value := range members {
		if value == nil {
			continue
		}

		packed := (*value)[labelsWord]
		slots := kernel.UnpackClassificationLabelSlots(packed)

		memberNonZero := 0

		for _, slot := range slots {
			if slot == 0 {
				continue
			}

			histogram[slot]++
			memberNonZero++
		}

		if memberNonZero > 0 {
			metrics.LabeledCount++
			metrics.SlotSum += memberNonZero
		}
	}

	metrics.Coverage = float64(metrics.LabeledCount) / float64(metrics.MemberCount)
	metrics.LabelDensity = float64(metrics.SlotSum) / float64(metrics.MemberCount*4)
	metrics.Consensus = shannonConsensus(histogram)
	metrics.Crystallization = metrics.Coverage * metrics.Consensus * metrics.LabelDensity
	metrics.Saturated = metrics.Crystallization >= crystallizationFloor

	return metrics
}

/*
shannonConsensus returns 1 − H / log2(N) where H is Shannon entropy in
bits over the label histogram and N is the number of distinct observed
labels. The result is 1 when every observed slot carries the same
label (total consensus) and 0 when the labels are uniformly spread.
An empty or single-bucket histogram returns 1 so a newly-seeded field
does not get artificially penalised for lack of diversity.
*/
func shannonConsensus(histogram map[uint16]int) float64 {
	if len(histogram) <= 1 {
		return 1
	}

	var total float64

	for _, count := range histogram {
		total += float64(count)
	}

	if total == 0 {
		return 1
	}

	var entropy float64

	for _, count := range histogram {
		p := float64(count) / total

		if p > 0 {
			entropy -= p * math.Log2(p)
		}
	}

	maxEntropy := math.Log2(float64(len(histogram)))

	if maxEntropy == 0 {
		return 1
	}

	return 1 - entropy/maxEntropy
}

/*
JaccardCouplingAffinity returns the Jaccard similarity of two affinity
fingerprints: popcount(a & b) / popcount(a | b). It is the coupling
function DetectModes consumes to partition members into eigenmodes.

Unrolled across the fixed affinityWords count so the compiler keeps the
comparison in registers. A union popcount of zero (both fingerprints
fully blank) is treated as maximal coupling — two blank values are
trivially indistinguishable and should always share a mode.
*/
func JaccardCouplingAffinity(a, b [affinityWords]uint64) float64 {
	inter := bits.OnesCount64(a[0]&b[0]) +
		bits.OnesCount64(a[1]&b[1]) +
		bits.OnesCount64(a[2]&b[2]) +
		bits.OnesCount64(a[3]&b[3]) +
		bits.OnesCount64(a[4]&b[4])

	union := bits.OnesCount64(a[0]|b[0]) +
		bits.OnesCount64(a[1]|b[1]) +
		bits.OnesCount64(a[2]|b[2]) +
		bits.OnesCount64(a[3]|b[3]) +
		bits.OnesCount64(a[4]|b[4])

	if union == 0 {
		return 1
	}

	return float64(inter) / float64(union)
}

/*
surprisalEnergy returns the single-word surprisal estimate stamped into
properties[0]+1 by the unsupervised/hypothesis programs — it is the
popcount of that word, normalised into [0,1]. Values that have never
been reduced report zero energy so they drop to the bottom of mode
selection without explicit filtering.
*/
func surprisalEnergy(value *primitive.Value) float64 {
	if value == nil {
		return 0
	}

	propsStart, propsWords := core.Cfg.Value.Region.Properties.WordExtent()

	if propsWords < 2 {
		return 0
	}

	// properties[1,1] is the reduced surprisal landing spot (see the
	// `unsupervised_learn` and `hypothesis` programs in config.yml).
	word := (*value)[propsStart+1]

	return float64(bits.OnesCount64(word)) / 64.0
}

/*
loadAffinityArray copies the Value's 5-word affinity region into a
fixed-size array so the per-pair coupling compare stays register-bound.
A nil Value returns the zero fingerprint, which JaccardCouplingAffinity
treats as maximal coupling — undefined Values cannot meaningfully
disagree, so they fold into whatever mode claims them first.
*/
func loadAffinityArray(value *primitive.Value) [affinityWords]uint64 {
	var out [affinityWords]uint64

	if value == nil {
		return out
	}

	start, _ := core.Cfg.Value.Region.Affinity.WordExtent()

	for i := 0; i < affinityWords; i++ {
		out[i] = (*value)[start+i]
	}

	return out
}
