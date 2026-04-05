package markovtrie

import "math"

/*
adaptiveState tracks lightweight online statistics that allow the trie to
self-tune parameters that were previously fixed constants. Every signal is
derived from data the trie already computes (surprisal, interpolation hits,
classification entropy) — no gradient descent, no backprop.
*/
type adaptiveState struct {
	// Interpolation depth tracking: how often each suffix depth "wins"
	// (assigns highest probability to the actual observed next token).
	depthHits    [maxAdaptiveDepth]float64
	depthTotal   float64
	depthDecay   float64
	depthWeights [maxAdaptiveDepth]float64

	// Surprisal EMA for adaptive decay factor.
	surprisalEMA     float64
	surprisalVar     float64
	surprisalSamples int

	// Classification entropy EMA for adaptive context window.
	entropyEMA float64

	// Episodic match quality EMA for adaptive blend weight.
	episodicQualityEMA float64

	// Rolling accuracy for adaptive unsupervised threshold.
	classifyHits  float64
	classifyTotal float64

	// Adaptive pruning: tracks node growth rate.
	lastNodeCount   uint64
	lastPruneStep   int
	growthRateEMA   float64
	pruneThreshold  float64

	// Field pressure: external modulation applied by the distributed field.
	// These shift the trie's behavior without the trie "deciding" anything.
	fieldDecayPressure    float64 // positive = decay faster, negative = retain more
	fieldLearningPressure float64 // positive = learn more aggressively
	fieldPrunePressure    float64 // positive = prune more aggressively

	enabled bool
}

const (
	maxAdaptiveDepth = 8
	adaptiveEMAAlpha = 0.05
	adaptiveMinSamples = 50
)

func newAdaptiveState() *adaptiveState {
	state := &adaptiveState{
		depthDecay:     0.99,
		pruneThreshold: defaultPruneMinimumCount,
		enabled:        true,
	}

	// Initialize uniform depth weights.
	for i := range state.depthWeights {
		state.depthWeights[i] = 1.0
	}

	return state
}

/*
observeInterpolationHit records which suffix depth assigned the highest
probability to the token that actually appeared. Called during training
after the trie has been updated, using the context before the token.
*/
func (state *adaptiveState) observeInterpolationHit(winningDepth int) {
	if !state.enabled || winningDepth < 0 || winningDepth >= maxAdaptiveDepth {
		return
	}

	// Decay all counters, then credit the winner.
	state.depthTotal *= state.depthDecay
	for i := range state.depthHits {
		state.depthHits[i] *= state.depthDecay
	}

	state.depthHits[winningDepth]++
	state.depthTotal++

	// Recompute weights from hit rates.
	if state.depthTotal < adaptiveMinSamples {
		return
	}

	for i := range state.depthWeights {
		rate := state.depthHits[i] / state.depthTotal
		// Blend empirical rate with a prior that favors deeper context
		// (deeper = more specific = generally better when available).
		prior := float64(i+1) / float64(maxAdaptiveDepth)
		state.depthWeights[i] = 0.7*rate + 0.3*prior
	}
}

/*
interpolationWeight returns the adaptive weight for a given suffix depth.
Falls back to exponential (2^depth) when adaptive data is insufficient.
*/
func (state *adaptiveState) interpolationWeight(depth int, linear bool) float64 {
	if !state.enabled || state.depthTotal < adaptiveMinSamples {
		if linear {
			return float64(depth + 1)
		}

		return math.Pow(2, float64(depth))
	}

	if depth < 0 || depth >= maxAdaptiveDepth {
		return 1.0
	}

	return math.Max(state.depthWeights[depth], 0.01)
}

/*
observeSurprisal feeds a token-level surprisal value into the running
EMA so the store can adapt its decay factor to domain volatility.
*/
func (state *adaptiveState) observeSurprisal(bits float64) {
	if !state.enabled {
		return
	}

	state.surprisalSamples++
	alpha := adaptiveEMAAlpha

	if state.surprisalSamples == 1 {
		state.surprisalEMA = bits
		state.surprisalVar = 0
		return
	}

	delta := bits - state.surprisalEMA
	state.surprisalEMA += alpha * delta
	state.surprisalVar = (1-alpha)*state.surprisalVar + alpha*delta*delta
}

/*
adaptiveDecayFactor returns a decay factor tuned to domain volatility.
High average surprisal → the domain is changing fast → decay faster (lower factor)
to forget stale patterns. Low surprisal → stable domain → decay slower (higher
factor) to retain long-term memory.
*/
func (state *adaptiveState) adaptiveDecayFactor(base float64) float64 {
	if !state.enabled || state.surprisalSamples < adaptiveMinSamples {
		return base
	}

	// Map surprisal EMA to a decay factor adjustment.
	// Baseline surprisal ~3-5 bits → no change.
	// High surprisal (>8 bits) → decay faster (factor -= 0.01).
	// Low surprisal (<2 bits) → decay slower (factor += 0.003).
	adjustment := (4.0 - state.surprisalEMA) * 0.003

	// Field pressure: external force shifts decay without local consent.
	adjustment -= state.fieldDecayPressure * 0.005

	result := base + adjustment

	return math.Max(0.95, math.Min(0.999, result))
}

