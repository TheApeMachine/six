package vm

import (
	"context"
	"math"
	"math/bits"
	"time"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/programmer"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/gossip"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/viz"
)

/*
Orchestrator is a component that orchestrates the different components of the machine.
*/
type Orchestrator struct {
	ctx          context.Context
	cancel       context.CancelFunc
	err          error
	conn         *gossip.Conn
	queue        *pool.Queue
	field        *geometry.Field
	pendingCh    chan *primitive.Value
	programReady chan struct{}
}

/*
NewOrchestrator creates a new orchestrator.

programReady, when non-nil, receives a coalesced wakeup whenever a cycle
observes at least one settled value (SchedulingNextProgramWord zeroed) so
Machine.Prompt can block instead of spinning until the kernel clears scheduling.
*/
func NewOrchestrator(
	ctx context.Context,
	conn *gossip.Conn,
	queue *pool.Queue,
	programReady chan struct{},
) (*Orchestrator, error) {
	ctx, cancel := context.WithCancel(ctx)

	orchestrator := &Orchestrator{
		ctx:          ctx,
		cancel:       cancel,
		conn:         conn,
		queue:        queue,
		field:        geometry.NewField(geometry.Mod65537),
		pendingCh:    make(chan *primitive.Value, 262144),
		programReady: programReady,
	}

	if err := validate.Require(map[string]any{
		"ctx":    orchestrator.ctx,
		"cancel": orchestrator.cancel,
		"conn":   orchestrator.conn,
		"queue":  orchestrator.queue,
		"field":  orchestrator.field,
	}); err != nil {
		cancel()

		return nil, err
	}

	go orchestrator.run()

	return orchestrator, nil
}

func (orchestrator *Orchestrator) signalProgramReady() {
	if orchestrator.programReady == nil {
		return
	}

	select {
	case orchestrator.programReady <- struct{}{}:
	default:
	}
}

