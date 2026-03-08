---
session: ses_3334
updated: 2026-03-08T09:18:29.811Z
---

# Session Summary

## Goal
Diagnose the most likely remaining root cause for `experiment/task/pipeline_test.go` still failing (with repeated `survivorsWritten=0`), and identify the smallest next corrective change to try.

## Constraints & Preferences
- Read-only repo inspection only: no edits, no git writes, no test execution.
- Must ground claims in exact code locations + concrete code behavior; no speculation without citations.
- Must account for verified fixes already made: `recallQueryManifolds` anchor/hole/physicalFace + support cubes 0..3, `collectRecallCandidates` hole-overlap filter, `queryBedrock` reinjection via `node.Rot.Reverse(cand.face)`.
- Must not propose changes already attempted; must not ignore `survivorsWritten=0`.

## Progress
### Done
- [x] Located the `survivorsWritten` log site and confirmed it comes from `g.WriteSurvivors(0.1)` in `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/think.go`.
- [x] Read survivor persistence path and thresholding:
  - `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/ticker.go`: `func (g *Graph) Survivors(threshold float64) []*Node` excludes `g.source` and `g.sink`, requires `node.Energy() >= threshold`.
  - `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/graph.go`: `func (g *Graph) WriteSurvivors(threshold float64) int` writes only those survivors’ non-empty faces into `PrimeField.Insert(...)`.
- [x] Audited the recall/dream loop implementation sites to connect to the “recall-focused fixes”:
  - `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/graph.go`: `recallQueryManifolds`, `collectRecallCandidates`, `queryBedrock`.
  - Verified reinjection uses `LogicalFace: node.Rot.Reverse(cand.face)` in `queryBedrock` and that `collectRecallCandidates` derives candidates from matched manifolds’ faces.
- [x] Traced how logical vs physical faces are handled in node accumulation and output extraction:
  - `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/reactions.go`: `Node.Arrive` routes by `routed := n.Rot.Forward(logicalFace)`; accumulation goes into `n.Cube[routed]`.
  - `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/node.go`: `Node.BestFace` scans physical faces then returns logical via `n.Rot.Reverse(bestFace)`.
- [x] Inspected the ingestion/storage path for manifolds and rotation/header semantics:
  - `/Users/theapemachine/go/src/github.com/theapemachine/six/store/prime_field.go`: `func (field *PrimeField) Insert(byteVal byte, pos uint32, chord data.Chord, events []int)` uses `field.rot.Forward(logicalFace)` to choose `blockIndex` and updates `field.manifolds[0].Header` via `applyEvent`.
  - `/Users/theapemachine/go/src/github.com/theapemachine/six/geometry/manifold.go`: `ManifoldHeader` bit packing and meaning of `State`, `RotState`, `Winding`.
- [x] Inspected the BestFill boundary/header filter behavior on CPU backend:
  - `/Users/theapemachine/go/src/github.com/theapemachine/six/kernel/cpu/cpu_backend.go`: `ctxHasBoundarySignal := ctxState != 0 || ctxWinding != 0`; if true, candidate headers must match `Winding` and `State` exactly (but not necessarily `RotState`).
- [x] Verified pipeline uses `vm.Machine` cortex-based prompting (not the older direct BestFill path):
  - `/Users/theapemachine/go/src/github.com/theapemachine/six/experiment/task/pipeline.go`: `pipeline.machine.Prompt(promptChords, nil)`
  - `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/machine.go`: `Prompt` constructs a `cortex.Graph` and calls `graph.Think(...)`.

### In Progress
- [ ] Concluding which concrete mismatch most directly explains `survivorsWritten=0` across pipeline runs: whether node energy never crosses threshold, bedrock recall never injects useful energy due to header/state/winding gating, or output/extraction stalls leaving no dense survivors.

### Blocked
- (none)

## Key Decisions
- **Follow the `survivorsWritten=0` signal upstream through `WriteSurvivors`/`Survivors`/`Node.Energy`**: this directly targets the verified symptom instead of re-litigating the already-fixed recall mechanics.
- **Inspect BestFill’s header gating (State/Winding) in the CPU backend**: because recall now depends on BestFill matches, and a header mismatch can yield “no matches,” which in turn can prevent node densification and thus produce `survivorsWritten=0`.

## Next Steps
1. Read `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/graph.go` earlier sections around graph initialization + any sink/source seeding to see if headers (`node.Header`) ever get set in a way that causes `ctxState/ctxWinding` to mismatch the stored `PrimeField` manifolds.
2. Check how `node.Header` is expected to track `State/Winding/RotState` during thought (it is updated in `Node.Arrive` for rotation tokens) versus how `PrimeField.Insert` updates stored headers (`applyEvent`); look for a divergence that would make `ctxHasBoundarySignal` true and then exclude most candidates in `BestFillCPUPackedBytes`.
3. Inspect whether `Graph.queryBedrock` passes a `ctx.Header`/`expected.Header` that should be zeroed (or aligned) for recall, given `BestFillCPUPackedBytes`’s strict `State/Winding` equality check when non-zero.
4. If a mismatch is confirmed, the smallest next change to try is to ensure recall queries use a header consistent with stored memories (likely forcing `ctx.Header`/`expected.Header` to a neutral header for recall, or synchronizing `node.Header` with `node.Rot`/sequencer events before calling `recallQueryManifolds`).

## Critical Context
- The `survivorsWritten=0` log is emitted after thought completes in `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/think.go` via `written := g.WriteSurvivors(0.1)`.
- `WriteSurvivors` only writes nodes returned by `g.Survivors(0.1)` and `Survivors` excludes `g.source` and `g.sink`; so `survivorsWritten=0` implies either:
  - no non-source/sink node has `Node.Energy() >= 0.1`, or
  - there are survivors but every face is empty (unlikely given how `Energy()` is computed from face popcounts).
- BestFill can silently return no match due to header gating:
  - `/Users/theapemachine/go/src/github.com/theapemachine/six/kernel/cpu/cpu_backend.go`: if `ctxState != 0 || ctxWinding != 0`, candidates must have exactly matching `State` and `Winding` or they’re skipped before scoring.
- Stored `PrimeField` headers are advanced by ingest events:
  - `/Users/theapemachine/go/src/github.com/theapemachine/six/store/prime_field.go`: `applyEvent` updates `field.manifolds[0].Header` and increments winding when state==1; therefore ingested manifolds can carry non-zero header signals.
- Recall query headers are taken from the node:
  - `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/graph.go`: `recallQueryManifolds` sets `ctx.Header = node.Header` and `expected.Header = node.Header`, so any mismatch between `node.Header` and stored manifold headers can suppress recall matches, indirectly preventing densification and leaving `survivorsWritten=0`.

## File Operations
### Read
- `/Users/theapemachine/go/src/github.com/theapemachine/six/experiment/task/pipeline.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/experiment/task/pipeline_test.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/geometry/manifold.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/kernel/cpu/cpu_backend.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/kernel/dispatch.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/kernel/metal/bitwise.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/store/prime_field.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/graph.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/node.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/reactions.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/think.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/ticker.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/cortex/token.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/loader.go`
- `/Users/theapemachine/go/src/github.com/theapemachine/six/vm/machine.go`

### Modified
- (none)