/*
observeClassificationEntropy records the entropy of a classification posterior
for adaptive context window sizing.
*/
func (state *adaptiveState) observeClassificationEntropy(scores map[string]float64) {
	if !state.enabled || len(scores) <= 1 {
		return
	}

	entropy := 0.0
	for _, pct := range scores {
		p := pct / 100.0
		if p > 0 {
			entropy -= p * math.Log2(p)
		}
	}

	state.entropyEMA = (1-adaptiveEMAAlpha)*state.entropyEMA + adaptiveEMAAlpha*entropy
}

/*
adaptiveClassificationContext returns a context window size tuned to how
discriminative deeper context has been. High entropy (confused classifier) →
widen the window to use more context. Low entropy → narrow window is sufficient.
*/
func (state *adaptiveState) adaptiveClassificationContext(base int) int {
	if !state.enabled || state.surprisalSamples < adaptiveMinSamples {
		return base
	}

	// Entropy of a uniform distribution over N labels = log2(N).
	// If we're above 70% of max entropy, the classifier is confused → widen.
	if state.entropyEMA > 1.5 {
		return min(base+2, maxAdaptiveDepth-1)
	}

	if state.entropyEMA < 0.5 {
		return max(base-1, 1)
	}

	return base
}

/*
observeEpisodicQuality records how much probability mass the episodic buffer
contributed when it had a match, so the blend weight can adapt.
*/
func (state *adaptiveState) observeEpisodicQuality(episodicMass float64, trieMass float64) {
	if !state.enabled {
		return
	}

	total := episodicMass + trieMass
	if total == 0 {
		return
	}

	quality := episodicMass / total
	state.episodicQualityEMA = (1-adaptiveEMAAlpha)*state.episodicQualityEMA + adaptiveEMAAlpha*quality
}

/*
adaptiveEpisodicBlend returns a blend weight scaled by how useful the episodic
buffer has been recently. If episodic predictions keep hitting, increase the
blend. If they're noise, reduce it.
*/
func (state *adaptiveState) adaptiveEpisodicBlend(base float64) float64 {
	if !state.enabled || state.surprisalSamples < adaptiveMinSamples {
		return base
	}

	// Scale the base blend by observed quality ratio.
	// quality ~0.5 → episodic is as good as trie → full blend.
	// quality ~0.0 → episodic is useless → minimal blend.
	scaled := base * (0.3 + 1.4*state.episodicQualityEMA)

	return math.Max(0.05, math.Min(0.7, scaled))
}

/*
observeClassifyAccuracy tracks whether unsupervised label assignment was
"correct" (the same label still wins after training). This lets the
unsupervised confidence threshold self-calibrate.
*/
func (state *adaptiveState) observeClassifyAccuracy(correct bool) {
	if !state.enabled {
		return
	}

	state.classifyTotal = state.classifyTotal*0.99 + 1
	if correct {
		state.classifyHits = state.classifyHits*0.99 + 1
	} else {
		state.classifyHits *= 0.99
	}
}

/*
adaptiveUnsupervisedThreshold returns a confidence threshold that tightens as
the label space matures and classification accuracy increases, and loosens
when accuracy drops (encouraging new concept creation).
*/
func (state *adaptiveState) adaptiveUnsupervisedThreshold(base float64) float64 {
	if !state.enabled || state.classifyTotal < adaptiveMinSamples {
		return base
	}

	accuracy := state.classifyHits / state.classifyTotal
	// High accuracy → tighten threshold (require more confidence to reuse labels).
	// Low accuracy → loosen (encourage new concepts since existing ones aren't working).
	return base * (0.6 + 0.8*accuracy)
}

/*
observeNodeGrowth tracks trie growth rate for adaptive pruning.
*/
func (state *adaptiveState) observeNodeGrowth(nodeCount uint64, currentStep int) {
	if !state.enabled || currentStep <= state.lastPruneStep {
		return
	}

	stepDelta := float64(currentStep - state.lastPruneStep)
	if stepDelta == 0 {
		return
	}

	growthRate := float64(nodeCount-state.lastNodeCount) / stepDelta
	state.growthRateEMA = (1-adaptiveEMAAlpha)*state.growthRateEMA + adaptiveEMAAlpha*growthRate

	state.lastNodeCount = nodeCount
	state.lastPruneStep = currentStep
}

/*
adaptivePruneThreshold returns a pruning floor that rises when the trie is
growing fast (aggressive cleanup) and falls when growth is slow (preserve
rare knowledge).
*/
func (state *adaptiveState) adaptivePruneThreshold(base float64) float64 {
	if !state.enabled || state.lastPruneStep == 0 {
		return base
	}

	// More growth → higher threshold → prune more aggressively.
	// growthRateEMA of ~10 nodes/step is "normal", scale around that.
	scale := 1.0 + (state.growthRateEMA-10.0)*0.01

	// Field pressure: positive prune pressure amplifies the scale.
	scale += state.fieldPrunePressure * 0.1

	result := base * math.Max(0.5, math.Min(2.0, scale))

	return math.Max(0.01, result)
}

