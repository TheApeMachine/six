package vm

import (
	"context"
	"math"
	"unsafe"

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
	ctx          context.Context
	cancel       context.CancelFunc
	err          error
	conn         *gossip.Conn
	queue        *pool.Queue
	field        *geometry.Field
	unsupervised *Unsupervised
}

/*
affinityMatchBitBudget is the maximum Hamming distance (in bits) between a
Value’s affinity words and a community’s aggregate for the Value to join that
community instead of starting a new one.
*/
const affinityMatchBitBudget = 64

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
		unsupervised: NewUnsupervised(queue),
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
	orchestrator.route(values...)

	if _, err := orchestrator.field.Cycle(); err != nil {
		return nil, err
	}

	active := viz.DefaultBus.IsActive()
	epsilon := core.Cfg.System.BeliefEpsilon
	stateIdx := core.Cfg.Value.Region.Properties.Start + 4

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

			affinity := affinityWordsFromValue(value)

			if affinityIsEmpty(affinity) {
				continue
			}

			valueID := (*value)[core.Cfg.Value.Region.ID.Start]
			gap := community.BeliefGap(affinity)

			if active {
				viz.DefaultBus.Publish(viz.BeliefGapEvaluatedEvent(
					valueID, slotIndex, gap,
				))
			}

			if gap <= epsilon {
				(*value)[stateIdx] = 4
				resolved = append(resolved, value)

				if active {
					viz.DefaultBus.Publish(viz.ValueResolvedEvent(
						valueID, slotIndex, gap,
					))
				}
			} else if (*value)[stateIdx] == 4 {
				resolved = append(resolved, value)
			}
		}

		if !community.EmissionReady() {
			continue
		}

		if active {
			viz.DefaultBus.Publish(viz.CommunityEmissionEvent(
				slotIndex, community.MemberCount(), dominant.Concentration,
			))
		}

		emitted := orchestrator.emitFromCommunity(community)

		if emitted != nil {
			orchestrator.queue.Publish(emitted)

			if active {
				emittedID := (*emitted)[core.Cfg.Value.Region.ID.Start]
				viz.DefaultBus.Publish(viz.CommunityActionEvent(
					slotIndex, emittedID, "aggregate", dominant.Concentration,
				))
			}
		}

		community.Values = community.Values[:0]
	}

	return resolved, nil
}

/*
Label runs one unsupervised labeling pass over all communities in the root
field. It is intended to be called exactly once after a full dataset has been
ingested, so that label candidates are derived from the complete population of
each community rather than a partial view. Calling it mid-ingestion would
produce labels biased toward whichever samples arrived first.
*/
func (orchestrator *Orchestrator) Label() {
	orchestrator.unsupervised.Cycle(orchestrator.field)
}

/*
emitFromCommunity mints a new Value that carries the community's aggregate
affinity state. The emitted Value is published back to the queue so it
re-enters the compute pipeline — this is the mechanism through which
concentrated communities drive further computation.
*/
func (orchestrator *Orchestrator) emitFromCommunity(community *geometry.Field) *primitive.Value {
	if community == nil || len(community.Affinity) == 0 {
		return nil
	}

	var emitted primitive.Value

	cfg := core.Cfg.Value.Region.Affinity

	for wordIndex, word := range community.Affinity {
		if cfg.Start+wordIndex >= 128 {
			break
		}

		emitted[cfg.Start+wordIndex] = word
	}

	return &emitted
}

/*
route places each Value into a community field: the sparse children of the
root phase field whose aggregate affinity is nearby (Hamming) and whose Shannon
saturation stays below the configured limit after an XOR merge.
*/
func (orchestrator *Orchestrator) route(
	values ...*primitive.Value,
) {
	root := orchestrator.field
	if root == nil {
		return
	}

	shannon := core.Cfg.System.ShannonLimit

	for _, value := range values {
		if value == nil {
			continue
		}

		affinity := affinityWordsFromValue(value)

		if affinityIsEmpty(affinity) {
			orchestrator.materializeAffinity(value)

			affinity = affinityWordsFromValue(value)

			if affinityIsEmpty(affinity) {
				continue
			}
		}

		bestSlot := -1
		bestDistance := math.MaxInt

		for slotIndex, community := range root.Fields {
			if community == nil {
				continue
			}

			if community.AffinitySaturation() >= shannon {
				continue
			}

			distance := geometry.AffinityHammingDistance(community.Affinity, affinity)
			if distance >= affinityMatchBitBudget {
				continue
			}

			if community.PredictAffinitySaturationAfterMerge(affinity) >= shannon {
				continue
			}

			if distance < bestDistance {
				bestDistance = distance
				bestSlot = slotIndex
			}
		}

		if bestSlot >= 0 {
			community := root.Fields[bestSlot]
			community.Values = append(community.Values, value)
			community.MergeAffinity(affinity)

			if viz.DefaultBus.IsActive() {
				viz.DefaultBus.Publish(viz.ValueJoinedCommunityEvent(
					(*value)[core.Cfg.Value.Region.ID.Start],
					bestSlot,
					bestDistance,
				))

				sat := community.AffinitySaturation()
				if sat >= shannon {
					viz.DefaultBus.Publish(viz.CommunitySaturatedEvent(bestSlot, sat))
				}
			}

			continue
		}

		freeSlot := firstFreeCommunitySlot(root)
		if freeSlot < 0 {
			continue
		}

		community := geometry.NewCommunityField(root.Modulus())
		community.Values = append(community.Values, value)
		community.MergeAffinity(affinity)
		root.Fields[freeSlot] = community

		if viz.DefaultBus.IsActive() {
			viz.DefaultBus.Publish(viz.CommunityCreatedEvent(freeSlot, community.Affinity))
		}
	}
}

/*
materializeAffinity installs the affinity program on the Value and executes
it synchronously through the queue's backend. This guarantees the affinity
region (words 123-127) is populated before the orchestrator reads it for
routing, regardless of what program the caller originally installed.
*/
func (orchestrator *Orchestrator) materializeAffinity(value *primitive.Value) {
	installer := programmer.Installer{}

	if err := installer.InstallProgram(value, "affinity"); err != nil {
		return
	}

	orchestrator.queue.ExecuteInline(
		[]unsafe.Pointer{unsafe.Pointer(value)},
	)
}

/*
affinityIsEmpty returns true when every word in the affinity slice is zero,
meaning the compute backend has not yet run the affinity kernel on this Value.
A zero-affinity Value produces a trivially zero gap against a zero-aggregate
community and must not be counted as resolved.
*/
func affinityIsEmpty(words []uint64) bool {
	for _, word := range words {
		if word != 0 {
			return false
		}
	}

	return true
}

/*
affinityWordsFromValue copies the configured Affinity region words from the wire frame.
*/
func affinityWordsFromValue(value *primitive.Value) []uint64 {
	cfg := core.Cfg.Value.Region.Affinity
	wordCount := int(cfg.Bits+63) / 64

	if wordCount < 1 {
		wordCount = 1
	}

	out := make([]uint64, wordCount)
	start := cfg.Start

	for wordIndex := range out {
		out[wordIndex] = (*value)[start+wordIndex]
	}

	return out
}

/*
firstFreeCommunitySlot returns the lowest index in the root phase vector that
is still nil and can host a new community leaf.
*/
func firstFreeCommunitySlot(root *geometry.Field) int {
	if root == nil || len(root.Fields) == 0 {
		return -1
	}

	for slotIndex := range root.Fields {
		if root.Fields[slotIndex] == nil {
			return slotIndex
		}
	}

	return -1
}
