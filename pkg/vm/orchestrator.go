package vm

import (
	"context"
	"io"

	"github.com/theapemachine/six/pkg/compute/programmer"
	"github.com/theapemachine/six/pkg/core"
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
	ctx      context.Context
	cancel   context.CancelFunc
	err      error
	conn     *gossip.Conn
	queue    *pool.Queue
	field    *geometry.Field
	firmware *programmer.Firmware
	linker   *Linker
	route    gossip.PriorityRoute
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
		ctx:      ctx,
		cancel:   cancel,
		conn:     conn,
		queue:    queue,
		field:    geometry.NewField(geometry.Mod65537),
		firmware: programmer.NewFirmware(),
		linker:   NewLinker(),
		route:    make(gossip.PriorityRoute, 0),
	}

	if conn != nil {
		orchestrator.route.AddPeer(conn, conn.Affinity())
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

	return orchestrator, nil
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
func (orchestrator *Orchestrator) Publish(values ...*primitive.Value) ([]*primitive.Value, error) {
	return orchestrator.Cycle(values...)
}

/*
Cycle runs one full processing pass: route incoming Values into communities,
cycle the global field (which cascades into each community's eigenmode
detection and rotational pressure), then evaluate every community for
gap resolution and emission readiness. Returns the Values whose belief gap
has dropped below epsilon — the stop condition that tells the experiment
pipeline the system has resolved the prompt.
*/
func (orchestrator *Orchestrator) Cycle(
	values ...*primitive.Value,
) ([]*primitive.Value, error) {
	orchestrator.linker.Push(values...)

	for {
		value, assets := orchestrator.linker.Pop()

		if value == nil {
			break
		}

		firmware := orchestrator.firmware.Next(value)

		if firmware == "" {
			orchestrator.routeValue(value)
			continue
		}

		// Only pass assets if the firmware actually requires them (like "link").
		// If other firmwares don't need them, we could conditionally pass nil,
		// but since the asset region is just scratch space, passing it is harmless.
		if firmware != "link" {
			assets = nil
		}

		orchestrator.queue.Submit(func() {
			executable := programmer.NewExecutable(
				value, firmware, assets,
			)

			executable.Compile(programmer.CPU)
		})
	}

	orchestrator.route.Reorder()

	if _, err := orchestrator.field.Cycle(); err != nil {
		return nil, err
	}

	active := viz.DefaultBus.IsActive()
	epsilon := core.Cfg.System.BeliefEpsilon
	stateIdx := core.Cfg.Value.Region.Properties.Start + int(primitive.STATE)

	var resolved []*primitive.Value

	for slotIndex, community := range orchestrator.field.Fields {
		if community == nil || len(community.Values) < 2 {
			continue
		}

		dominant := community.Dominant()

		if active && dominant.Index >= 0 {
			viz.DefaultBus.Publish(viz.EigenmodeDetected(
				0, community.MemberCount(), dominant.Concentration,
			))
		}

		for _, value := range community.Values {
			if value == nil {
				continue
			}

			affinity := value.Get(primitive.AffinityRegion)

			valueID := value.ID()
			gap := community.BeliefGap(affinity)

			if active {
				viz.DefaultBus.Publish(viz.BeliefGapEvaluatedEvent(
					valueID, slotIndex, gap,
				))
			}

			if gap <= epsilon {
				(*value)[stateIdx] = uint64(primitive.RESOLVED)
				resolved = append(resolved, value)

				if active {
					viz.DefaultBus.Publish(viz.ValueResolvedEvent(
						valueID, slotIndex, gap,
					))
				}
			}
		}
	}

	return resolved, nil
}

/*
Flush forces the linker to pop all remaining Values and processes them.
*/
func (orchestrator *Orchestrator) Flush() {
	for {
		value, assets := orchestrator.linker.Flush()

		if value == nil {
			break
		}

		firmware := orchestrator.firmware.Next(value)

		if firmware == "" {
			orchestrator.routeValue(value)
			continue
		}

		if firmware != "link" {
			assets = nil
		}

		orchestrator.queue.Submit(func() {
			executable := programmer.NewExecutable(
				value, firmware, assets,
			)

			executable.Compile(programmer.CPU)
		})
	}
}

/*
routeValue routes a settled Value to the most appropriate community field
using the gossip-based fast path. It wraps the PriorityRoute in an
AffinityFilter and copies the Value. If no community accepts the Value,
a new community is spawned and added to the route.
*/
func (orchestrator *Orchestrator) routeValue(value *primitive.Value) {
	affinity := value.Get(primitive.AffinityRegion)
	var frameAffinity [5]uint64
	copy(frameAffinity[:], affinity)

	// Try to route to an existing community in priority order
	for _, peer := range orchestrator.route {
		dist := geometry.AffinityHammingDistance(frameAffinity[:], peer.Affinity[:])
		if dist <= 10 { // budget
			// We found a community within budget. Use AffinityFilter to write it.
			// Since we already checked the distance, we could just write directly to peer.Dst,
			// but we'll use AffinityFilter to follow the proposal's composable I/O pattern.
			filter := gossip.NewAffinityFilter(peer.Dst, peer.Affinity, 10)

			// Value implements io.Reader, so we can use io.Copy.
			// However, io.Copy will consume the Value's reader.
			// Since we only want to write it once, and value.Read returns EOF after one frame, this is perfect.
			_, _ = io.Copy(filter, value)

			// Find the community ID for the visualizer
			communityID := -1
			for i, c := range orchestrator.field.Fields {
				if c == peer.Dst {
					communityID = i
					break
				}
			}
			if communityID != -1 {
				viz.DefaultBus.Publish(viz.ValueJoinedCommunityEvent(value.ID(), communityID, dist))
			}

			return
		}
	}

	// If no community found or all too far, create a new GF(8191) community
	newCommunity := geometry.NewField(geometry.Mod8191)
	newCommunity.Affinity = make([]uint64, 5)
	copy(newCommunity.Affinity, frameAffinity[:])

	// Add it to the global field's children
	placed := false
	communityID := -1
	for i, c := range orchestrator.field.Fields {
		if c == nil {
			orchestrator.field.Fields[i] = newCommunity
			placed = true
			communityID = i
			break
		}
	}
	if !placed {
		communityID = len(orchestrator.field.Fields)
		orchestrator.field.Fields = append(orchestrator.field.Fields, newCommunity)
	}

	viz.DefaultBus.Publish(viz.CommunityCreatedEvent(communityID, frameAffinity[:]))

	// Add it to our PriorityRoute
	orchestrator.route.AddPeer(newCommunity, frameAffinity)

	// Route the value to the newly created community
	filter := gossip.NewAffinityFilter(newCommunity, frameAffinity, 10)
	_, _ = io.Copy(filter, value)

	viz.DefaultBus.Publish(viz.ValueJoinedCommunityEvent(value.ID(), communityID, 0))
}
