package markovtrie

import (
	"context"
	"errors"
	"maps"
	"math"
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
RewardSum accumulates signed reinforcement; Count tracks how many observations
contributed to that sum so expected reward can be recovered lazily.
*/
type coactivationStat struct {
	RewardSum float64
	Count     float64
	SensoryID uint64
	ActionID  uint64
	RewardID  uint64
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

	coordinator.updateCoactivation(
		linkKey,
		reinforcement,
		sensory.ID(),
		action.ID(),
		reward.ID(),
	)

	targetLabel := coordinator.rewardTargetLabel(reinforcement, rewardLabels...)

	if targetLabel != "" && coordinator.causal != nil {
		causalPrediction := algo.NewPrediction()
		causalPrediction.AddTargets(algo.Label{
			Label:      []byte(targetLabel),
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
neighboring states.
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

		if !matched || stat.Count <= 0 {
			continue
		}

		expectedReward := stat.RewardSum / stat.Count
		causalWeight := coordinator.causalPathWeight(
			stat.SensoryID,
			stat.ActionID,
			stat.RewardID,
		)

		actionValues[actionSequence] += weight * expectedReward * (1.0 + causalWeight)
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
	sensoryID uint64,
	actionID uint64,
	rewardID uint64,
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
		stat.RewardSum += reinforcement
		stat.Count++
		stat.SensoryID = sensoryID
		stat.ActionID = actionID
		stat.RewardID = rewardID
		base[linkKey] = stat

		next := &coactivationMap{m: base}

		if coordinator.coactivation.CompareAndSwap(old, next) {
			return
		}
	}
}

func (coordinator *MultimodalCoordinator) rewardTargetLabel(
	reinforcement float64,
	rewardLabels ...string,
) string {
	for _, label := range rewardLabels {
		label = strings.TrimSpace(label)

		if label != "" {
			return label
		}
	}

	if reinforcement > 0 {
		return "positive"
	}

	if reinforcement < 0 {
		return "negative"
	}

	return "neutral"
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

	total := 0.0
	count := 0.0

	if sensoryID != 0 && actionID != 0 {
		weight := coordinator.causal.EdgeInvariance(sensoryID, actionID)

		if weight > 0 {
			total += weight
			count++
		}
	}

	if actionID != 0 && rewardID != 0 {
		weight := coordinator.causal.EdgeInvariance(actionID, rewardID)

		if weight > 0 {
			total += weight
			count++
		}
	}

	if count == 0 {
		return 0
	}

	return total / count
}
