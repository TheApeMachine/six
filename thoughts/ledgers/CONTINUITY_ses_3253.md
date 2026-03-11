---
session: ses_3253
updated: 2026-03-11T03:54:53.088Z
---

# Session Summary

## Goal
Improve `experiment/task/codegen/languages.go` so the `Languages` experiment uses the intended fixed 50-byte suffix prompts and then continue diagnosing the remaining low completion quality until `TestPipeline/Languages` produces meaningfully better outputs without regressing the earlier timeout fix.

## Constraints & Preferences
- Every live-session assistant message had to start with `DIGGING IN...`
- User prefers exhaustive search/research mode: parallel `explore`/`librarian` agents plus direct repo searching
- Preserve exact file paths and function names
- Prefer minimal, safe integration changes over broad refactors
- Do not revert unrelated user changes
- `lsp_diagnostics` is not reliable here; both Go files reported `warning[go list] ... No active builds contain ...`
- Important existing runtime constraint from earlier work: keep prompt-time `logic.Circuits` gated in `experiment/task/pipeline.go` to avoid the `Languages` timeout path through `vm/composer_circuit.go`

## Progress
### Done
- [x] Reconfirmed the earlier timeout fix is still in place in `experiment/task/pipeline.go`: `solvePromptReadout(...)` only forwards `logic.Circuits` when `shouldUsePromptLogicCircuits(...)` allows it, and focused prompt tests plus `TestPipeline/Languages` complete without hanging.
- [x] Traced the `Languages` quality path through `experiment/task/codegen/languages.go`, `experiment/task/pipeline.go`, and `tokenizer/prompt.go`.
- [x] Found a concrete benchmark-definition mismatch: `LanguagesExperiment.Prompts()` used `process.PromptWithHoldout(experiment.Holdout())`, and `Holdout()` returns `(50, tokenizer.RIGHT)`, but in `tokenizer/prompt.go` that `50` is interpreted as 50 percent, not 50 bytes.
- [x] Verified in code that `process.PromptWithSamples(...)` is the repo’s established way to represent exact prompt splits, using `experiment/task/imagegen/reconstruction.go` and `experiment/task/logic/babi_benchmark.go` as patterns.
- [x] Patched `experiment/task/codegen/languages.go` so `LanguagesExperiment.Prompts()` now uses `process.PromptWithSamples(experiment.mds.PromptSamples(holdoutBytes))` with `holdoutBytes = 50` instead of percentage holdout.
- [x] Added `func (m *multiDataset) PromptSamples(holdoutBytes int) []process.PromptSample` in `experiment/task/codegen/languages.go` to rebuild exact per-sample byte buffers from `m.Generate()` and split them into `Visible`, `HeldOut`, and `Full`.
- [x] Added regression coverage in `experiment/task/codegen/languages_test.go` to prove `LanguagesExperiment.Prompts()` now holds out exactly 50 bytes on samples of different lengths and preserves the full sample text.
- [x] Added a benchmark in `experiment/task/codegen/languages_test.go` for `LanguagesExperiment.Prompts()`.
- [x] Formatted and ran:
  - `go test ./experiment/task/codegen -run 'TestLanguagesExperimentPrompts'`
  - `go test -v ./experiment/task -run 'TestPipeline/Languages'`
- [x] Verified the new `codegen` regression passes.
- [x] Collected two `explore` agent reports:
  - `bg_384dcff9` traced the `Languages` runtime/quality path and highlighted prompt-time logic gating plus fallback retrieval as likely quality bottlenecks.
  - `bg_794f3308` compared `Languages` to other prompt experiments and noted patterns like explicit prompt samples, cross-domain data, and other holdout schemes.

### In Progress
- [ ] Diagnosing the second quality bottleneck after the fixed-50-byte patch, because `go test -v ./experiment/task -run 'TestPipeline/Languages'` still reports `60 failure(s)` and many prompts collapse toward repeated wrong completions.
- [ ] Investigating whether prompt outputs are being selected from stale/global graph state or over-dominant corpus retrieval rather than the current prompt, especially since the failure log repeatedly returns the same unrelated completion such as the Python `has_close_elements` suffix.
- [ ] Checking whether the single `Booter`/`Graph` instance reused across the whole pipeline run is still causing prompt contamination despite `cortex.Graph.ResetPromptCycle()` existing in `vm/cortex/graph.go`.

### Blocked
- (none)

## Key Decisions
- **Switch `LanguagesExperiment.Prompts()` to explicit samples**: `process.PromptWithHoldout(50, tokenizer.RIGHT)` was provably using 50 percent, not 50 bytes, so the experiment was not matching its own comment/prose or intended benchmark shape.
- **Implement exact splitting in `multiDataset.PromptSamples(...)`**: This reused existing repo patterns from `experiment/task/imagegen/reconstruction.go` and `experiment/task/logic/babi_benchmark.go` rather than inventing a new prompt API.
- **Keep the earlier prompt-circuit timeout guard unchanged while investigating quality**: The `Languages` hang was already fixed safely in `experiment/task/pipeline.go`, and the current problem is completion quality, not performance.
- **Do not trust editor diagnostics for validation**: `lsp_diagnostics` returned `warning[go list] at 1:8: No active builds contain ...`, so validation continued through `gofmt` and `go test`.
- **Treat the new 50-byte fix as necessary but not sufficient**: After the patch, `TestPipeline/Languages` still completed with `60 failure(s)`, so another issue remains beyond holdout construction.

