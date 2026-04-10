package markovtrie

import (
	"context"
	"errors"
	"maps"
	"math"
	"math/bits"
	"strings"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/algo/causal"
	"github.com/theapemachine/six/pkg/core/algo/policy"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
coactivationStat is one immutable sensory-action-reward reinforcement summary.
RewardSum accumulates signed reinforcement; ResidualSum accumulates reward
affinity drift against the previous prediction for the same sensory-action
pair. Count tracks how many observations contributed so expectation and
residual confidence can be recovered lazily.
*/
type coactivationStat struct {
	RewardSum       float64
	ResidualSum     float64
	Count           float64
	SensoryID       uint64
	ActionID        uint64
	RewardID        uint64
	SensoryAffinity [primitive.AffinityWords]uint64
	ActionAffinity  [primitive.AffinityWords]uint64
	RewardAffinity  [primitive.AffinityWords]uint64
	SensoryVector   primitive.FrameMultivector
	ActionVector    primitive.FrameMultivector
	RewardVector    primitive.FrameMultivector
}

/*
coactivationMap holds immutable coactivation statistics keyed by LinkKey.
It is swapped atomically so multimodal bookkeeping stays mutex-free.
*/
type coactivationMap struct {
	m map[string]coactivationStat
}

/*
MultimodalCoordinator binds sensory, action, and reward tries while tracking
coactivated terminal nodes so reward expectations can be queried from paired
sensory-action histories without a separate simulator graph.
*/
type MultimodalCoordinator struct {
	ctx          context.Context
	cancel       context.CancelFunc
	err          error
	Sensory      *Store
	Action       *Store
	Reward       *Store
	causal       *causal.Graph
	coactivation atomic.Pointer[coactivationMap]
	actionAlign  atomic.Pointer[domainAlignment]
}

/*
NewMultimodalCoordinator constructs three independent stores that share option
parity but maintain isolated trie roots and vocabularies.
*/
func NewMultimodalCoordinator(
	ctx context.Context, options ...Option,
) (coordinator *MultimodalCoordinator, err error) {
	ctx, cancel := context.WithCancel(ctx)

	initial := &coactivationMap{m: make(map[string]coactivationStat)}

	coordinator = &MultimodalCoordinator{
		ctx:    ctx,
		cancel: cancel,
		causal: causal.NewGraph(),
	}

	coordinator.coactivation.Store(initial)

	if coordinator.Sensory, err = NewStore(ctx, primitive.Affinity{}, options...); err != nil {
		cancel()
		return nil, errnie.Error(err)
	}

	if coordinator.Action, err = NewStore(ctx, primitive.Affinity{}, options...); err != nil {
		cancel()
		return nil, errnie.Error(err)
	}

	if coordinator.Reward, err = NewStore(ctx, primitive.Affinity{}, options...); err != nil {
		cancel()
		return nil, errnie.Error(err)
	}

	return coordinator, validate.Require(map[string]any{
		"ctx":          coordinator.ctx,
		"cancel":       coordinator.cancel,
		"Sensory":      coordinator.Sensory,
		"Action":       coordinator.Action,
		"Reward":       coordinator.Reward,
		"causal":       coordinator.causal,
		"coactivation": coordinator.coactivation.Load(),
	})
}

func (coordinator *MultimodalCoordinator) LinkKey(
	sensorySequence string, actionSequence string, rewardSequence string,
) string {
	if coordinator == nil {
		return ""
	}

	if coordinator.Sensory == nil || coordinator.Action == nil || coordinator.Reward == nil {
		return ""
	}

	return sensorySequence + "\x1f" + actionSequence + "\x1f" + rewardSequence
}

/*
ObserveOutcome loads one sensory-action-reward event into the respective tries
and folds the signed reinforcement into the coactivation table. This is the
minimal policy-learning path: exact experiences update local modality stores,
while the coordinator tracks expected reward for their joint activation.
*/
func (coordinator *MultimodalCoordinator) ObserveOutcome(
	sensory primitive.Value,
	action primitive.Value,
	reward primitive.Value,
	reinforcement float64,
	rewardLabels ...string,
) error {
	return coordinator.ObserveOutcomeInRegimes(
		sensory,
		action,
		reward,
		reinforcement,
		nil,
		rewardLabels...,
	)
}

/*
ObserveOutcomeInRegimes keeps reward labels in the reward store while causal
regime labels describe the context where the transition should remain stable.
If no regime is supplied, the edge is treated as single-regime evidence rather
than overloading the reward label as a causal environment.
*/
func (coordinator *MultimodalCoordinator) ObserveOutcomeInRegimes(
	sensory primitive.Value,
	action primitive.Value,
	reward primitive.Value,
	reinforcement float64,
	causalRegimes []string,
	rewardLabels ...string,
) error {
	if coordinator == nil {
		return nil
	}

	var observedErr error

	if coordinator.Sensory != nil {
		if err := coordinator.Sensory.Load(sensory); err != nil {
			observedErr = errors.Join(observedErr, err)
		}
	}

	if coordinator.Action != nil {
		if err := coordinator.Action.Load(action); err != nil {
			observedErr = errors.Join(observedErr, err)
		}
	}

	if coordinator.Reward != nil {
		if err := coordinator.Reward.Load(reward, rewardLabels...); err != nil {
			observedErr = errors.Join(observedErr, err)
		}
	}

	linkKey := coordinator.LinkKey(
		sensory.String(),
		action.String(),
		reward.String(),
	)

	if linkKey == "" {
		return observedErr
	}

	predictedRewardAffinity, hasPredictedReward := coordinator.predictedRewardAffinity(
		sensory.String(),
		action.String(),
	)
	residual := 0.0

	if hasPredictedReward {
		residual = coordinator.observeRewardResidual(predictedRewardAffinity, reward)
	}

	coordinator.updateCoactivation(
		linkKey,
		reinforcement,
		residual,
		sensory.ID(),
		action.ID(),
		reward.ID(),
		sensory.AffinityVector(),
		action.AffinityVector(),
		reward.AffinityVector(),
		sensory.ContextMultivector(),
		action.ContextMultivector(),
		reward.ContextMultivector(),
	)

	regimeLabels := coordinator.causalRegimeLabels(causalRegimes)

	for _, regimeLabel := range regimeLabels {
		if regimeLabel == "" || coordinator.causal == nil {
			continue
		}

		causalPrediction := algo.NewPrediction()
		causalPrediction.AddTargets(algo.Label{
			Label:      []byte(regimeLabel),
			Confidence: 1,
		})
		causalPrediction.AddContext(sensory, action, reward)

		if _, err := coordinator.causal.Update(causalPrediction); err != nil {
			observedErr = errors.Join(observedErr, err)
		}
	}

	return observedErr
}

/*
ActionScores projects current sensory evidence upward into ranked action
candidates. Exact sensory matches always contribute; trie-level sensory
continuations contribute additional weight when prediction can generalize to
neighboring states. Causal reliability is applied as a bottleneck over the
sensory-action-reward path, while residual drift attenuates unstable outcomes.
*/
func (coordinator *MultimodalCoordinator) ActionScores(
	sensory primitive.Value,
) (policy.ActionScores, error) {
	if coordinator == nil || coordinator.Sensory == nil {
		return nil, nil
	}

	sensoryPrediction, predictErr := coordinator.Sensory.Predict(sensory)
	candidates := coordinator.sensoryCandidateWeights(sensory.String(), sensoryPrediction)

	snapshot := coordinator.coactivation.Load()
	projectedAction, hasProjection := coordinator.projectSensoryAction(
		sensory.ContextMultivector(),
	)

	if snapshot == nil || len(snapshot.m) == 0 {
		return nil, predictErr
	}

	actionValues := make(map[string]float64)
	actionSupport := make(map[string]float64)

	for linkKey, stat := range snapshot.m {
		sensorySequence, actionSequence, _, ok := coordinator.parseLinkKey(linkKey)

		if !ok {
			continue
		}

		weight, matched := candidates[sensorySequence]
		projectionWeight := 0.0

		if hasProjection {
			projectionWeight = frameMultivectorCosine(projectedAction, stat.ActionVector)

			if projectionWeight < 0 {
				projectionWeight = 0
			}
		}

		if projectionWeight > 0 {
			weight += projectionWeight
			matched = true
		}

		if !matched || stat.Count <= 0 {
			continue
		}

		expectedReward := stat.RewardSum / stat.Count
		causalWeight := coordinator.causalPathWeight(
			stat.SensoryID,
			stat.ActionID,
			stat.RewardID,
		)
		residualConfidence := coordinator.residualConfidence(stat)

		actionValues[actionSequence] += weight * expectedReward * (1.0 + causalWeight) * residualConfidence
		actionSupport[actionSequence] += weight * stat.Count
	}

	scores := policy.NewActionScores(actionValues, actionSupport)
	scores.SortDescending()

	return scores, predictErr
}

/*
PredictAction returns ranked action continuations for the given sensory Value.
The result lives in the shared Prediction envelope so higher layers can compose
policy output exactly like beam output.
*/
func (coordinator *MultimodalCoordinator) PredictAction(
	sensory primitive.Value,
) (*algo.Prediction, error) {
	scores, err := coordinator.ActionScores(sensory)
	prediction := algo.NewPrediction()

	if len(scores) > 0 {
		prediction = scores.Prediction()
	}

	if coordinator != nil && coordinator.causal != nil {
		prediction.Merge(coordinator.causal.Value())
	}

	return prediction, err
}

func (coordinator *MultimodalCoordinator) updateCoactivation(
	linkKey string,
	reinforcement float64,
	residual float64,
	sensoryID uint64,
	actionID uint64,
	rewardID uint64,
	sensoryAffinity [primitive.AffinityWords]uint64,
	actionAffinity [primitive.AffinityWords]uint64,
	rewardAffinity [primitive.AffinityWords]uint64,
	sensoryVector primitive.FrameMultivector,
	actionVector primitive.FrameMultivector,
	rewardVector primitive.FrameMultivector,
) {
	if coordinator == nil || linkKey == "" {
		return
	}

	for {
		old := coordinator.coactivation.Load()
		base := make(map[string]coactivationStat)

		if old != nil && old.m != nil {
			base = maps.Clone(old.m)
		}

		stat := base[linkKey]
		firstObservation := stat.Count == 0

		stat.RewardSum += reinforcement
		stat.ResidualSum += residual
		stat.Count++

		/*
			Keep the first-seen terminal IDs so causalPathWeight reflects the
			initial coactivation pairings; later updates only accumulate reward mass.
		*/
		if firstObservation {
			stat.SensoryID = sensoryID
			stat.ActionID = actionID
			stat.RewardID = rewardID
			stat.SensoryAffinity = sensoryAffinity
			stat.ActionAffinity = actionAffinity
			stat.RewardAffinity = rewardAffinity
			stat.SensoryVector = sensoryVector
			stat.ActionVector = actionVector
			stat.RewardVector = rewardVector
		}

		base[linkKey] = stat

		next := &coactivationMap{m: base}

		if coordinator.coactivation.CompareAndSwap(old, next) {
			coordinator.actionAlign.Store(newDomainAlignment(base))

			return
		}
	}
}

func (coordinator *MultimodalCoordinator) causalRegimeLabels(
	causalRegimes []string,
) []string {
	labels := make([]string, 0, len(causalRegimes))

	for _, label := range causalRegimes {
		label = strings.TrimSpace(label)

		if label != "" {
			labels = append(labels, label)
		}
	}

	if len(labels) > 0 {
		return labels
	}

	return []string{"default"}
}

func (coordinator *MultimodalCoordinator) predictedRewardAffinity(
	sensorySequence string,
	actionSequence string,
) ([primitive.AffinityWords]uint64, bool) {
	var best [primitive.AffinityWords]uint64

	if coordinator == nil || sensorySequence == "" || actionSequence == "" {
		return best, false
	}

	snapshot := coordinator.coactivation.Load()

	if snapshot == nil || len(snapshot.m) == 0 {
		return best, false
	}

	bestSupport := 0.0

	for linkKey, stat := range snapshot.m {
		sensoryCandidate, actionCandidate, _, ok := coordinator.parseLinkKey(linkKey)

		if !ok || sensoryCandidate != sensorySequence || actionCandidate != actionSequence {
			continue
		}

		if stat.Count <= bestSupport {
			continue
		}

		best = stat.RewardAffinity
		bestSupport = stat.Count
	}

	return best, bestSupport > 0
}

func (coordinator *MultimodalCoordinator) observeRewardResidual(
	predictedAffinity [primitive.AffinityWords]uint64,
	observed primitive.Value,
) float64 {
	if coordinator == nil || coordinator.causal == nil {
		return 0
	}

	predicted := observed
	predicted.SetAffinityVector(predictedAffinity)

	observedCopy := observed
	coordinator.causal.ObserveResidual(&predicted, &observedCopy)

	return coordinator.affinityResidualDistance(
		predictedAffinity,
		observed.AffinityVector(),
	)
}

func (coordinator *MultimodalCoordinator) affinityResidualDistance(
	predicted [primitive.AffinityWords]uint64,
	observed [primitive.AffinityWords]uint64,
) float64 {
	distance := 0

	for wordIdx := range primitive.AffinityWords {
		xor := predicted[wordIdx] ^ observed[wordIdx]

		if wordIdx == primitive.AffinityWords-1 {
			xor &= primitive.AffinityLastWordMask
		}

		distance += bits.OnesCount64(xor)
	}

	return float64(distance)
}

func (coordinator *MultimodalCoordinator) residualConfidence(
	stat coactivationStat,
) float64 {
	if stat.Count <= 0 || stat.ResidualSum <= 0 {
		return 1
	}

	meanResidual := stat.ResidualSum / stat.Count
	normalized := meanResidual / float64(primitive.AffinityBits)

	return 1 / (1 + normalized)
}

func (coordinator *MultimodalCoordinator) parseLinkKey(
	linkKey string,
) (sensorySequence string, actionSequence string, rewardSequence string, ok bool) {
	if linkKey == "" {
		return "", "", "", false
	}

	sensorySequence, rest, found := strings.Cut(linkKey, "\x1f")

	if !found {
		return "", "", "", false
	}

	actionSequence, rewardSequence, found = strings.Cut(rest, "\x1f")

	if !found {
		return "", "", "", false
	}

	return sensorySequence, actionSequence, rewardSequence, true
}

func (coordinator *MultimodalCoordinator) sensoryCandidateWeights(
	input string,
	prediction *algo.Prediction,
) map[string]float64 {
	candidates := map[string]float64{
		input: 1.0,
	}

	if prediction == nil || len(prediction.Continuations) == 0 {
		return candidates
	}

	maxScore := prediction.Continuations[0].Score

	for _, continuation := range prediction.Continuations {
		if continuation.Score > maxScore {
			maxScore = continuation.Score
		}
	}

	total := 0.0

	for _, continuation := range prediction.Continuations {
		sequence := string(continuation.Sequence)

		if sequence == "" {
			continue
		}

		weight := math.Exp(continuation.Score - maxScore)
		candidates[sequence] += weight
		total += weight
	}

	if total <= 0 {
		return candidates
	}

	for sequence, weight := range candidates {
		if sequence == input {
			continue
		}

		candidates[sequence] = weight / total
	}

	return candidates
}

func (coordinator *MultimodalCoordinator) causalPathWeight(
	sensoryID uint64,
	actionID uint64,
	rewardID uint64,
) float64 {
	if coordinator == nil || coordinator.causal == nil {
		return 0
	}

	sensoryAction := 0.0
	actionReward := 0.0

	if sensoryID != 0 && actionID != 0 {
		sensoryAction = coordinator.causal.EdgeReliability(sensoryID, actionID)
	}

	if actionID != 0 && rewardID != 0 {
		actionReward = coordinator.causal.EdgeReliability(actionID, rewardID)
	}

	if sensoryAction <= 0 || actionReward <= 0 {
		return 0
	}

	return math.Min(sensoryAction, actionReward)
}
