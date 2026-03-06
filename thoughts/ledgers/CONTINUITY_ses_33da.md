---
session: ses_33da
updated: 2026-03-06T10:03:21.046Z
---

DIGGING IN...
# Session Summary

## Goal
Get the SIX geometry-reasoning architecture working end-to-end as a real memory retrieval/inference system (typed relational evidence, iterative hole-filling, cleanup, negative evidence), and make `experiment/task/pipeline_test.go` reliably pass with genuine retrieval quality (not relaxed thresholds).

## Constraints & Preferences
User requires exhaustive search/research mode (parallel explore/librarian + grep/ast/rg), and every assistant message must start with `DIGGING IN...`; avoid optional/flagged core features for useful system behavior; preserve existing repo conventions; avoid destructive git operations; verify with real tests/builds.

## Progress
### Done
- [x] Performed exhaustive architecture audit with parallel agents/tools across `store`, `vm`, `kernel`, `geometry`, `tokenizer`, `experiment` and external references.
- [x] Updated architecture docs to align conceptual vs runtime reality in `/Users/theapemachine/go/src/github.com/theapemachine/six/README.md`.
- [x] Grounded terminology in code comments (`Wormhole`, `Virtual mitosis`) in:
  - `/Users/theapemachine/go/src/github.com/theapemachine/six/resonance/logic.go` (`TransitiveResonance`)
  - `/Users/theapemachine/go/src/github.com/theapemachine/six/geometry/permutation.go` (`ConditionMitosis`, `Mitosis`, `ApplyPermutation`)
- [x] Added decoupling seam in `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/machine.go`:
  - `bestFillFn`, `Machine.bestFill`, `MachineWithBestFill`, default binding to `kernel.BestFill`.
- [x] Implemented typed/event-driven routing + episodic freezing in `/Users/theapemachine/go/src/github.com/theapemachine/six/store/prime_field.go`:
  - `cubeFromEvents`, `blockFromChordDynamics`, `freezeActiveIfBoundary`, `SearchSnapshot`, support/veto channel handling.
- [x] Added cleanup memory/prototype snapping in `/Users/theapemachine/go/src/github.com/theapemachine/six/store/prime_field.go`:
  - `rememberPrototype`, `snapPrototype`, `CleanupSnap`.
- [x] Implemented iterative reasoning loop (`fill -> rotate -> fill`) in `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/machine.go`:
  - `slotMask`, hole derivation, `integrateFill`, bounded hops, cycle guard, score-improvement stop.
- [x] Replaced `ChordBin` hashing with deterministic SimHash-style projection in `/Users/theapemachine/go/src/github.com/theapemachine/six/data/chord.go` (`ChordBin`).
- [x] Aligned scoring semantics with contradiction/veto-aware terms:
  - `/Users/theapemachine/go/src/github.com/theapemachine/six/kernel/cpu/cpu_backend.go`
  - `/Users/theapemachine/go/src/github.com/theapemachine/six/kernel/cuda/bitwise_cuda.cu`
  - `/Users/theapemachine/go/src/github.com/theapemachine/six/kernel/metal/bitwise.metal`
- [x] Updated tests for new `PrimeField` behavior in `/Users/theapemachine/go/src/github.com/theapemachine/six/store/prime_field_test.go`.
- [x] Diagnosed and fixed `pipeline` experiment failure without lowering threshold:
  - Root cause in `/Users/theapemachine/go/src/github.com/theapemachine/six/tokenizer/prompt.go` (`Prompt.Next`, `Prompt.Value`) token/value desynchronization.
  - Added `outputs` history in `Prompt`.
  - Added regression test `/Users/theapemachine/go/src/github.com/theapemachine/six/tokenizer/prompt_test.go` (`TestPromptValueStaysAlignedWithNext`).
- [x] Verification succeeded after fix:
  - `go test ./experiment/task -run TestPipeline -v` passed.
  - `go test ./...` passed.
  - `go build ./...` passed.

