---
session: ses_3337
updated: 2026-03-08T09:04:53.466Z
---

# Session Summary

## Goal
Fix the memorization/retrieval failure behind `experiment/task/pipeline_test.go` by making cortex bedrock recall query and reinjection behavior align with how `PrimeField` stores memories, and verify the pipeline score rises above the failing threshold.

## Constraints & Preferences
Use local repo inspection/editing only; follow `AGENTS.md` conventions; use Go tests for validation; avoid reverting unrelated changes; preserve exact file paths and function names; keep working without permission once approved; no destructive git operations.

## Progress
### Done
- [x] Read `experiment/task/pipeline_test.go`, `experiment/task/pipeline.go`, `experiment/task/codegen/languages.go`, `vm/loader.go`, `store/prime_field.go`, `kernel/cpu/cpu_backend.go`, `vm/cortex/graph.go`, `vm/cortex/reactions.go`, `vm/cortex/node.go`, `vm/cortex/think.go`, and `vm/cortex/ticker.go` to trace ingestion -> storage -> retrieval -> generation.
- [x] Confirmed the failing path is the `Languages` pipeline experiment in `experiment/task/pipeline_test.go`, where success requires `experiment.Score() > 0.5`.
- [x] Confirmed `store/prime_field.go:PrimeField.Insert` stores memories as full `geometry.IcosahedralManifold` values using `supportCubeFromEvents`, `vetoCubeFromSupport`, `field.rot.Forward`, header updates, and boundary freezing via `freezeActiveIfBoundary`.
- [x] Confirmed `kernel/cpu/cpu_backend.go:BestFillCPUPackedBytes` scores support across cubes `0..3` and veto in cube `4`, so a recall query with only `Cubes[0]` populated is structurally disadvantaged.
- [x] Identified the original architectural mismatch: `vm/cortex/graph.go:recallQueryManifolds` flattened `node.Cube` into only `ctx.Cubes[0]` / `expected.Cubes[0]` and ignored `anchor`, `hole`, and `physicalFace`.
- [x] Confirmed sink extraction is secondary, not primary: `vm/cortex/graph.go:queryBedrock` injects recalled tokens back into the dreaming node, while `vm/cortex/think.go` emits from `g.sink.BestFace()`.
- [x] Read `AGENTS.md` and followed local rules; noted testing preference says Goconvey, but existing `vm/cortex/cortex_test.go` already uses standard `testing` and was extended in-place to match the current file.
- [x] Modified `vm/cortex/graph.go:recallQueryManifolds` to stop flattening the entire node into one cube and instead:
  - preserve `node.Header`
  - anchor the query on the dominant `physicalFace`
  - differentiate `ctx` vs `expected` using `anchor` and `hole`
  - project support signal into cubes `0..3`
  - spread strongest supporting faces across support cubes rather than leaving them empty
- [x] Added tests and a benchmark to `vm/cortex/cortex_test.go` covering `recallQueryManifolds`, including:
  - `TestRecallQueryManifolds_UsesAnchorHoleAndSupportCubes`
  - `TestRecallQueryManifolds_DialShiftMovesProbe`
  - `BenchmarkRecallQueryManifolds`
- [x] Fixed brittle test assumptions in `vm/cortex/cortex_test.go` by asserting against the returned `physicalFace`/shifted face and making the synthetic peak face unambiguous.
- [x] Ran `go test ./vm/cortex ./experiment/task` twice after the recall query changes:
  - `./vm/cortex` passes
  - `./experiment/task` still fails in `experiment/task/pipeline_test.go:63`
- [x] Identified a second likely retrieval mismatch and patched it in `vm/cortex/graph.go`:
  - extended `type recallCandidate struct` with `face int`
  - changed `collectRecallCandidates` to record the matched manifold `face`
  - changed `queryBedrock` reinjection from `LogicalFace: cand.chord.IntrinsicFace()` to `LogicalFace: cand.face`

### In Progress
- [ ] Re-run targeted tests after the latest `recallCandidate.face` / `LogicalFace: cand.face` reinjection fix in `vm/cortex/graph.go`.
- [ ] Determine whether pipeline failure is now dominated by remaining manifold-shape mismatch, candidate extraction policy in `collectRecallCandidates`, or downstream routing/sink behavior.
- [ ] Verify whether recalled face injection should use stored face directly, `node.Rot.Reverse(face)`, or another mapping tied to header/rotation state.

### Blocked
- (none)

