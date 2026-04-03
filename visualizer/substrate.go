package visualizer

import (
	"github.com/theapemachine/six/pkg/core"
)

/*
SubstrateRuntime is JSON for the browser HUD: how compute.Batch actually runs
UniversalBitwise and post-batch program evolution. Keeps viz aligned with
pkg/compute/backend.go and VM ingress (prompt settle vs batch coalescing).
*/
type SubstrateRuntime struct {
	ProgramEvolution         bool   `json:"programEvolution"`
	BatchSize                int    `json:"batchSize"`
	BatchWindow              string `json:"batchWindow"`
	EvolutionBatchWindow     string `json:"evolutionBatchWindow"`
	StepwiseUniversalBitwise bool   `json:"stepwiseUniversalBitwise"`
	ExecSummary              string `json:"execSummary"`
	IngressSummary           string `json:"ingressSummary"`
	ProgramModel             string `json:"programModel"`
	UbTelemetryNote          string `json:"ubTelemetryNote"`
}

/*
BuildSubstrateRuntime snapshots live system.* knobs for /api/layout and tools.
*/
func BuildSubstrateRuntime() SubstrateRuntime {
	sys := core.Cfg.System
	evWindow := sys.EvolutionBatchWindow

	exec := "gatherBatch → executeBatch: groupFramesByProgram · UniversalBitwise (all LGP slots) · evolveProgramsInGroup (HomologousCrossover when ≥2 same-firmware frames) · handleFollowUp"

	ingress := "Machine.Read queues each frame on Backend.Queue; gatherBatch coalesces for up to max(batchWindow, evolutionBatchWindow) when programEvolution is on so mates can arrive after prompt settle (~50ms)."

	prog := "32-bit LGP slots packed 2 per uint64 program word: bits [31:18] dst word, [17:4] src word, [3:0] truth-table op — see pkg/compute/kernel/cpu.executeScalarInstruction"

	ubNote := "UniversalBitwise visits every slot 0..totalSlots-1 (skips instr==0). With telemetry.universal_bitwise_slots, the backend prefers the CPU substrate first so per-slot UDP hooks run; GPU paths are still used if CPU errors. experiment/task tests call telemetry.WireUniversalBitwiseSlotHook() after NewConfig (same as cmd/root)."

	return SubstrateRuntime{
		ProgramEvolution:         sys.ProgramEvolution,
		BatchSize:                sys.BatchSize,
		BatchWindow:              sys.BatchWindow.String(),
		EvolutionBatchWindow:     evWindow.String(),
		StepwiseUniversalBitwise: sys.StepwiseUniversalBitwise,
		ExecSummary:              exec,
		IngressSummary:           ingress,
		ProgramModel:             prog,
		UbTelemetryNote:          ubNote,
	}
}