### In Progress
- [ ] User requested terminal display of prompt + actual generated code result; current `TestPipeline` verbose output does not print those values directly, so extraction/printing command is being prepared.

### Blocked
- (none)

## Key Decisions
- **Do not relax experiment thresholds**: kept `LanguagesExperiment` outcome criterion (`> 0.5`) intact; fixed upstream retrieval/target alignment instead.
- **Implement non-optional reasoning features in core path**: cleanup memory, negative evidence, hole-fill loops are integrated directly (no feature flags), per user instruction.
- **Fix `Prompt` alignment before deeper manifold reorder changes for pipeline**: `Prompt.Next`/`Prompt.Value` desync was a direct high-confidence correctness bug affecting pipeline scoring.
- **Keep backend parity**: aligned CPU/CUDA/Metal scoring formulas to avoid divergent behavior across execution backends.

## Next Steps
1. Print prompt/result pairs on terminal by running pipeline and dumping `LanguagesExperiment.TableData()` entries (`prompt` and `result`) after `pipeline.Run()`.
2. Share those concrete prompt/result lines back to user.
3. If any pair looks off, trace from `Pipeline.Prompt` (`bPrompt`, `bRes`) through `LanguagesExperiment.AddResult` state merge.

## Critical Context
- Initial pipeline failure was deterministic: `Expected '0.2429729353706171' to be greater than '0.5'`.
- That failure is now resolved after `Prompt` alignment fix; full test suite currently passes.
- `Pipeline.Prompt` currently logs only `pipeline prompt response` via `console.Trace`, which may not appear in test stdout depending on logger sink.
- `Prompt.Value(idx)` is used in `LanguagesExperiment.AddResult` to build `target`; misalignment there directly corrupts experiment scoring even if retrieval is good.
- LSP diagnostics were unavailable in this workspace (`No active builds contain ...`), so source-of-truth validation used `go test`/`go build`.

## File Operations
### Read
- `/Users/theapemachine/go/src/github.com/theapemachine/six`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/BITWISE.md`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/README.md`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/cmd`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/console/logger.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/core`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/data`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/data/chord.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/experiment/interface.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/experiment/task/codegen/languages.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/experiment/task/pipeline.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/experiment/task/textgen/compositional_test.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/geometry`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/geometry/manifold.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/geometry/permutation.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/geometry/phase.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/geometry/substrate.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/go.mod`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/kernel`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/kernel/dispatch.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/kernel/metal/bitwise.m`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/numeric/core.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/numeric/prime.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/provider/dataset.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/resonance`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/resonance/logic.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/store/prime_field.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/store/prime_field_test.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/tokenizer`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/tokenizer/prompt.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/loader.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/machine.go`

### Modified
- `/Users/theapemachine/go/src/github.com/theapemachine/six/README.md`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/data/chord.go` (`ChordBin`)
- `/Users/theapemachine/go/src/github.com/theapemachine/six/geometry/permutation.go` (`ConditionMitosis`, `Mitosis`, `ApplyPermutation` comments)
- `/Users/theapemachine/go/src/github.com/theapemachine/six/kernel/cpu/cpu_backend.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/kernel/cuda/bitwise_cuda.cu`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/kernel/metal/bitwise.metal`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/resonance/logic.go` (`TransitiveResonance` comments)
- `/Users/theapemachine/go/src/github.com/theapemachine/six/store/prime_field.go` (`Insert`, `SearchSnapshot`, cleanup/prototype methods, routing helpers)
- `/Users/theapemachine/go/src/github.com/theapemachine/six/store/prime_field_test.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/tokenizer/prompt.go` (`Prompt.Next`, `Prompt.Value`, `Prompt.outputs`)
- `/Users/theapemachine/go/src/github.com/theapemachine/six/tokenizer/prompt_test.go` (`TestPromptValueStaysAlignedWithNext`)
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/machine.go` (reasoning loop, masks, event application, retrieval integration, `MachineWithBestFill`)
