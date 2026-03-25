# In-Band Programmable Substrate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate the `residents` von Neumann controller and move all merging, shattering, and orchestration logic in-band into the Value using Region 2 (Affinity) and Region 3 (64-op Program).

**Architecture:** 
- Extend primitive.Value with new region constants and program helpers.
- Make UniversalBitwise support full 64-tick program execution from Region 3.
- Replace Go-level cancellation logic (trie.go) with fast bitwise affinity matching.
- Simplify backend.go to a dumb stream processor (remove residents list entirely).
- Keep single-tick mode for compatibility. Use hardcoded structuring programs initially.
- Follow TDD, make frequent small commits, maintain visualizer/telemetry.

**Tech Stack:** Go, existing primitive.Value bit manipulation, ringbuffer pipeline, UniversalBitwise ALU.

**Spec Reference:** `docs/specs/2026-03-25-inband-programmable-substrate-design.md`

---

### Task 1: Define New Regions and Helpers in primitive.Value

**Files:**
- Modify: `pkg/primitive/value.go` (add constants + helpers)
- Test: `pkg/primitive/value_test.go`

- [ ] **Step 1:** Add new region constants after existing ones
```go
	RegionAffinityStart = 3856 // after current instr + padding
	RegionAffinityBits  = 256
	RegionProgramStart  = RegionAffinityStart + RegionAffinityBits
	RegionProgramBits   = 256
```
- [ ] **Step 2:** Add helper methods: `AffinityMask()`, `SetAffinityMask()`, `ProgramOp(pc int) uint8`, `SetProgramOp(pc int, op uint8)`, `InstallProgram(name string)`
- [ ] **Step 3:** Write failing test for new region helpers and program packing
- [ ] **Step 4:** Run test → verify it fails
- [ ] **Step 5:** Implement minimal helpers that pass test
- [ ] **Step 6:** Run test → verify it passes
- [ ] **Step 7:** Commit with message "feat(primitive): add Region 2 Affinity and Region 3 Program support"

### Task 2: Extend UniversalBitwise for Multi-Tick Programs

**Files:**
- Modify: `pkg/compute/kernel/cpu/backend.go` (UniversalBitwise + Read/WriteRegion)
- Test: update existing backend_test.go or add to pipeline_test.go

- [ ] **Step 1:** Add `RunProgram` mode flag and update function signature if needed
- [ ] **Step 2:** Implement 64-tick loop inside UniversalBitwise that reads from RegionProgramStart
- [ ] **Step 3:** Support early exit on 0 opcode; preserve single-tick backward compat
- [ ] **Step 4:** Write test that exercises a simple 3-op program (XOR then AND then HALT)
- [ ] **Step 5:** Run test → should fail
- [ ] **Step 6:** Fix implementation until test passes
- [ ] **Step 7:** Commit "feat(cpu): extend UniversalBitwise to execute 64-op programs from Region 3"

### Task 3: Add Hardcoded Structuring Programs

**Files:**
- Modify: `pkg/primitive/value.go` (add Install*Program helpers)
- Modify: `pkg/compute/kernel/cpu/backend.go` (expose common programs)

- [ ] **Step 1:** Add constants for common programs (Shatter, Merge, PromptXOR)
- [ ] **Step 2:** Implement `InstallShatterProgram(v *Value)`, `InstallMergeProgram(v *Value)`, etc.
- [ ] **Step 3:** Add test verifying a shatter program produces expected AND + remainders pattern
- [ ] **Step 4:** Run tests
- [ ] **Step 5:** Commit "feat(primitive): add hardcoded structuring programs for merge/shatter/xor"

### Task 4: Replace Cancellation Logic with Affinity Matching

**Files:**
- Modify: `pkg/compute/kernel/cpu/trie.go` (delete old functions, add AffinityMatch)
- Modify: `pkg/compute/kernel/cpu/backend.go` (update process*Batch methods)

- [ ] **Step 1:** Add `AffinityMatch(a, b *primitive.Value, threshold int) bool` using Popcount on Region 2
- [ ] **Step 2:** Remove `longestCancellationSpan`, `buildEmittedValues`, `strongestCancellation`, `Cancellation*` types
- [ ] **Step 3:** Update backend to use affinity-based pairing instead of residents list
- [ ] **Step 4:** Update pipeline_test.go to use new program-based approach
- [ ] **Step 5:** Run all tests, fix any breakage
- [ ] **Step 6:** Commit "refactor(cpu): replace residents + token cancellation with in-band affinity + programs"

### Task 5: Final Cleanup and Telemetry/Visualizer Updates

**Files:**
- Modify: `pkg/compute/kernel/cpu/backend.go` (remove residents field entirely)
- Modify: visualizer and telemetry files
- Update: experiment/pipeline_test.go, any remaining references

- [ ] **Step 1:** Remove `residents`, `nextID` management, `isInstruction` branch
- [ ] **Step 2:** Update graph events to reflect program execution
- [ ] **Step 3:** Update visualizer/substrate.go and telemetry to read new regions
- [ ] **Step 4:** Run full test suite + manual viz test
- [ ] **Step 5:** Final commit "chore: complete in-band programmable substrate refactor"

---

**Verification Checklist (before marking complete):**
- All tests pass (`go test ./pkg/compute/kernel/cpu... -count=1`)
- `experiment/pipeline_test.go` demonstrates shatter/merge via programs
- No `residents` slice remains in backend.go
- Visualizer still works
- Single-tick instructions still function for backward compat

**Plan complete.** 

Now that the implementation plan is written, we have two options:

**1. Subagent-Driven (recommended)** - I will dispatch fresh subagents for each task with review checkpoints between major steps. This gives clean isolation and parallel progress where possible.

**2. Inline Execution** - I execute the tasks directly in this session with checkpoints for your review.

Which approach would you prefer? (Or any changes to the plan first?)