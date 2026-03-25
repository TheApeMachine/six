# In-Band Programmable Substrate Design

**Date:** 2026-03-25  
**Status:** Approved by project author  
**Goal:** Eliminate the von Neumann `residents` controller and move all routing, merging, shattering, and orchestration logic into the `Value` itself using unused regions as Affinity Mask and executable Program.

## Current Problems (Amateur Hour Layer)

- `pkg/compute/kernel/cpu/backend.go:54` — `residents []primitive.Value`
- `trie.go` — `longestCancellationSpan`, `buildEmittedValues`, `strongestCancellation`
- Go-level `if isInstruction` branching and ID management
- Manual token-span comparison instead of bitwise physics
- `Value` is treated as dumb data instead of self-executing substrate

This violates the core thesis that **the Value is the substrate**.

## Approved Memory Layout

The 128-word (8192-bit) `Value` is partitioned as follows:

### Region 0 — Data Field (unchanged)

- Words 0–59: 57 tokens + ValueID, PrevValueID, NextValueID
- `primitive.Region0TokenCount = 57`
- `DataWords = 60`, `DataBits = 3840`

### Region 1 — Instruction Register (kept for compatibility)

- Current 4-bit instruction (single-tick fallback)
- Location: `primitive.InstrStart`

### Region 2 — Affinity Mask (256 bits)

- Used for fast topological clustering via bitwise `AND` + popcount
- When two Values have sufficient overlap in this mask, they are considered "topologically close" and pulled into the ALU together.
- Replaces Go-level search over `residents`.

### Region 3 — Program Register (256 bits)

- **64 × 4-bit instructions** (`256 / 4 = 64`)
- Each 4-bit opcode drives the existing `UniversalBitwise` truth table.
- `0b0000` = NOP/HALT (early exit)
- This turns every `Value` into a self-executing 64-step microcode machine.

### Region 4+ — Temporary Links / Grouping + Future Use

- Remaining ~7400 bits.
- Can be used for explicit temporary linking/grouping of Values that should participate together in a structuring program (optional enhancement).
- Not required for initial implementation.

## New Execution Model

```text
Incoming Value(s)
      ↓
Compute Affinity Mask overlap (Region 2)
      ↓
If match: pair controlling Value's Program (Region 3) with target data
      ↓
UniversalBitwise runs full 64-tick program (or single op for compat)
      ↓
Result emitted downstream
      ↓
No residents list, no Go orchestration, no string/token comparison
```

The backend becomes a **dumb physics engine**:
- Stream processor over ringbuffer
- Affinity-based collision detection (bitwise, fast)
- ALU executes whatever program the controlling Value carries
- Graph events still emitted for visualization

## UniversalBitwise Changes

- Add support for reading 64 sequential 4-bit ops from Region 3
- Loop over program counter (pc = 0..63)
- Early exit on 0 opcode
- Each tick applies the existing truth table (`m0..m3`, `k1..k3`) to Region 0 data
- Output of tick *n* becomes input to tick *n+1* (in-place evolution)
- Maintain single-tick mode for backward compatibility

## Hardcoded Structuring Programs (Phase 1)

We will provide helper functions to install common programs:

- **Merge / Accumulate** (`OR` semantics)
- **Shatter** (`AND`, `A &^ B`, `B &^ A` sequence — implements the Roy/Harold kitchen example)
- **Prompt / XOR** (annihilation for question answering)
- **Identity / Passthrough**

These can be set via new methods like:
- `v.SetProgramOp(pc int, op uint8)`
- `v.InstallStructuringProgram(name string)`

Later phases can add self-modifying code or a simple assembler.

## Files to Change

**Core:**
- `pkg/primitive/value.go` — add region constants, program helpers, affinity helpers
- `pkg/compute/kernel/cpu/backend.go` — remove `residents`, update `UniversalBitwise`, simplify batch processing, replace cancellation logic
- `pkg/compute/kernel/cpu/trie.go` — delete `longestCancellationSpan`, `buildEmittedValues`, etc. (or deprecate)

**Supporting:**
- `experiment/pipeline_test.go`
- Visualizer/telemetry files (`visualizer/substrate.go`, `pkg/telemetry/*`)
- Any tests using the old cancellation path

## Success Criteria

1. `residents` slice and all Go-level search/build logic removed from backend
2. `UniversalBitwise` executes multi-tick programs from Region 3
3. Roy/Harold-style shattering works purely via ALU passes (no `buildEmittedValues`)
4. Existing pipeline test still passes (with updated instruction setup)
5. Visualizer still receives meaningful graph events
6. No regression in single-tick instruction mode

## Non-Goals (YAGNI)

- Full self-programming / program synthesis in phase 1
- Complex temporary linking logic (Region 4) — can be added later if grouping proves necessary
- Changing Region 0 size or token count
- GPU kernel changes yet (CPU backend first)

## Next Steps After Spec Approval

1. Create detailed implementation plan using writing-plans skill
2. Implement in isolated steps (regions → ALU → backend refactor → tests)
3. Verify with existing experiments

---

**Approval Status:** ✅ Approved by author  
**Implementation Ready:** Awaiting implementation plan creation.

This design fully realizes the "Value is the substrate" and "do the entire thing in-band" vision while remaining grounded in the existing 8192-bit frame and current ALU implementation.