## Key Decisions
- **Fix `recallQueryManifolds` first**: The strongest confirmed mismatch was between `PrimeField.Insert` storage layout and cortex query layout, so the smallest safe first fix was to make `vm/cortex/graph.go:recallQueryManifolds` structurally closer to storage rather than changing `PrimeField.Insert`.
- **Use `anchor`, `hole`, and `physicalFace` directly**: These values were already computed by `vm/cortex/reactions.go:Node.Hole()` but discarded, so the fix used them to create meaningful `ctx` vs `expected` manifolds.
- **Project support into cubes `0..3`**: `kernel/cpu/cpu_backend.go:BestFillCPUPackedBytes` explicitly scores across support cubes `0..3`, so leaving three support cubes empty was a structural scoring handicap.
- **Add focused cortex tests before iterating further**: `vm/cortex/cortex_test.go` was extended first so changes to `recallQueryManifolds` could be validated independently of the noisier end-to-end pipeline.
- **Preserve face identity during reinjection**: `LogicalFace: cand.chord.IntrinsicFace()` was judged likely wrong because bedrock retrieval is organized by manifold face position, not the chord’s intrinsic face, so reinjection was changed to use the matched `face`.

## Next Steps
1. Run `go test ./vm/cortex ./experiment/task` again after the latest `recallCandidate.face` / reinjection patch to see whether `experiment/task/pipeline_test.go` improves or still fails.
2. If `./experiment/task` still fails, inspect `vm/cortex/graph.go:collectRecallCandidates` next, especially:
   - scanning all faces from matched manifolds
   - novelty check `data.ChordHole(&faceChord, anchor)`
   - ignoring `hole` when scoring/extracting candidates
3. Validate whether reinjected `LogicalFace` should be the raw stored `face` or transformed through current node rotation/header semantics.
4. If recall is still weak, compare candidate extraction against `PrimeField.Insert` face placement more directly and consider using matched-face-targeted extraction instead of whole-manifold face sweeping.
5. Re-run the smallest failing pipeline cases from `experiment/task/pipeline_test.go` and compare `PROMPT` / `HOLDOUT` / `OBSERVED` logs to judge whether changes improve byte alignment.

## Critical Context
- The original leading diagnosis was that `store/prime_field.go:PrimeField.Insert` stores memories with event-driven cube routing and rotated face placement, while `vm/cortex/graph.go:recallQueryManifolds` was querying with a flattened single-cube copy.
- `kernel/cpu/cpu_backend.go:BestFillCPUPackedBytes` uses support cubes `0..3` and veto cube `4`; this made the original single-cube recall query especially weak.
- `vm/cortex/reactions.go:Node.Hole()` returns `(anchor, hole, physicalFace, shouldDream)`, and the fix now uses these values in `recallQueryManifolds`.
- `vm/cortex/graph.go:queryBedrock` previously reinjected recalled tokens with `LogicalFace: cand.chord.IntrinsicFace()`, which likely lost manifold face identity; this was changed to `LogicalFace: cand.face`.
- `go test ./vm/cortex ./experiment/task` after the first recall-query fixes produced:
  - `ok  	github.com/theapemachine/six/vm/cortex`
  - repeated failures in `experiment/task/pipeline_test.go:63`
- Example failing assertions/logs from `./experiment/task`:
  - `Expected '0.0049913941480206545' to be greater than '0.5' (but it wasn't)!`
  - `Expected '0' to be greater than '0.5' (but it wasn't)!`
  - `Expected '0' to be greater than '0' (but it wasn't)!`
- Example observed outputs remained low-quality/nonsensical even after the first fix:
  - For the `remove_Occ` prompt, `OBSERVED` was binary/noisy bytes like `"\x02t\x8a\xa5..."`
  - For `def sort_matrix(M):`, `OBSERVED` was malformed text like ` rsteo ... ((((((((((())))))))))))))`
  - For short prompts like `the quick re`, `OBSERVED` was repetitive garbage like `e        cccceeeee...hhhhhhh`
- Tooling issues encountered earlier:
  - `lsp_symbols` returned `Error: no views`
  - `Glob` failed with `Error: ENOENT: no such file or directory, posix_spawn '/opt/homebrew/bin/rg'`
  - `.opencode/context/openagents-repo/quick-start.md` was missing: `Error: File not found`
- Current modified files are not "(none)" anymore:
  - `vm/cortex/graph.go`
  - `vm/cortex/cortex_test.go`

## File Operations
### Read
- `/Users/theapemachine/go/src/github.com/theapemachine/six/AGENTS.md`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/experiment/interface.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/geometry/gf_rotation.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/kernel/cpu/cpu_backend.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/store/prime_field.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/store/prime_field_test.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/cortex_test.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/graph.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/node.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/reactions.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/think.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/ticker.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/loader.go`

### Modified
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/cortex_test.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/graph.go`
