package markovtrie

import (
	"context"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
)

/*
float64StringMap holds coactivation weights keyed by LinkKey strings.
It is swapped atomically so multimodal bookkeeping stays mutex-free.
*/
type float64StringMap struct {
	m map[string]float64
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
	coactivation atomic.Pointer[float64StringMap]
}

/*
NewMultimodalCoordinator constructs three independent stores that share option
parity but maintain isolated trie roots and vocabularies.
*/
func NewMultimodalCoordinator(
	ctx context.Context, options ...Option,
) (coordinator *MultimodalCoordinator, err error) {
	ctx, cancel := context.WithCancel(ctx)

	initial := &float64StringMap{m: make(map[string]float64)}

	coordinator = &MultimodalCoordinator{
		ctx:    ctx,
		cancel: cancel,
	}

	coordinator.coactivation.Store(initial)

	if coordinator.Sensory, err = NewStore(ctx, options...); err != nil {
		cancel()
		return nil, errnie.Error(err)
	}

	if coordinator.Action, err = NewStore(ctx, options...); err != nil {
		cancel()
		return nil, errnie.Error(err)
	}

	if coordinator.Reward, err = NewStore(ctx, options...); err != nil {
		cancel()
		return nil, errnie.Error(err)
	}

	return coordinator, validate.Require(map[string]any{
		"ctx":          coordinator.ctx,
		"cancel":       coordinator.cancel,
		"Sensory":      coordinator.Sensory,
		"Action":       coordinator.Action,
		"Reward":       coordinator.Reward,
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
