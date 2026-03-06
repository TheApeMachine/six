# Expected Field Phase 2 Migration

## What Changed

- CPU scoring now accepts an optional expected-precision table (5 cubes x 27 blocks, `uint16` each).
- Dispatch and distributed execution paths now thread an optional precision pointer.
- CUDA and Metal interfaces now accept an optional precision pointer.
- `vm.Machine` now supports field-aware retrieval through `PromptWithExpectedField` without breaking `Prompt`.

## Default Behavior

- Existing behavior remains unchanged when precision is not provided.
- Precision defaults to unity (`1024` scale) in CPU/CUDA/Metal scoring paths.
- `Prompt(prompt, expectedReality)` remains unchanged and backward-compatible.

## New API Surface

- `kernel.BestSpanWithPrecision(...)`
- `kernel.BestFillWithPrecision(...)`
- `kernel.BestFillWithExpectedField(...)`
- `vm.Machine.PromptWithExpectedField(...)` now routes through field-aware best-fill resolution.
- `vm.MachineWithBranchPolicy(...)`
- `vm.Machine.Observability()`

## Scoring Notes

- Precision is applied per block.
- Support-related terms use support cube precision.
- Veto-related terms use veto cube precision (cube 4).
- Scaled terms are normalized by `/1024` before fixed-point weighting.

## Distributed Protocol

- Work request pointer layout now includes precision payload.
- Precision payload is optional.
- If omitted, workers run unity precision behavior.

## Verification Status

- `go test ./kernel/... ./vm ./geometry` passes.
- `go test ./experiment/task/scaling` passes after signature updates.
- Full suite still has an existing quality-threshold failure in `experiment/task/pipeline_test.go` (score assertion), unchanged in nature.
