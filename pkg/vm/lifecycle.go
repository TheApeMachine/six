package vm

import (
	"math"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/mesh"
	"github.com/theapemachine/six/pkg/primitive"
)

type lifecycleDecision struct {
	Resolved bool
	Reinject bool
	Keep     bool
}

func (orchestrator *Orchestrator) evaluateResult(value *primitive.Value) lifecycleDecision {
	if value == nil {
		return lifecycleDecision{}
	}

	value.NormalizeAffinity()
	value.IncEpoch()
	decrementTTL(value)

	if isInBandReturn(value) {
		value.SetStatus(primitive.RESOLVED)
		value.RequestEmit()
		stampConfidence(value, 1)
		// A Readout/Association/explicit-emit Value is the substrate's
		// answer for this lifecycle. Clearing SchedulingNext is what
		// stops `next self` programs (vote_swarm in particular) from
		// re-arming the priority queue after the value has already been
		// surfaced. Without this clear, the Cycle loop never quiesces
		// because the queue keeps re-receiving the same resolved frame.
		value.SetSchedulingNext(0)
		return lifecycleDecision{Resolved: true, Reinject: true, Keep: true}
	}

	gap := orchestrator.beliefGap(value)
	stampConfidence(value, 1-gap)

	if gap <= core.Cfg.System.BeliefEpsilon {
		value.SetStatus(primitive.RESOLVED)
		value.RequestEmit()
		value.SetSchedulingNext(0)
		return lifecycleDecision{Resolved: true, Reinject: true, Keep: true}
	}

	if shouldContinue(value) {
		value.SetStatus(primitive.READY)
		return lifecycleDecision{Reinject: true}
	}

	// Non-continuing DONE Values still matter: their freshly computed affinity,
	// context, gradient, and properties must be folded back into the resident
	// field member. They are not returned to the caller unless they explicitly
	// emitted or targeted something. We also clear SchedulingNext so the value
	// stops re-arming itself; otherwise a `next self` program (vote_swarm,
	// learner loops, etc.) keeps the priority queue saturated even though the
	// epoch cap says we are done iterating.
	value.SetStatus(primitive.DONE)
	value.SetSchedulingNext(0)
	return lifecycleDecision{Reinject: true}
}

func isInBandReturn(value *primitive.Value) bool {
	if value == nil {
		return false
	}

	if value.EmitRequested() {
		return true
	}

	switch value.Role() {
	case primitive.ValueRoleReadout, primitive.ValueRoleAssociation:
		return true
	default:
		return false
	}
}

func shouldContinue(value *primitive.Value) bool {
	if value == nil || value.SchedulingNext() == 0 {
		return false
	}

	if value.TTL() == 0 && value.Role() == primitive.ValueRoleLearner {
		return false
	}

	maxPasses := uint64(core.DefaultTokenSettleMaxPasses)
	if maxPasses == 0 {
		maxPasses = 4
	}

	return value.Epoch() < maxPasses
}

func decrementTTL(value *primitive.Value) {
	if value == nil {
		return
	}

	if value.TTL() > 0 {
		value.DecTTL()
	}
}

func (orchestrator *Orchestrator) beliefGap(value *primitive.Value) float64 {
	if value == nil {
		return 1
	}

	// The ALU writes reduced prediction error into SURPRISAL. Fresh Values
	// start at 512 so they cannot accidentally resolve before one pass.
	surprisal := float64(value.Surprisal()) / 512.0
	if surprisal < 0 {
		surprisal = 0
	}
	if surprisal > 1 {
		surprisal = 1
	}

	pressure := 1.0
	if orchestrator != nil && orchestrator.field != nil {
		if metrics := orchestrator.fieldMetrics(); metrics != nil {
			pressure = 0.5 + metrics.PressureMult
		}
	}

	gap := surprisal * pressure
	if math.IsNaN(gap) || math.IsInf(gap, 0) {
		return 1
	}
	if gap < 0 {
		return 0
	}
	if gap > 1 {
		return 1
	}

	return gap
}

func (orchestrator *Orchestrator) fieldMetrics() *mesh.FieldMetrics {
	if orchestrator == nil || orchestrator.field == nil {
		return nil
	}

	return orchestrator.field.MetricsSnapshot()
}

func stampConfidence(value *primitive.Value, confidence float64) {
	if value == nil {
		return
	}

	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}

	value.SetProperty(primitive.CONFIDENCE, uint64(confidence*1_000_000))
}
