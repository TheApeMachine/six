package mesh

import (
	"math"
	"math/bits"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
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
labelPropertyWords is how many consecutive properties words primitive.Emit
WithLabels stamps (Properties.Start+LABLES+i). MeasureFieldMetrics must
scan the same span so slot counts match Emit, not only the first packed
word.
*/
const labelPropertyWords = 4

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

Unrolled across the fixed primitive.AffinityWords count so the compiler keeps the
comparison in registers. A union popcount of zero (both fingerprints
fully blank) is treated as maximal coupling — two blank values are
trivially indistinguishable and should always share a mode.
*/
func JaccardCouplingAffinity(a, b [primitive.AffinityWords]uint64) float64 {
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
func loadAffinityArray(value *primitive.Value) [primitive.AffinityWords]uint64 {
	var out [primitive.AffinityWords]uint64

	if value == nil {
		return out
	}

	start, _ := core.Cfg.Value.Region.Affinity.WordExtent()

	for i := 0; i < primitive.AffinityWords; i++ {
		out[i] = (*value)[start+i]
	}

	return out
}

/*
MeasureFieldMetrics aggregates crystallisation-style metrics from a member
slice and the current eigenmode snapshot. Used when a Field ingests frames
(io.Write) so observation stays on the streaming path instead of a separate Cycle.
*/
func MeasureFieldMetrics(members []*primitive.Value, snap *geometry.EigenSnap) FieldMetrics {
	var out FieldMetrics

	n := 0

	for _, v := range members {
		if v != nil {
			n++
		}
	}

	out.MemberCount = n

	if n == 0 {
		return out
	}

	propsStart, propsWords := core.Cfg.Value.Region.Properties.WordExtent()

	if propsWords < 1 {
		return out
	}

	hist := make(map[uint16]int)
	labelSlots := 0
	labeled := 0

	labelWords := labelPropertyWords
	if propsWords < labelWords {
		labelWords = propsWords
	}

	for _, v := range members {
		if v == nil {
			continue
		}

		memberSlots := 0

		for widx := 0; widx < labelWords; widx++ {
			w := (*v)[propsStart+widx]

			for lane := 0; lane < 4; lane++ {
				if uint16(w>>(lane*16)) != 0 {
					memberSlots++
				}
			}
		}

		// LabelDensity divides by n×4 — at most four supervision slots per member.
		if memberSlots > 4 {
			memberSlots = 4
		}

		if memberSlots > 0 {
			labeled++

			// Consensus histogram: only labeled members (same as Coverage). Unlabeled
			// frames would otherwise stamp bucket 0 and dilute single-class consensus.
			w0 := (*v)[propsStart]
			hist[uint16(w0&0xFFFF)]++
		}

		labelSlots += memberSlots
	}

	out.LabeledCount = labeled
	out.SlotSum = labelSlots
	out.Coverage = float64(labeled) / float64(n)
	out.LabelDensity = float64(labelSlots) / float64(n*4)
	out.Consensus = shannonConsensus(hist)
	out.Crystallization = out.Coverage * out.Consensus * out.LabelDensity
	out.Saturated = out.Crystallization >= crystallizationFloor

	if snap != nil {
		out.ModeCount = len(snap.Modes())
		out.DominantRatio = dominantEnergyRatio(snap)
		out.PressureMult = 1 - out.DominantRatio
	}

	return out
}

// RollupFieldMetrics combines child community metrics for a routing parent (one level).
func RollupFieldMetrics(children []*Field) FieldMetrics {
	if len(children) == 0 {
		return FieldMetrics{}
	}

	var out FieldMetrics

	var (
		totalMembers    int
		sumCryst        float64
		sumCoverage     float64
		sumConsensus    float64
		sumLabelDensity float64
		maxModeCount    int
		maxDominant     float64
	)

	for _, ch := range children {
		if ch == nil {
			continue
		}

		metrics := ch.metrics.Load()

		if metrics.MemberCount == 0 {
			continue
		}

		w := float64(metrics.MemberCount)
		totalMembers += metrics.MemberCount
		sumCryst += metrics.Crystallization * w
		sumCoverage += metrics.Coverage * w
		sumConsensus += metrics.Consensus * w
		sumLabelDensity += metrics.LabelDensity * w

		if metrics.ModeCount > maxModeCount {
			maxModeCount = metrics.ModeCount
		}

		if metrics.DominantRatio > maxDominant {
			maxDominant = metrics.DominantRatio
		}
	}

	out.MemberCount = totalMembers
	out.ModeCount = maxModeCount
	out.DominantRatio = maxDominant
	out.PressureMult = 1 - out.DominantRatio

	if totalMembers > 0 {
		inv := 1 / float64(totalMembers)
		out.Crystallization = sumCryst * inv
		out.Coverage = sumCoverage * inv
		out.Consensus = sumConsensus * inv
		out.LabelDensity = sumLabelDensity * inv
		out.Saturated = out.Crystallization >= crystallizationFloor
	}

	return out
}
