package vm

import (
	"context"
	"math"
	"math/bits"
	"runtime"
	"sync"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/programmer"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/gossip"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/viz"
)

/*
Orchestrator is a component that orchestrates the different components of the machine.
*/
type Orchestrator struct {
	ctx     context.Context
	cancel  context.CancelFunc
	err     error
	conn    *gossip.Conn
	queue   *pool.Queue
	field   *geometry.Field
	pending []*primitive.Value
	mu      sync.Mutex
}

/*
NewOrchestrator creates a new orchestrator.
*/
func NewOrchestrator(ctx context.Context, conn *gossip.Conn, queue *pool.Queue) (*Orchestrator, error) {
	ctx, cancel := context.WithCancel(ctx)

	orchestrator := &Orchestrator{
		ctx:    ctx,
		cancel: cancel,
		conn:   conn,
		queue:  queue,
		field:  geometry.NewField(geometry.Mod65537),
	}

	go orchestrator.run()

	return orchestrator, validate.Require(map[string]any{
		"ctx":    orchestrator.ctx,
		"cancel": orchestrator.cancel,
		"conn":   orchestrator.conn,
		"queue":  orchestrator.queue,
		"field":  orchestrator.field,
	})
}

func (orchestrator *Orchestrator) run() {
	for {
		select {
		case <-orchestrator.ctx.Done():
			return
		default:
			orchestrator.Cycle()
			// Yield to prevent tight loop if nothing to do
			runtime.Gosched()
		}
	}
}

/*
Close the orchestrator.
*/
func (orchestrator *Orchestrator) Close() error {
	orchestrator.cancel()
	return orchestrator.err
}

/*
Error returns the error of the orchestrator.
*/
func (orchestrator *Orchestrator) Error() error {
	return orchestrator.err
}

/*
Publish implements the Publishable interface.
It adds the value to the pending list for the next Cycle.
*/
func (orchestrator *Orchestrator) Publish(value *primitive.Value, label string) error {
	orchestrator.mu.Lock()
	defer orchestrator.mu.Unlock()

	orchestrator.pending = append(orchestrator.pending, value)
	return nil
}

/*
Cycle the system so everything goes through another cycle of processing. This should
not be seen as "stepping" given that there are no guarantees about the order of
operations, or predictable consistency timing.
*/
func (orchestrator *Orchestrator) Cycle(values ...*primitive.Value) error {
	orchestrator.mu.Lock()

	// Add any explicitly passed values
	if len(values) > 0 {
		orchestrator.pending = append(orchestrator.pending, values...)
	}

	// Filter for settled values
	var settled []*primitive.Value
	var stillPending []*primitive.Value

	for _, value := range orchestrator.pending {
		if value == nil {
			continue
		}

		if (*value)[kernel.SchedulingNextProgramWord] == 0 {
			settled = append(settled, value)
		} else {
			stillPending = append(stillPending, value)
		}
	}

	orchestrator.pending = stillPending
	orchestrator.mu.Unlock()

	if len(settled) > 0 {
		orchestrator.findCommunity(settled)
	}

	orchestrator.emitActions()

	return orchestrator.err
}

