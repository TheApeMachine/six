package markovtrie

import (
	"strconv"
	"sync"
)

/*
MultimodalCoordinator binds sensory, action, and reward tries while tracking
coactivated terminal nodes so reward expectations can be queried from paired
sensory-action histories without a separate simulator graph.
*/
type MultimodalCoordinator struct {
	Sensory *Store
	Action  *Store
	Reward  *Store

	coactivationMu sync.RWMutex
	coactivation   map[string]float64
}

/*
NewMultimodalCoordinator constructs three independent stores that share option
parity but maintain isolated trie roots and vocabularies.
*/
func NewMultimodalCoordinator(options ...Option) *MultimodalCoordinator {
	return &MultimodalCoordinator{
		Sensory:      NewStore(options...),
		Action:       NewStore(options...),
		Reward:       NewStore(options...),
		coactivation: make(map[string]float64),
	}
}

func multimodalLinkKey(sensoryID string, actionID string, rewardID string) string {
	return sensoryID + "\x1f" + actionID + "\x1f" + rewardID
}

/*
TrainStep walks every stream under one label, applies the same learningRate, and
records how often the resulting deepest nodes line up.
*/
func (coordinator *MultimodalCoordinator) TrainStep(
	sensorySequence string,
	actionSequence string,
	rewardSignal float64,
	label string,
	learningRate float64,
) {
	if coordinator == nil {
		return
	}

	rewardSequence := strconv.FormatFloat(rewardSignal, 'g', 6, 64)

	if coordinator.Sensory != nil {
		coordinator.Sensory.Train(sensorySequence, label, learningRate)
	}

	if coordinator.Action != nil {
		coordinator.Action.Train(actionSequence, label, learningRate)
	}

	if coordinator.Reward != nil {
		coordinator.Reward.Train(rewardSequence, label, learningRate)
	}

	if coordinator.Sensory == nil || coordinator.Action == nil || coordinator.Reward == nil {
		return
	}

	sensoryID := coordinator.Sensory.DeepestNodeID(sensorySequence)
	actionID := coordinator.Action.DeepestNodeID(actionSequence)
	rewardID := coordinator.Reward.DeepestNodeID(rewardSequence)

	key := multimodalLinkKey(sensoryID, actionID, rewardID)

	coordinator.coactivationMu.Lock()
	coordinator.coactivation[key] += learningRate
	coordinator.coactivationMu.Unlock()
}

/*
CoactivationStrength returns accumulated plasticity mass on the synchronous
triple for one sensory-action-reward tuple.
*/
func (coordinator *MultimodalCoordinator) CoactivationStrength(
	sensorySequence string,
	actionSequence string,
	rewardSignal float64,
) float64 {
	if coordinator == nil {
		return 0
	}

	if coordinator.Sensory == nil || coordinator.Action == nil || coordinator.Reward == nil {
		return 0
	}

	rewardSequence := strconv.FormatFloat(rewardSignal, 'g', 6, 64)

	key := multimodalLinkKey(
		coordinator.Sensory.DeepestNodeID(sensorySequence),
		coordinator.Action.DeepestNodeID(actionSequence),
		coordinator.Reward.DeepestNodeID(rewardSequence),
	)

	coordinator.coactivationMu.RLock()
	strength := coordinator.coactivation[key]
	coordinator.coactivationMu.RUnlock()

	return strength
}
