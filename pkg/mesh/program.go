package mesh

import (
	"math/rand"
	"time"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/numeric/learned"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Program encapsulates the probabilistic program selection and top-down
feedback routing for a Field. It maintains the policy weights that emerge
from the Field's metrics and assigns firmware to visiting Values.
*/
type Program struct {
	policy map[core.FirmwareType]*learned.Weight
	rng    *rand.Rand
}

/*
NewProgram creates a new Program selector for the given Field.
It initializes the policy weights for all available firmware types.
*/
func NewProgram() *Program {
	policy := make(map[core.FirmwareType]*learned.Weight, len(core.Cfg.Programs))

	for fw := range core.Cfg.Programs {
		policy[fw] = learned.NewWeight(0.35)
	}

	return &Program{
		policy: policy,
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

/*
Select probabilistically chooses a firmware program based on the current
policy weights, and emits a new Value (a probe/carrier) stamped with
that firmware. It targets the provided visitor ID so the new Value can
interact with it.
*/
func (program *Program) Select(visitorID uint64) *primitive.Value {
	if program == nil {
		return nil
	}

	var totalWeight float64
	for _, weight := range program.policy {
		// learned.Weight.Next(0) returns the current value without adjusting it
		val, _ := weight.Next(0)
		totalWeight += val
	}

	var selectedFw core.FirmwareType = "beam_swarm_step" // Fallback

	if totalWeight > 0 {
		r := program.rng.Float64() * totalWeight
		var cumulativeWeight float64

		for fw, weight := range program.policy {
			val, _ := weight.Next(0)
			cumulativeWeight += val
			if r <= cumulativeWeight {
				selectedFw = fw
				break
			}
		}
	}

	// Emit a new Value that runs the selected program, targeting the visitor
	carrier := primitive.Emit(
		primitive.WithFirmware(selectedFw),
		primitive.WithTarget(visitorID),
		primitive.WithStatus(uint64(primitive.READY)),
	)

	return carrier
}

/*
Update adjusts the policy weights based on the current FieldMetrics.
It feeds the metrics into the learned.Weight instances, allowing the
programs to emerge based on the community's structural state.
*/
func (program *Program) Update(metrics *FieldMetrics) {
	if program == nil || metrics == nil {
		return
	}

	// 1. beam_swarm_step (The Drive):
	// Thrives on the gap (Free Energy). High pressure means the community's belief
	// is fractured and uncertain. We increase exploration.
	// We predict 1.0; if PressureMult is high, error is low, weight increases.
	if w, ok := program.policy["beam_swarm_step"]; ok {
		w.Next(1.0, metrics.PressureMult)
	}

	// 2. hypothesis & falsification (The Test):
	// Thrives on convergence. When the eigenmode is strong (DominantRatio is high),
	// we don't need to explore blindly; we need to test the belief against predicted-absent patterns.
	// We predict 1.0; if DominantRatio is high, error is low, weight increases.
	if w, ok := program.policy["hypothesis"]; ok {
		w.Next(1.0, metrics.DominantRatio)
	}

	if w, ok := program.policy["falsification"]; ok {
		w.Next(1.0, metrics.DominantRatio)
	}

	// 3. temperature & intervene (The Rotation/Attention):
	// Thrives on stagnation. If the field is highly crystallized (Consensus is high)
	// but we might be in a local minimum, we inject physical noise into the Affinity vector.
	// We predict 1.0; if Consensus is high, error is low, weight increases.
	if w, ok := program.policy["temperature"]; ok {
		w.Next(1.0, metrics.Consensus)
	}

	if w, ok := program.policy["intervene"]; ok {
		w.Next(1.0, metrics.Consensus)
	}

	// 4. unsupervised_learn (The Discovery):
	// Thrives when Coverage is low. The field needs to discover new structure
	// because it lacks labels.
	// We predict 0.0; if Coverage is low, error is low, weight increases.
	if w, ok := program.policy["unsupervised_learn"]; ok {
		w.Next(0.0, metrics.Coverage)
	}

	// 5. classify_readout & link (The Exploit):
	// Thrives when the field is fully crystallized/saturated.
	// We predict 1.0; if Crystallization is high, error is low, weight increases.
	if w, ok := program.policy["classify_readout"]; ok {
		w.Next(1.0, metrics.Crystallization)
	}

	if w, ok := program.policy["link"]; ok {
		w.Next(1.0, metrics.Crystallization)
	}
}
