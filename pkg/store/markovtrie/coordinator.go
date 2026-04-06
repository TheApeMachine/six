package markovtrie

import (
	"context"
	"sync"

	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
)

/*
MultimodalCoordinator binds sensory, action, and reward tries while tracking
coactivated terminal nodes so reward expectations can be queried from paired
sensory-action histories without a separate simulator graph.
*/
type MultimodalCoordinator struct {
	ctx     context.Context
	cancel  context.CancelFunc
	err     error
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
func NewMultimodalCoordinator(
	ctx context.Context, options ...Option,
) (*MultimodalCoordinator, error) {
	ctx, cancel := context.WithCancel(ctx)

	coordinator := &MultimodalCoordinator{
		ctx:          ctx,
		cancel:       cancel,
		coactivation: make(map[string]float64),
	}

	var err error

	coordinator.Sensory, err = NewStore(ctx, options...)

	if err != nil {
		cancel()

		return nil, errnie.Error(err)
	}

	coordinator.Action, err = NewStore(ctx, options...)

	if err != nil {
		cancel()

		return nil, errnie.Error(err)
	}

	coordinator.Reward, err = NewStore(ctx, options...)

	if err != nil {
		cancel()

		return nil, errnie.Error(err)
	}

	return coordinator, validate.Require(map[string]any{
		"ctx":          coordinator.ctx,
		"cancel":       coordinator.cancel,
		"Sensory":      coordinator.Sensory,
		"Action":       coordinator.Action,
		"Reward":       coordinator.Reward,
		"coactivation": coordinator.coactivation,
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