## Next Steps
1. Read the remaining relevant runtime path around prompt handling in `vm/cortex/graph.go`, `vm/cortex/ticker.go`, and any `results` emission/selection logic to determine whether prompt outputs can be stale across prompts.
2. Inspect how `collectPromptOutputs(...)` in `experiment/task/pipeline.go` chooses `results` and whether it may accept a leftover `results` payload unrelated to the just-sent `"prompt"` event.
3. Compare the repeated failure outputs in `/Users/theapemachine/.local/share/opencode/tool-output/tool_cdb0607110013HssUQwuW9ovPd` to see whether the same suffix is reused due to graph-state bleed or retrieval dominance.
4. If prompt-state bleed is confirmed, add a minimal fix and regression test around per-prompt result isolation/reset behavior.
5. Re-run:
   - `go test ./experiment/task/codegen -run 'TestLanguagesExperimentPrompts'`
   - `go test -v ./experiment/task -run 'TestPipeline/Languages'`
6. After the next fix is identified, consult Oracle for a final sanity review before broader package verification.

## Critical Context
- `experiment/task/codegen/languages.go` originally claimed: “Each sample ingests a function prompt + canonical solution; the right-50-byte holdout is used as the expected completion.”
- In reality, before the patch, `LanguagesExperiment.Prompts()` called `process.PromptWithHoldout(experiment.Holdout())`, and `tokenizer/prompt.go` computes holdout width as `int(float64(n)*float64(prompt.percentage)/100.0)`, so `50` meant 50 percent.
- Current patch in `experiment/task/codegen/languages.go`:
  - `LanguagesExperiment.Prompts()` now uses `process.PromptWithSamples(experiment.mds.PromptSamples(holdoutBytes))`
  - `holdoutBytes` is `50`
  - `multiDataset.PromptSamples(...)` reconstructs each sample from `m.Generate()` and splits at `max(len(current)-holdoutBytes, 1)`
- Added test file `experiment/task/codegen/languages_test.go` verifies:
  - sample of 80 bytes -> `HeldOut` length 50, visible length 30
  - sample of 120 bytes -> `HeldOut` length 50, visible length 70
  - `prompt.Full(...)` remains unchanged
- Validation result after this patch:
  - `go test ./experiment/task/codegen -run 'TestLanguagesExperimentPrompts'` passed
  - `go test -v ./experiment/task -run 'TestPipeline/Languages'` still completed but now showed `60 failure(s)` and a long run of `❌`
- The failure output strongly suggests repeated wrong completions, often returning the same unrelated code body such as Python `has_close_elements(...)`, even for prompts from other tasks/languages.
- That repeated-output pattern is the main new clue: it points toward either prompt-state contamination or stale/global `results` selection rather than just an incorrectly sized holdout.
- Important runtime findings from code:
  - `experiment/task/pipeline.go:263` `collectPromptOutputs(...)` subscribes, sends `"prompt"`, then returns the first non-empty `"results"` it sees
  - `vm/cortex/graph.go:113` `Tick(...)` does call `graph.ResetPromptCycle()` when it receives `pv.Key == "prompt"`
  - `vm/booter.go` runs a single `Graph` instance for the whole pipeline run
- Agent findings:
  - `bg_384dcff9` ranked likely bottlenecks as prompt-time logic gating for large code spans, composer fallback retrieval quality, and synthetic boundary behavior
  - `bg_794f3308` found explicit prompt samples and other prompt constructions in successful experiments, which directly motivated the fixed-50-byte patch
- The latest live `Languages` output came from `/Users/theapemachine/.local/share/opencode/tool-output/tool_cdb0607110013HssUQwuW9ovPd`
- `lsp_diagnostics` warnings encountered:
  - `warning[go list] at 1:8: No active builds contain /Users/theapemachine/go/src/github.com/theapemachine/six/experiment/task/codegen/languages.go: consider opening a new workspace folder containing it`
  - `warning[go list] at 1:8: No active builds contain /Users/theapemachine/go/src/github.com/theapemachine/six/experiment/task/codegen/languages_test.go: consider opening a new workspace folder containing it`

## File Operations
### Read
- `/Users/theapemachine/.local/share/opencode/tool-output/tool_cdb0607110013HssUQwuW9ovPd`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/experiment/interface.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/experiment/task/codegen/languages.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/experiment/task/imagegen/reconstruction.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/experiment/task/logic/babi_benchmark.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/experiment/task/pipeline.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/experiment/task/pipeline_prompt_test.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/experiment/task/pipeline_test.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/provider/dataset.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/provider/local/dataset.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/tokenizer/prompt.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/booter.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/composer.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/composer_circuit.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/composer_field.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/composer_test.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/circuit_compiler.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/graph.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/logic_snapshot.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/ticker.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/tooling.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six_updates/six_repo 7/vm/composer_field.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six_updates/six_repo 7/vm/cortex/tooling.go`

### Modified
- `/Users/theapemachine/go/src/github.com/theapemachine/six/experiment/task/codegen/languages.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/experiment/task/codegen/languages_test.go`
