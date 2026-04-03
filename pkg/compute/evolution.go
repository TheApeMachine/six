package compute

import (
	"math/rand"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/firmware"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
)

/*
EvolutionStage is the interface for post-execution lifecycle processing.
It receives frames AFTER hardware dispatch (UniversalBitwise) and applies
evolutionary operators (crossover, signal emission) before returning them
for follow-up handling.

This separation keeps the Backend as a pure hardware load balancer:
it dispatches frames to CUDA/Metal/CPU substrates and nothing more.
Algorithmic behavior — what happens to frames after execution — is
the EvolutionStage's concern.
*/
type EvolutionStage interface {
	// ProcessBatch applies evolutionary operators to frames that have
	// already been executed by the hardware substrate. The groups
	// parameter carries the program-grouped frames (for same-program
	// crossover), and allFrames carries the full ungrouped batch (for
	// cross-group signal emission).
	ProcessBatch(groups [][]unsafe.Pointer, allFrames []unsafe.Pointer)
}

/*
EvolutionManager is the default EvolutionStage implementation. It performs:
  - Pairwise HolographicCrossover within program groups (same-program mates)
  - Signal emission across the full batch (cross-program, cross-group)

The Backend holds a reference to this via the EvolutionStage interface
and invokes it after hardware dispatch completes.
*/
type EvolutionManager struct {
	onEmit EmitCallback
	queues map[QueueType]chan unsafe.Pointer
}

// NewEvolutionManager creates an EvolutionManager wired to the backend's
// emit callback and queue system for child re-injection.
func NewEvolutionManager(onEmit EmitCallback, queues map[QueueType]chan unsafe.Pointer) *EvolutionManager {
	return &EvolutionManager{
		onEmit: onEmit,
		queues: queues,
	}
}

/*
ProcessBatch applies evolutionary operators to executed frames.
Phase 1: pairwise crossover within program groups.
Phase 2: signal emission across the full batch.
*/
func (em *EvolutionManager) ProcessBatch(groups [][]unsafe.Pointer, allFrames []unsafe.Pointer) {
	// Phase 1: Evolution within program groups (same-program crossover).
	for _, group := range groups {
		em.evolveProgramsInGroup(group)
	}

	// Phase 2: Signal emission across the FULL batch (cross-group).
	em.emitSignalsInBatch(allFrames)
}

/*
evolveProgramsInGroup performs pairwise HolographicCrossover on adjacent frames
within a program group when system.programEvolution is enabled.

HolographicCrossover blends two parents plus a structured third-parent noise
source via majority-rule in HIE (holographic instruction encoding) space. The
parentBias parameter steers between exploration (0 = pure affine noise orbit)
and exploitation (1 = collapse to donor). SubstrateExploitScore provides the
bias: high token-region structure similarity → exploit, low → explore.

The RNG is seeded from batch shape and frame IDs so runs are reproducible for
a given queued ordering without introducing yet another global entropy source.
*/
func (em *EvolutionManager) evolveProgramsInGroup(group []unsafe.Pointer) {
	if !core.Cfg.System.ProgramEvolution {
		return
	}

	if len(group) < 2 {
		return
	}

	seed := uint64(len(group)) * 0x9E3779B97F4A7C15
	idWord := core.Cfg.Value.Region.ID.Start

	for index, ptr := range group {
		frame := (*[128]uint64)(ptr)
		if idWord >= 0 && idWord < len(frame) {
			seed ^= frame[idWord]
		}

		seed ^= uint64(index+1) * 0x85EBCA6B
	}

	rng := rand.New(rand.NewSource(int64(seed ^ (seed >> 32))))

	for pairIdx := 0; pairIdx+1 < len(group); pairIdx += 2 {
		recipient := (*[128]uint64)(group[pairIdx])
		donor := (*[128]uint64)(group[pairIdx+1])

		recipientValue := primitive.Value(*recipient)
		donorValue := primitive.Value(*donor)
		parentBias := primitive.SubstrateExploitScore(&recipientValue, &donorValue)

		firmware.HolographicCrossover(recipient, recipient, donor, rng, parentBias)
	}
}

/*
emitSignalsInBatch scans signals between adjacent frame pairs across the
full batch after all UniversalBitwise groups have executed. Signals are
about token-region structure, not program similarity — a prompt Value and
a corpus Value with completely different programs can produce strong
signals. This is how prompts discover structure.

New children are inserted into the spatial index via onEmit and queued
for execution.
*/
func (em *EvolutionManager) emitSignalsInBatch(frames []unsafe.Pointer) {
	if len(frames) < 2 {
		return
	}

	seed := uint64(len(frames)) * 0x517CC1B727220A95
	idWord := core.Cfg.Value.Region.ID.Start

	for index, ptr := range frames {
		frame := (*[128]uint64)(ptr)
		if idWord >= 0 && idWord < len(frame) {
			seed ^= frame[idWord]
		}
		seed ^= uint64(index+1) * 0x9E3779B97F4A7C15
	}

	rng := rand.New(rand.NewSource(int64(seed ^ (seed >> 32))))

	nextWord := core.Cfg.Value.Region.Next.Start

	for pairIdx := 0; pairIdx+1 < len(frames); pairIdx += 2 {
		frameA := (*[128]uint64)(frames[pairIdx])
		frameB := (*[128]uint64)(frames[pairIdx+1])

		a := primitive.Value(*frameA)
		b := primitive.Value(*frameB)

		children := primitive.EmitFromSignals(&a, &b, rng)
		if len(children) == 0 {
			continue
		}

		// Link parent A → first child via NextID so the chain is walkable.
		firstChildID := children[0][idWord]
		if firstChildID != 0 && frameA[nextWord] == 0 {
			frameA[nextWord] = firstChildID

			if em.onEmit != nil {
				parentVal := primitive.Value(*frameA)
				em.onEmit(&parentVal)
			}
		}

		for _, child := range children {
			if em.onEmit != nil {
				em.onEmit(child)
			}

			ptr := unsafe.Pointer(child)
			select {
			case em.queues[NORMAL] <- ptr:
			default:
				errnie.Warn(
					"compute.evolution: dropped emitted child",
					"child_id", child[idWord],
				)
			}
		}

		telemetry.Emit(telemetry.Event{
			Component: "Substrate",
			Action:    "Emit",
			Data: telemetry.EventData{
				Stage:        "signal-emission",
				UbFrameCount: len(children),
				Message:      "emitted child Values from signal detection",
			},
		})
	}
}