/*
adaptiveSurprisalScale returns the divisor for surprise-modulated learning
rate, calibrated to the observed surprisal distribution. If the trie routinely
sees high surprisal, the scale increases so learning rate doesn't saturate at
1.0 for everything.
*/
func (state *adaptiveState) adaptiveSurprisalScale() float64 {
	if !state.enabled || state.surprisalSamples < adaptiveMinSamples {
		return defaultSurprisalScaleBits
	}

	// Use mean + 1 stddev as the scale, so ~84% of observations produce
	// learning rates below 0.6, keeping headroom for truly novel inputs.
	stddev := math.Sqrt(state.surprisalVar)

	// Field pressure: positive learning pressure lowers the scale,
	// which makes the same surprisal produce a higher learning rate.
	scale := state.surprisalEMA + stddev - state.fieldLearningPressure*0.5

	return math.Max(1.0, scale)
}

/*
adaptiveTemperature modulates a caller-supplied temperature based on the
entropy of the raw (pre-temperature) probability distribution.

Low entropy (confident) → scale temperature up to allow exploration.
High entropy (uncertain) → scale temperature down to concentrate signal.

When the caller passes temperature=0 (greedy), this returns 0 unchanged.
*/
func (state *adaptiveState) adaptiveTemperature(base float64, rawProbabilities map[string]float64) float64 {
	if !state.enabled || base == 0 || len(rawProbabilities) <= 1 {
		return base
	}

	entropy := 0.0
	for _, p := range rawProbabilities {
		if p > 0 {
			entropy -= p * math.Log2(p)
		}
	}

	// Normalize entropy to [0, 1] relative to uniform distribution.
	maxEntropy := math.Log2(float64(len(rawProbabilities)))
	if maxEntropy == 0 {
		return base
	}

	normalizedEntropy := entropy / maxEntropy

	// Scale: low normalized entropy (confident) → multiply by up to 1.5.
	//        high normalized entropy (confused) → multiply by down to 0.5.
	scale := 1.5 - normalizedEntropy

	result := base * scale

	return math.Max(0.1, math.Min(2.0, result))
}

/*
deriveTemperature computes a temperature from scratch based on the trie's
current state, without any caller hint. Uses the surprisal EMA as a proxy
for how volatile/uncertain the domain is.

Low surprisal (stable/known domain) → higher temperature (explore).
High surprisal (volatile/unknown domain) → lower temperature (exploit what you know).
*/
func (state *adaptiveState) deriveTemperature() float64 {
	if !state.enabled || state.surprisalSamples < adaptiveMinSamples {
		return 0.7 // sensible default before calibration
	}

	// Map surprisal EMA to temperature.
	// EMA ~1 bit (very familiar) → temp ~1.2
	// EMA ~5 bits (moderately novel) → temp ~0.7
	// EMA ~10 bits (very novel) → temp ~0.3
	temp := 1.4 - state.surprisalEMA*0.11

	return math.Max(0.2, math.Min(1.5, temp))
}

/*
deriveBeamWidth returns a beam width based on classification confidence.
High confidence → narrow beam (fewer resources, answer is clear).
Low confidence → wide beam (explore more hypotheses).
*/
func (state *adaptiveState) deriveBeamWidth(confidence float64) int {
	if !state.enabled {
		return 3
	}

	// confidence is a percentage [0, 100].
	// >80% → beam 2 (tight)
	// 40-80% → beam 3-4
	// <40% → beam 5-6 (wide exploration)
	if confidence > 80 {
		return 2
	}

	if confidence > 40 {
		return 3 + int((80-confidence)/40*2)
	}

	return 5 + int((40-confidence)/40*2)
}

/*
deriveMaxHops returns a generation length based on how deep the trie's
productive paths tend to be.
*/
func (state *adaptiveState) deriveMaxHops(base int) int {
	if !state.enabled || state.depthTotal < adaptiveMinSamples {
		return base
	}

	// Find the deepest depth with >10% hit rate — that's roughly how far
	// the trie can reliably predict. Generate 2x that for exploration.
	deepest := 2

	for i := maxAdaptiveDepth - 1; i >= 0; i-- {
		rate := state.depthHits[i] / state.depthTotal
		if rate > 0.10 {
			deepest = i
			break
		}
	}

	hops := deepest * 2
	if hops < 4 {
		hops = 4
	}

	return min(hops, 20)
}

/*
adaptiveInterpolationDepth returns how many suffix levels to consider,
based on which depths have been productive.
*/
func (state *adaptiveState) adaptiveInterpolationDepth(base int) int {
	if !state.enabled || state.depthTotal < adaptiveMinSamples {
		return base
	}

	// Find the deepest depth that has meaningful hit rate (>5%).
	deepest := base

	for i := maxAdaptiveDepth - 1; i > base; i-- {
		rate := state.depthHits[i] / state.depthTotal
		if rate > 0.05 {
			deepest = i
			break
		}
	}

	// Never go below the configured base.
	return max(base, min(deepest, maxAdaptiveDepth-1))
}
