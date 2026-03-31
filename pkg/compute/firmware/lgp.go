package firmware

import (
	"math/bits"
	"math/rand"

	"github.com/theapemachine/six/pkg/core"
)

// ---------------------------------------------------------------------------
// LGP — Linear Genetic Programming safeguards
// Implements IDEAS.md §3: Intron management and Homologous Crossover.
// ---------------------------------------------------------------------------

// InstructionSlot reads the 32-bit instruction at the given program slot.
func InstructionSlot(c *[128]uint64, slot int) uint32 {
	if c == nil || slot < 0 || slot >= core.Cfg.MaxPC {
		return 0
	}
	wordIdx := core.Cfg.ProgramIndex + slot/2
	if wordIdx < 0 || wordIdx >= len(c) {
		return 0
	}
	shift := uint((slot % 2) * 32)
	return uint32(c[wordIdx] >> shift)
}

// SetInstructionSlot writes a 32-bit instruction at the given program slot.
func SetInstructionSlot(c *[128]uint64, slot int, instr uint32) {
	if c == nil || slot < 0 || slot >= core.Cfg.MaxPC {
		return
	}
	wordIdx := core.Cfg.ProgramIndex + slot/2
	if wordIdx < 0 || wordIdx >= len(c) {
		return
	}
	shift := uint((slot % 2) * 32)
	mask := uint64(0xFFFFFFFF) << shift
	c[wordIdx] = (c[wordIdx] &^ mask) | (uint64(instr) << shift)
}

// IsIntron returns true if the instruction at the given slot is effectively
// a no-op (an "intron" in LGP terminology). Introns protect adjacent working
// logic from destructive crossover.
//
// An instruction is an intron if:
//   - It is zero (NOP / halt marker)
//   - It applies opcode 0011 (identity A) with src == dst (copies reg to itself)
//   - It applies opcode 0101 (identity B) with src == dst
func IsIntron(instr uint32) bool {
	if instr == 0 {
		return true
	}
	op := uint8(instr & 0xF)
	sc := uint16((instr >> 4) & 0x3FFF)
	dc := uint16((instr >> 18) & 0x3FFF)
	// Identity-A with same src/dst
	if op == 0x3 && sc == dc {
		return true
	}
	// Identity-B with same src/dst
	if op == 0x5 && sc == dc {
		return true
	}
	return false
}

// MakeIntron creates a no-op instruction that writes register idx to itself
// using opcode 0011 (identity A). This is a structural intron that protects
// adjacent instructions during crossover.
func MakeIntron(regIdx uint16) uint32 {
	// opcode=0011, src=regIdx, dst=regIdx
	return uint32(0x3) | (uint32(regIdx) << 4) | (uint32(regIdx) << 18)
}

// InsertIntrons peppers the program region of a frame with intron instructions
// at regular intervals. This shields working instruction blocks from being
// destroyed by blind crossover during the Build firmware phase.
// spacing controls how many real slots sit between each intron.
func InsertIntrons(c *[128]uint64, spacing int) {
	if c == nil || spacing <= 0 {
		return
	}
	start := int(PayloadProgramPCStart())
	// Use r0 for intron identity operations (word index of r0)
	intronInstr := MakeIntron(uint16(core.Cfg.R0))
	for slot := start; slot < core.Cfg.MaxPC; slot++ {
		if (slot-start)%(spacing+1) == spacing {
			SetInstructionSlot(c, slot, intronInstr)
		}
	}
}

// ---------------------------------------------------------------------------
// Execution Tracing — track which instructions influence the output register
// ---------------------------------------------------------------------------

// TraceEffective executes a simulated trace of a program and returns
// a bitmask where bit i is set if instruction i influenced register r6
// (the output/feature register). Only effective instructions should be
// propagated during crossover.
func TraceEffective(c *[128]uint64) uint64 {
	if c == nil {
		return 0
	}

	r6Idx := uint16(core.Cfg.R6 & 0x7F)

	// Dependency tracking: for each register, which instruction slots wrote to it.
	type regDep struct {
		slots uint64 // bitmask of instruction slots
	}
	deps := make(map[uint16]*regDep)
	ensureDep := func(r uint16) *regDep {
		if d, ok := deps[r]; ok {
			return d
		}
		d := &regDep{}
		deps[r] = d
		return d
	}

	start := int(PayloadProgramPCStart())
	for slot := start; slot < core.Cfg.MaxPC && slot-start < 64; slot++ {
		instr := InstructionSlot(c, slot)
		if instr == 0 {
			break
		}
		if IsIntron(instr) {
			continue
		}

		sc := uint16((instr >> 4) & 0x3FFF)
		dc := uint16((instr >> 18) & 0x3FFF)
		dstReg := dc & 0x7F

		// The instruction at this slot writes to dstReg.
		// It depends on src and dst register values.
		srcReg := sc & 0x7F
		bit := uint64(1) << (slot - start)

		d := ensureDep(dstReg)
		d.slots |= bit
		// Inherit dependencies from source
		if sd, ok := deps[srcReg]; ok {
			d.slots |= sd.slots
		}
	}

	if d, ok := deps[r6Idx]; ok {
		return d.slots
	}
	return 0
}

// ---------------------------------------------------------------------------
// Homologous Crossover
// ---------------------------------------------------------------------------

// HomologousCrossover copies only effective instructions from donor into
// recipient at matching slot positions. Non-effective (intron) slots in the
// donor are skipped. This prevents catastrophic forgetting of working logic.
func HomologousCrossover(recipient, donor *[128]uint64, rng *rand.Rand) {
	if recipient == nil || donor == nil {
		return
	}

	// Find which slots are effective in both programs.
	donorEffective := TraceEffective(donor)
	recipientEffective := TraceEffective(recipient)

	start := int(PayloadProgramPCStart())
	for slot := start; slot < core.Cfg.MaxPC && slot-start < 64; slot++ {
		bit := uint64(1) << (slot - start)

		donorInstr := InstructionSlot(donor, slot)
		if donorInstr == 0 {
			continue
		}

		// Only copy from donor if the slot is effective in the donor
		// AND not effective in the recipient (to avoid overwriting working code).
		donorIsEffective := donorEffective&bit != 0
		recipientIsEffective := recipientEffective&bit != 0

		if donorIsEffective && !recipientIsEffective {
			SetInstructionSlot(recipient, slot, donorInstr)
		} else if donorIsEffective && recipientIsEffective {
			// Both have effective code at this slot. Accept donor with
			// probability proportional to donor's instruction complexity.
			complexity := bits.OnesCount32(donorInstr)
			if rng.Intn(32) < complexity {
				SetInstructionSlot(recipient, slot, donorInstr)
			}
		}
	}
}