func (orchestrator *Orchestrator) run() {
	idle := time.NewTicker(4 * time.Millisecond)

	defer idle.Stop()

	for {
		select {
		case <-orchestrator.ctx.Done():
			return

		case value := <-orchestrator.pendingCh:
			orchestrator.Cycle(value)

		case <-idle.C:
			orchestrator.Cycle()
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
It sends the value on the pending queue for the next Cycle.
*/
func (orchestrator *Orchestrator) Publish(value *primitive.Value, label string) error {
	select {
	case orchestrator.pendingCh <- value:
		return nil
	case <-orchestrator.ctx.Done():
		return orchestrator.ctx.Err()
	}
}

/*
Cycle the system so everything goes through another cycle of processing. This should
not be seen as "stepping" given that there are no guarantees about the order of
operations, or predictable consistency timing.
*/
func (orchestrator *Orchestrator) Cycle(values ...*primitive.Value) error {
	var batch []*primitive.Value

	batch = append(batch, values...)

	for {
		select {
		case value := <-orchestrator.pendingCh:
			batch = append(batch, value)
		default:
			goto drained
		}
	}

drained:

	var settled []*primitive.Value
	var stillPending []*primitive.Value

	for _, value := range batch {
		if value == nil {
			continue
		}

		if (*value)[kernel.SchedulingNextProgramWord] == 0 {
			settled = append(settled, value)

			continue
		}

		stillPending = append(stillPending, value)
	}

	for _, value := range stillPending {
		select {
		case orchestrator.pendingCh <- value:
		case <-orchestrator.ctx.Done():
			orchestrator.err = orchestrator.ctx.Err()

			return orchestrator.err
		}
	}

	if len(settled) > 0 {
		orchestrator.signalProgramReady()
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
	for communityIndex, community := range orchestrator.field.Fields {
		if community == nil || len(community.Values) == 0 {
			continue
		}

		dominant := community.Dominant()
		resonance := dominant.Concentration

		var successful []*primitive.Value
		var unsuccessful []*primitive.Value

		for _, valuePointer := range community.Values {
			distance := 0

			for affinityWordIndex := 0; affinityWordIndex < 5; affinityWordIndex++ {
				distance += bits.OnesCount64(
					community.Affinity[affinityWordIndex] ^ (*valuePointer)[kernel.AffinityStartWord+affinityWordIndex],
				)
			}

			if distance < 32 {
				successful = append(successful, valuePointer)

				continue
			}

			unsuccessful = append(unsuccessful, valuePointer)
		}

		for _, valuePointer := range successful {
			ttl := (*valuePointer)[kernel.PropertiesTTLWord]

			if ttl > 0 && ttl < 255 {
				valuePointer.Set(kernel.PropertiesTTLWord, ttl+1)
			}

			community.Rotate(2, 1)
		}

		for _, valuePointer := range unsuccessful {
			valuePointer.Set(kernel.PropertiesTTLWord, 0)
		}

		if resonance <= 0.25 || len(successful) == 0 {
			community.Values = nil

			continue
		}

		actionBytes := make([]byte, 16)

		for laneIndex := range 16 {
			if laneIndex < len(community.Fields) && community.Fields[laneIndex] != nil {
				laneVal := community.Fields[laneIndex].Dominant().Amplitude
				actionBytes[laneIndex] = byte(laneVal & 0xff)
			}
		}

		actionValues, newValueErr := primitive.NewValue(actionBytes)

		if newValueErr != nil {
			errnie.Warn(
				"vm.orchestrator.emitActions: primitive.NewValue",
				"err", newValueErr,
				"community", communityIndex,
			)
			community.Values = nil

			continue
		}

		if len(actionValues) == 0 {
			community.Values = nil

			continue
		}

		action := actionValues[0]

		progName := "beam_swarm_step"

		if dominant.Index%2 == 0 {
			progName = "active_inference"
		}

		installer := programmer.Installer{}

		if installErr := installer.InstallProgram(action, progName); installErr != nil {
			errnie.Warn(
				"vm.orchestrator.emitActions: programmer.Installer.InstallProgram",
				"err", installErr,
				"community", communityIndex,
				"program", progName,
			)
			community.Values = nil

			continue
		}

		if publishErr := orchestrator.queue.Publish(action, progName); publishErr != nil {
			errnie.Warn(
				"vm.orchestrator.emitActions: orchestrator.queue.Publish",
				"err", publishErr,
				"community", communityIndex,
				"program", progName,
			)
		}

		if viz.DefaultBus.IsActive() {
			viz.DefaultBus.Publish(viz.CommunityActionEvent(communityIndex, action.ID(), progName, resonance))
		}

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

		for affinityWordIndex := range 5 {
			valueAffinity[affinityWordIndex] = (*value)[kernel.AffinityStartWord+affinityWordIndex]
		}

		var bestCommunityIdx int
		var bestCommunity *geometry.Field

		bestDistance := math.MaxInt32

		for communityIndex, community := range orchestrator.field.Fields {
			if community == nil {
				continue
			}

			if community.AffinitySaturation() >= 0.47 {
				if viz.DefaultBus.IsActive() {
					viz.DefaultBus.Publish(viz.CommunitySaturatedEvent(communityIndex, community.AffinitySaturation()))
				}

				continue
			}

			distance := 0

			for affinityWordIndex := 0; affinityWordIndex < 5; affinityWordIndex++ {
				distance += bits.OnesCount64(community.Affinity[affinityWordIndex] ^ valueAffinity[affinityWordIndex])
			}

			if distance < bestDistance {
				bestDistance = distance
				bestCommunity = community
				bestCommunityIdx = communityIndex
			}
		}

		if bestCommunity != nil && bestDistance < 64 {
			bestCommunity.Values = append(bestCommunity.Values, value)
			bestCommunity.MergeAffinity(valueAffinity)

			if viz.DefaultBus.IsActive() {
				viz.DefaultBus.Publish(viz.ValueJoinedCommunityEvent(value.ID(), bestCommunityIdx, bestDistance))
			}

			continue
		}

		placed := false

		for slotIndex, slot := range orchestrator.field.Fields {
			if slot != nil {
				continue
			}

			newCommunity := geometry.NewField(geometry.Mod8191)
			newCommunity.MergeAffinity(valueAffinity)
			newCommunity.Values = append(newCommunity.Values, value)
			orchestrator.field.Fields[slotIndex] = newCommunity
			placed = true

			if viz.DefaultBus.IsActive() {
				viz.DefaultBus.Publish(viz.CommunityCreatedEvent(slotIndex, valueAffinity))
				viz.DefaultBus.Publish(viz.ValueJoinedCommunityEvent(value.ID(), slotIndex, 0))
			}

			break
		}

		if !placed {
			errnie.Warn(
				"vm.orchestrator.findCommunity: no empty field slot; value not placed in field",
				"value_id", value.ID(),
				"best_distance", bestDistance,
			)
		}
	}
}
