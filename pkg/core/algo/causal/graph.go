package causal

import (
	"math"
	"math/bits"
	"strconv"
	"sync"

	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/numeric"
	"github.com/theapemachine/six/pkg/core/numeric/adaptive"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
edgeKey identifies a directed transition between two Values by their
IDs. Directionality comes from temporal ordering in the trie walk —
from precedes to. The trie's Prev/Next pointers encode this ordering;
the edge map accumulates per-label statistics over it.
*/
type edgeKey struct {
	from uint64
	to   uint64
}

/*
edge tracks per-label conditional counts for one directed transition.
Invariance — the consistency of P(to|from) across labels — separates
causal edges from correlational ones.

A causal edge has stable conditional probability regardless of which
cluster (label) it appears in. A correlational edge's conditional
varies by cluster. The coefficient of variation inverted to [0,1]
quantifies this: 1 = perfectly stable (causal), 0 = wildly varying
(correlational).
*/
type edge struct {
	labelCounts map[string]float64
	totalCount  float64
	invariance  float64
	reliability float64
}

/*
recomputeInvariance measures how stable P(to|from) is across labels.
Uses coefficient of variation (stdev/mean) inverted to [0,1].
Reliability keeps single-regime evidence useful while still giving
cross-regime invariance more authority once enough labels exist.
*/
func (edge *edge) recomputeInvariance() {
	support := edge.supportConfidence()

	if len(edge.labelCounts) < 2 {
		edge.invariance = 0
		edge.reliability = support * 0.5
		return
	}

	var sum, sumSq float64
	count := float64(len(edge.labelCounts))

	for _, labelCount := range edge.labelCounts {
		prob := labelCount / edge.totalCount
		sum += prob
		sumSq += prob * prob
	}

	mean := sum / count
	variance := sumSq/count - mean*mean

	if variance < 0 {
		variance = 0
	}

	if mean <= 0 {
		edge.invariance = 0
		edge.reliability = 0
		return
	}

	edge.invariance = 1.0 / (1.0 + math.Sqrt(variance)/mean)
	edge.reliability = support * edge.invariance
}

func (edge *edge) supportConfidence() float64 {
	if edge == nil || edge.totalCount <= 0 {
		return 0
	}

	return edge.totalCount / (edge.totalCount + float64(len(edge.labelCounts)+1))
}

/*
Graph implements algo.Algorithm and tracks the causal structure
that emerges from observing directed transitions across different
label contexts (clusters).

Pearl's three levels map to concrete operations on existing Value
regions:

	Level 1 — Association: per-label edge counts yield P(Y|X).
	          Affinity similarity between Values quantifies
	          associative strength.

	Level 2 — Intervention: Intervene severs a Value from its
	          causal parents by XOR-unbinding them from the
	          Context region, then rebinds the forced value.
	          The modified Value routes through normal prediction.

	Level 3 — Counterfactual: Counterfactual abducts noise from
	          the Gradient region (accumulated intervention
	          residuals), intervenes, then re-applies noise so
	          the prediction carries latent state from the
	          original observation.

The edge tracks two related quantities: transportable invariance across
regime labels, and support-adjusted reliability for policy. Field phase
can be captured as part of the regime label so the same edge can become
stable only under a specific mesh mode.

Signals produced:
  - CausalStrength: smoothed mean reliability across observed edges.
  - InterventionResidual: smoothed Hamming distance between
    predicted and observed affinities after intervention.
*/
type Graph struct {
	mu    sync.RWMutex
	edges map[edgeKey]*edge

	prediction      *algo.Prediction
	causalStrength  *numeric.Derived
	residualTracker *numeric.Derived
	phaseRegime     string
}

/*
NewGraph constructs a causal graph algorithm with self-tuning
signal chains.
*/
func NewGraph() *Graph {
	causalStrength := numeric.NewDerived(
		numeric.WithDynamics(adaptive.NewEMA()),
	)

	residualTracker := numeric.NewDerived(
		numeric.WithDynamics(adaptive.NewEMA()),
	)

	prediction := algo.NewPrediction()
	prediction.Signals[algo.CausalStrength] = causalStrength
	prediction.Signals[algo.InterventionResidual] = residualTracker

	return &Graph{
		edges:           make(map[edgeKey]*edge),
		prediction:      prediction,
		causalStrength:  causalStrength,
		residualTracker: residualTracker,
	}
}

/*
Update receives a walked trie path in prediction.Context and the
current causal regime label in prediction.Targets. Consecutive Value
pairs in the path are directed edges. Per-regime counts accumulate on
each edge and invariance/reliability are recomputed. The CausalStrength
signal tracks the smoothed mean reliability across all edges observed
this update.
*/
func (graph *Graph) Update(
	prediction *algo.Prediction,
) (*algo.Prediction, error) {
	graph.capturePhaseRegime(prediction)

	if prediction == nil || len(prediction.Context) < 2 {
		return graph.prediction, nil
	}

	label := graph.updateLabel(prediction)

	if label == "" {
		return graph.prediction, nil
	}

	graph.mu.Lock()

	var totalReliability float64
	var edgeCount int

	for idx := 0; idx < len(prediction.Context)-1; idx++ {
		key := edgeKey{
			from: prediction.Context[idx].ID(),
			to:   prediction.Context[idx+1].ID(),
		}

		entry := graph.edges[key]

		if entry == nil {
			entry = &edge{
				labelCounts: make(map[string]float64),
			}
			graph.edges[key] = entry
		}

		entry.labelCounts[label]++
		entry.totalCount++
		entry.recomputeInvariance()

		totalReliability += entry.reliability
		edgeCount++
	}

	graph.mu.Unlock()

	if edgeCount > 0 {
		graph.causalStrength.Next(totalReliability / float64(edgeCount))
	}

	return graph.prediction, nil
}

func (graph *Graph) capturePhaseRegime(prediction *algo.Prediction) {
	if graph == nil || prediction == nil || prediction.Signals == nil {
		return
	}

	globalPhase, ok := prediction.Signals[algo.GlobalPhase]

	if !ok || globalPhase == nil {
		return
	}

	lane, active := algo.ParseGlobalPhaseIndex(globalPhase.Value())
	regime := ""

	if active && lane >= 0 {
		regime = "phase:" + strconv.Itoa(lane)
	}

	graph.mu.Lock()
	graph.phaseRegime = regime
	graph.mu.Unlock()
}

func (graph *Graph) updateLabel(prediction *algo.Prediction) string {
	targets := prediction.SupervisionLabels()

	if len(targets) == 0 {
		return ""
	}

	label := string(targets[0].Label)

	if label == "" {
		return ""
	}

	graph.mu.RLock()
	phaseRegime := graph.phaseRegime
	graph.mu.RUnlock()

	if phaseRegime == "" {
		return label
	}

	return label + "\x1e" + phaseRegime
}

func (graph *Graph) Value() *algo.Prediction {
	return graph.prediction
}

/*
EdgeInvariance returns the invariance score for a specific directed
edge by Value ID. Returns 0 if the edge has not been observed.
*/
func (graph *Graph) EdgeInvariance(from, to uint64) float64 {
	graph.mu.RLock()
	defer graph.mu.RUnlock()

	entry := graph.edges[edgeKey{from: from, to: to}]

	if entry == nil {
		return 0
	}

	return entry.invariance
}

/*
EdgeReliability returns the support-adjusted strength for a directed edge.
Single-regime evidence can contribute up to half strength; cross-regime
invariance can approach full strength as support grows.
*/
func (graph *Graph) EdgeReliability(from, to uint64) float64 {
	graph.mu.RLock()
	defer graph.mu.RUnlock()

	entry := graph.edges[edgeKey{from: from, to: to}]

	if entry == nil {
		return 0
	}

	return entry.reliability
}

/*
CausalParents returns edges leading into the given Value ID sorted
by invariance descending. Only edges above the threshold are
returned — these are the candidate causal parents in the SCM.
*/
func (graph *Graph) CausalParents(
	target uint64, threshold float64,
) []ParentEdge {
	graph.mu.RLock()
	defer graph.mu.RUnlock()

	var parents []ParentEdge

	for key, entry := range graph.edges {
		if key.to != target || entry.invariance < threshold {
			continue
		}

		parents = append(parents, ParentEdge{
			ID:          key.from,
			Invariance:  entry.invariance,
			Reliability: entry.reliability,
			Count:       entry.totalCount,
		})
	}

	for idx := 1; idx < len(parents); idx++ {
		for jdx := idx; jdx > 0 && parents[jdx].Invariance > parents[jdx-1].Invariance; jdx-- {
			parents[jdx], parents[jdx-1] = parents[jdx-1], parents[jdx]
		}
	}

	return parents
}

/*
ParentEdge is a single causal parent with its invariance score.
*/
type ParentEdge struct {
	ID          uint64
	Invariance  float64
	Reliability float64
	Count       float64
}

/*
Intervene implements Pearl's do(X=x) operator. Severs the target
Value from its causal parents by XOR-unbinding their affinities
from the Context region, then rebinds the forced Value's affinity.

The severed Value can be routed through normal prediction to answer
"what would happen if this were forced to a different value?"

parentAffinities maps parent Value IDs to their affinity vectors.
The caller provides these because the graph tracks IDs, not Values.
*/
func (graph *Graph) Intervene(
	value InterventionTarget,
	target uint64,
	forced InterventionTarget,
	parentAffinities map[uint64][primitive.AffinityWords]uint64,
) (severed []uint64) {
	if value == nil || forced == nil {
		return nil
	}

	parents := graph.CausalParents(target, 0.5)

	for _, parent := range parents {
		aff, ok := parentAffinities[parent.ID]

		if !ok {
			continue
		}

		value.BindContext(aff)
		severed = append(severed, parent.ID)
	}

	forcedAff := forced.AffinityVector()
	value.BindContext(forcedAff)

	return severed
}

/*
Counterfactual implements Pearl's Level 3: "What would Y have been
if X had been x', given that we observed X=x?"

Three steps:
 1. Abduction — capture noise from the Gradient region (accumulated
    intervention residuals encode the latent state).
 2. Intervention — sever X's causal parents and force X=x'.
 3. Re-apply noise — the modified context carries the latent state
    of the original observation forward into the counterfactual.

Returns the IDs of severed parents.
*/
func (graph *Graph) Counterfactual(
	observed InterventionTarget,
	target uint64,
	forced InterventionTarget,
	parentAffinities map[uint64][primitive.AffinityWords]uint64,
) []uint64 {
	if observed == nil || forced == nil {
		return nil
	}

	noise := observed.GradientVector()

	severed := graph.Intervene(observed, target, forced, parentAffinities)

	observed.AccumulateGradient(noise)

	return severed
}

/*
HopResult captures the output of a single counterfactual hop —
the severed parents, the residual Hamming distance from the
intervention, and whether the causal chain has bottomed out.
*/
type HopResult struct {
	Severed  []uint64
	Residual int
	Settled  bool
	// LatentNoise captures the gradient region immediately before this hop's
	// intervention — the abducted SCM noise term used for counterfactuals.
	// Together across hops this exposes more latent state than a single
	// 512-bit on-frame gradient holds after XOR folding.
	LatentNoise [primitive.RegionWords]uint64
}

/*
CounterfactualChain performs multi-hop counterfactual reasoning by
iteratively applying interventions and feeding each hop's result
through a predictor. Each hop:

 1. Applies a single-hop Counterfactual on the current Value.
 2. Calls predict to obtain the downstream Value that results from
    routing the intervened Value through normal trie prediction.
 3. Measures the gradient residual (Hamming distance between the
    predicted and observed affinities).
 4. Terminates when the residual stops growing (the causal chain
    has bottomed out) or maxHops is reached.

The interventions slice specifies (target, forced, parentAffinities)
for each hop in sequence. If fewer interventions exist than maxHops,
the chain terminates after applying them all.

predict must return the downstream Value that the trie would produce
given the intervened Value. The graph does not own prediction — the
caller wires this to their trie's Predict path.
*/
func (graph *Graph) CounterfactualChain(
	observed InterventionTarget,
	interventions []Intervention,
	predict func(InterventionTarget) InterventionTarget,
	maxHops int,
) []HopResult {
	if observed == nil || predict == nil || len(interventions) == 0 {
		return nil
	}

	if maxHops <= 0 {
		maxHops = len(interventions)
	}

	results := make([]HopResult, 0, maxHops)
	current := observed
	prevResidual := -1

	for hop := 0; hop < maxHops && hop < len(interventions); hop++ {
		iv := interventions[hop]

		latent := current.GradientVector()

		severed := graph.Counterfactual(
			current, iv.Target, iv.Forced, iv.ParentAffinities,
		)

		predicted := predict(current)

		residual := 0

		if predicted != nil {
			predAff := predicted.AffinityVector()
			curAff := current.AffinityVector()

			for wordIdx := range primitive.AffinityWords {
				xor := predAff[wordIdx] ^ curAff[wordIdx]

				if wordIdx == primitive.AffinityWords-1 {
					xor &= primitive.AffinityLastWordMask
				}

				residual += bits.OnesCount64(xor)
			}
		}

		settled := prevResidual >= 0 && residual <= prevResidual
		prevResidual = residual

		results = append(results, HopResult{
			Severed:     severed,
			Residual:    residual,
			Settled:     settled,
			LatentNoise: latent,
		})

		if settled {
			break
		}

		if predicted != nil {
			current = predicted
		}
	}

	return results
}

/*
Intervention specifies a single hop in a multi-hop counterfactual chain.
*/
type Intervention struct {
	Target           uint64
	Forced           InterventionTarget
	ParentAffinities map[uint64][primitive.AffinityWords]uint64
}

/*
ObserveResidual records the difference between predicted and observed
outcomes after an intervention. The XOR of their affinity vectors is
accumulated into the Gradient region, building the noise term that
enables future counterfactual abduction.
*/
func (graph *Graph) ObserveResidual(
	predicted InterventionTarget,
	observed InterventionTarget,
) {
	if predicted == nil || observed == nil {
		return
	}

	predAff := predicted.AffinityVector()
	obsAff := observed.AffinityVector()

	/*
		residual is RegionWords-wide because observed.AccumulateGradient
		consumes a full region buffer. Only indices below AffinityWords are
		filled from the affinity XOR; RegionWords >= AffinityWords, so the
		tail stays zero by design and is still passed through as part of the
		gradient slice shape.
	*/
	var residual [primitive.RegionWords]uint64
	var dist int

	for wordIdx := range primitive.AffinityWords {
		xor := predAff[wordIdx] ^ obsAff[wordIdx]

		if wordIdx == primitive.AffinityWords-1 {
			xor &= primitive.AffinityLastWordMask
		}

		residual[wordIdx] = xor
		dist += bits.OnesCount64(xor)
	}

	observed.AccumulateGradient(residual)

	graph.residualTracker.Next(float64(dist))
}

/*
Mediate decomposes the total causal effect of X on Y into direct
and indirect (mediated through Z) components.

	Total effect:    do(X=x') on Y
	Direct effect:   do(X=x', Z=z_observed) on Y — hold Z fixed
	Indirect effect: Total - Direct

The caller provides:
  - value: the Value to intervene on
  - xTarget/xForced: the X intervention
  - zTarget/zObserved: the mediator Z and its observed value
  - parentAffinities: affinity vectors for causal parents
  - predict: trie prediction function

Returns (directResidual, indirectResidual). A large indirect
residual means Z mediates a significant portion of X→Y.
*/
func (graph *Graph) Mediate(
	value *primitive.Value,
	xTarget uint64,
	xForced *primitive.Value,
	zTarget uint64,
	zObserved *primitive.Value,
	parentAffinities map[uint64][primitive.AffinityWords]uint64,
	predict func(InterventionTarget) InterventionTarget,
) (directResidual int, indirectResidual int) {
	if value == nil || xForced == nil || zObserved == nil || predict == nil {
		return 0, 0
	}

	totalChain := graph.CounterfactualChain(
		value,
		[]Intervention{{
			Target:           xTarget,
			Forced:           xForced,
			ParentAffinities: parentAffinities,
		}},
		predict,
		1,
	)

	totalResidual := 0

	if len(totalChain) > 0 {
		totalResidual = totalChain[0].Residual
	}

	var copy primitive.Value
	copy = *value

	graph.CounterfactualChain(
		&copy,
		[]Intervention{
			{
				Target:           xTarget,
				Forced:           xForced,
				ParentAffinities: parentAffinities,
			},
			{
				Target:           zTarget,
				Forced:           zObserved,
				ParentAffinities: parentAffinities,
			},
		},
		predict,
		2,
	)

	directPredicted := predict(&copy)

	if directPredicted != nil {
		predAff := directPredicted.AffinityVector()
		origAff := value.AffinityVector()

		for wordIdx := range primitive.AffinityWords {
			xor := predAff[wordIdx] ^ origAff[wordIdx]

			if wordIdx == primitive.AffinityWords-1 {
				xor &= primitive.AffinityLastWordMask
			}

			directResidual += bits.OnesCount64(xor)
		}
	}

	indirectResidual = totalResidual - directResidual

	if indirectResidual < 0 {
		indirectResidual = 0
	}

	return directResidual, indirectResidual
}

/*
Moderate tests whether Z moderates the effect of X on Y — whether
X's causal effect on Y changes depending on Z's value.

Runs two counterfactual chains with the same X intervention but
different Z values. If the residuals differ significantly, Z
moderates X→Y.

Returns (residualWithZ1, residualWithZ2). The caller compares
these to determine moderation strength.
*/
func (graph *Graph) Moderate(
	value *primitive.Value,
	xTarget uint64,
	xForced *primitive.Value,
	zTarget uint64,
	zValue1 *primitive.Value,
	zValue2 *primitive.Value,
	parentAffinities map[uint64][primitive.AffinityWords]uint64,
	predict func(InterventionTarget) InterventionTarget,
) (residualZ1 int, residualZ2 int) {
	if value == nil || xForced == nil || zValue1 == nil || zValue2 == nil || predict == nil {
		return 0, 0
	}

	var copy1 primitive.Value
	copy1 = *value

	chain1 := graph.CounterfactualChain(
		&copy1,
		[]Intervention{
			{Target: xTarget, Forced: xForced, ParentAffinities: parentAffinities},
			{Target: zTarget, Forced: zValue1, ParentAffinities: parentAffinities},
		},
		predict,
		2,
	)

	if len(chain1) > 0 {
		residualZ1 = chain1[len(chain1)-1].Residual
	}

	var copy2 primitive.Value
	copy2 = *value

	chain2 := graph.CounterfactualChain(
		&copy2,
		[]Intervention{
			{Target: xTarget, Forced: xForced, ParentAffinities: parentAffinities},
			{Target: zTarget, Forced: zValue2, ParentAffinities: parentAffinities},
		},
		predict,
		2,
	)

	if len(chain2) > 0 {
		residualZ2 = chain2[len(chain2)-1].Residual
	}

	return residualZ1, residualZ2
}
