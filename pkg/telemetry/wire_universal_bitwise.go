package telemetry

import (
	"fmt"

	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/core"
)

func universalBitwiseSlotMessage(info cpu.UniversalBitwiseSlotInfo) string {
	suffix := fmt.Sprintf(
		"instr=0x%08X · op=%s",
		info.Instr,
		TruthOpName(uint8(info.Instr)),
	)

	if info.Instr == 0 {
		suffix = "instr=0 (NOP · kernel skips this slot, not LSM I/O)"
	}

	return fmt.Sprintf(
		"UB slot %d/%d · batch_frames=%d · simd_tile=%v · %s",
		info.Slot+1,
		info.TotalSlots,
		info.FrameCount,
		info.Homogeneous,
		suffix,
	)
}

/*
WireUniversalBitwiseSlotHook attaches the per-LGP-slot UDP emitter to the CPU
UniversalBitwise path. Call after core.NewConfig() loads viper (same timing as
cmd init and experiment TestMain).

When telemetry.universal_bitwise_slots is false or telemetry.enabled is false,
the hook is cleared so hot paths stay untouched.
*/
func WireUniversalBitwiseSlotHook() {
	if !core.Cfg.TelemetryEnabled || !core.Cfg.TelemetryUniversalBitwiseSlots {
		cpu.UniversalBitwiseSlotHook = nil

		return
	}

	cpu.UniversalBitwiseSlotHook = func(info cpu.UniversalBitwiseSlotInfo) {
		Emit(Event{
			Component: "Backend",
			Action:    "UniversalBitwise",
			Data: EventData{
				Stage:         "slot",
				LgpSlot:       info.Slot,
				LgpSlotsTotal: info.TotalSlots,
				LgpInstr:      info.Instr,
				UbHomogeneous: info.Homogeneous,
				UbFrameCount:  info.FrameCount,
				Instruction:   TruthOpName(uint8(info.Instr)),
				Message:       universalBitwiseSlotMessage(info),
			},
		})
	}
}