/*
emitActions iterates over communities, evaluates the beams (Values) they passed up,
and provides top-down feedback: rewarding successful compositions with attention/bias,
and breaking unsuccessful ones to avoid local minima.
*/
func (orchestrator *Orchestrator) emitActions() {
	for i, community := range orchestrator.field.Fields {
		if community == nil || len(community.Values) == 0 {
			continue
		}

		dominant := community.Dominant()
		resonance := dominant.Concentration

		// The field provides top-down feedback to the Values (beams)
		var successful []*primitive.Value
		var unsuccessful []*primitive.Value

		for _, val := range community.Values {
			// Calculate alignment of Value's affinity with the community's aggregate affinity
			distance := 0
			for j := 0; j < 5; j++ {
				distance += bits.OnesCount64(community.Affinity[j] ^ (*val)[kernel.AffinityStartWord+j])
			}

			// If the distance is small, the beam was successfully composed into the field
			if distance < 32 {
				successful = append(successful, val)
			} else {
				unsuccessful = append(unsuccessful, val)
			}
		}

		// Reward successful beams with attention/bias (e.g., boosting TTL or applying affine rotation)
		for _, val := range successful {
			ttl := (*val)[kernel.PropertiesTTLWord]
			if ttl > 0 && ttl < 255 {
				val.Set(kernel.PropertiesTTLWord, ttl+1) // Reward
			}

			// Apply a positive affine rotation as attention mechanism
			// This rotation is reversible via the phasedial if needed later
			community.Rotate(2, 1)
		}

		// Break unsuccessful beams so they don't get stuck in local minima
		for _, val := range unsuccessful {
			// Send a top-down break signal by zeroing the TTL, terminating the cascade
			val.Set(kernel.PropertiesTTLWord, 0)
		}

		if resonance > 0.25 && len(successful) > 0 {
			// Emit an action based on the successful composition
			actionBytes := make([]byte, 16)

			for j := range 16 {
				if j < len(community.Fields) && community.Fields[j] != nil {
					laneVal := community.Fields[j].Dominant().Amplitude
					actionBytes[j] = byte(laneVal & 0xff)
				}
			}

			actionValues, err := primitive.NewValue(actionBytes)

			if err == nil && len(actionValues) > 0 {
				action := actionValues[0]

				progName := "beam_swarm_step"
				if dominant.Index%2 == 0 {
					progName = "active_inference"
				}

				installer := programmer.Installer{}
				if err := installer.InstallProgram(action, progName); err == nil {
					_ = orchestrator.queue.Publish(action, progName)

					if viz.DefaultBus.IsActive() {
						viz.DefaultBus.Publish(viz.CommunityActionEvent(i, action.ID(), progName, resonance))
					}
				}
			}
		}

		// Clear community values after processing this cycle's beams
		community.Values = nil
	}
}

/*
findCommunity finds the community that a value belongs to.
*/
func (orchestrator *Orchestrator) findCommunity(values []*primitive.Value) {
	for _, value := range values {
		if value == nil {
			continue
		}

		valueAffinity := make([]uint64, 5)

		for i := range 5 {
			valueAffinity[i] = (*value)[kernel.AffinityStartWord+i]
		}

		var bestCommunityIdx int
		var bestCommunity *geometry.Field
		bestDistance := math.MaxInt32

		// Find the closest non-saturated community
		for i, community := range orchestrator.field.Fields {
			if community == nil {
				continue
			}

			if community.AffinitySaturation() >= 0.47 {
				if viz.DefaultBus.IsActive() {
					viz.DefaultBus.Publish(viz.CommunitySaturatedEvent(i, community.AffinitySaturation()))
				}
				continue
			}

			distance := 0
			for j := 0; j < 5; j++ {
				distance += bits.OnesCount64(community.Affinity[j] ^ valueAffinity[j])
			}

			if distance < bestDistance {
				bestDistance = distance
				bestCommunity = community
				bestCommunityIdx = i
			}
		}

		// Threshold for joining a community (e.g., 64 bits difference max)
		if bestCommunity != nil && bestDistance < 64 {
			bestCommunity.Values = append(bestCommunity.Values, value)
			bestCommunity.MergeAffinity(valueAffinity)

			if viz.DefaultBus.IsActive() {
				viz.DefaultBus.Publish(viz.ValueJoinedCommunityEvent(value.ID(), bestCommunityIdx, bestDistance))
			}
		} else {
			// Spawn a new community in the first available slot
			for i, slot := range orchestrator.field.Fields {
				if slot == nil {
					newCommunity := geometry.NewField(geometry.Mod8191)
					newCommunity.MergeAffinity(valueAffinity)
					newCommunity.Values = append(newCommunity.Values, value)
					orchestrator.field.Fields[i] = newCommunity

					if viz.DefaultBus.IsActive() {
						viz.DefaultBus.Publish(viz.CommunityCreatedEvent(i, valueAffinity))
						viz.DefaultBus.Publish(viz.ValueJoinedCommunityEvent(value.ID(), i, 0))
					}
					break
				}
			}
		}
	}
}
