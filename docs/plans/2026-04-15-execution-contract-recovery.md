# Execution Contract Recovery Plan

> For Hermes: use this plan to get the execution model back on track. Do not introduce substrate-specific semantic hacks. Kernels must remain dumb executors; compiler owns lowering.

Goal: restore a clean architecture where programs express intent, the compiler lowers each program into a target-appropriate frame contract, and CPU/Metal/CUDA kernels only execute A + B + OP -> DST style contracts without embedded semantic policy.

Architecture:
- Programs in config.yml define intent, not hardware shape.
- Compiler selects a contract family per program and per target.
- Kernels execute only the lowered contract.
- Finalizer stays minimal: inspect outputs, emit new Values if required, reschedule loops/branches through in-band scheduler metadata.

Tech stack: Go, custom compiler/lowering in pkg/compute/firmware, CPU/Metal/CUDA kernels in pkg/compute/kernel/*, config-driven programs in cmd/cfg/config.yml.

---

## Non-negotiable architectural rules

1. Kernels must not encode program semantics like cancel/merge/link/community logic.
2. Rotation sweep is a lowering strategy for signal-style programs, not the universal kernel semantic.
3. No host-side registries for ValueID addressing.
4. No substrate-only opcodes unless implemented consistently across CPU, Metal, and CUDA.
5. End-to-end tests must only be added for behavior that exists across the shared contract, not a single backend hack.

---

## Immediate stabilization status

Keep:
- Parser support for trailing `next self` / `next <uint64>`.
- Executable continuation materialization into word 117.
- Resident-program path in backend/orchestrator after config rules no longer match.
- vm TestMain loading cmd/cfg/config.yml.
- Publish staging both asset[0] and asset[1].

Do not keep:
- Any CPU-only semantic opcode for exact copy.
- Any test that asserts end-to-end link semantics before a cross-substrate exact contract exists.

---

## Contract families to introduce

### Contract A: exact_binary
Intent:
- deterministic exact lane semantics
- examples: link, exact overwrite/insert, exact transfer, exact masked operations

Kernel contract:
- read A span exactly as laid out
- read B span exactly as laid out
- apply OP exactly
- write DST exactly
- no sweep/signature generation

Compiler responsibilities:
- pre-align source/destination spans
- pre-pack any masks or shifts required by target
- choose the best exact execution shape per substrate

### Contract B: sweep_signal
Intent:
- structure discovery via XOR/AND across aligned/rotated comparisons
- examples: cancel, merge, affinity-like structural comparisons if still appropriate

Kernel contract:
- execute the pre-lowered sweep/tiling prepared by compiler
- emit raw signal output to DST

Compiler responsibilities:
- pre-rotate/tile B if helpful
- layout sweepable frame for SIMD/GPU
- choose signal-friendly lowering per target

### Contract C: reduce_binary
Intent:
- scalar reductions/popcount/reduction outputs

Kernel contract:
- execute binary operation over pre-lowered spans
- write scalar or compact reduced result to DST

### Contract D: geometric
Intent:
- geometric algebra lane operations

Kernel contract:
- execute geometric op on fixed geometric frame layout

---

## Program classification pass

Map current config programs into contract families before coding:

- link -> exact_binary
- affinity -> likely sweep_signal or reduce_binary depending on true intended semantics
- popcount -> reduce_binary
- coupling -> reduce_binary
- beam_swarm_step -> mixed; likely split into smaller program lines and classify each lowered frame
- surprisal -> likely sweep_signal + reduce_binary
- falsification -> likely sweep_signal + reduce_binary
- temperature -> exact_binary or simple binary depending on true semantics
- active_inference -> mixed; requires decomposition into per-line contracts
- causal_explore -> sweep_signal + reduce_binary
- causal_hub -> sweep_signal + reduce_binary
- unsupervised_learn -> sweep_signal + reduce_binary
- measure_field -> reduce_binary or signal-style depending on intended output shape

Important: mixed programs may compile into multiple frames using different contracts. One source program does not have to map to one kernel semantic.

---

## Implementation order

### Phase 1: contract design doc
Objective: freeze vocabulary and ABI direction before further coding.

Files:
- Create: docs/plans/2026-04-15-execution-contract-recovery.md (this file)
- Create: docs/architecture/execution-contracts.md

Output must define:
- contract families
- per-contract frame fields
- which existing programs use which contracts
- what finalizer is allowed to do

### Phase 2: compiler refactor design
Objective: move semantic choice into compiler.

Files to inspect/modify later:
- pkg/compute/firmware/compiler.go
- pkg/compute/firmware/builder.go
- pkg/compute/firmware/token.go
- pkg/compute/firmware/frame.go

Required direction:
- compiler chooses lowering strategy by program/line intent
- builder no longer assumes one universal sweep lowering for all binary programs

### Phase 3: implement first shared exact contract
Objective: enable link honestly across all substrates.

Files:
- pkg/compute/kernel/layout.go
- pkg/compute/kernel/cpu/*
- pkg/compute/kernel/metal/*
- pkg/compute/kernel/cuda/*
- pkg/compute/backend.go
- cmd/cfg/config.yml

Rules:
- same contract visible to all three substrates
- no backend-only semantics
- backend dispatch may choose substrate capability, but semantics must be shared

### Phase 4: re-enable end-to-end link test
Objective: only after exact contract exists on CPU/Metal/CUDA.

Files:
- pkg/vm/orchestrator_test.go

Assertions:
- prev/next exact values
- affinity generated after link
- routing/community assignment after affinity

### Phase 5: community-local addressing design
Objective: only after residency is real.

Files to inspect later:
- pkg/vm/router.go
- pkg/core/numeric/geometry/field.go
- pkg/gossip/*

Rules:
- no global registry
- calls propagate via gossip
- community fields know local residents
- addressed resident schedules itself onto pool

---

## Decision checklist before writing code

Before any kernel/compiler edit, answer all of these:

1. Is this semantic intent exact transfer, signal discovery, reduction, or geometric?
2. Is the proposed behavior in the compiler or the kernel?
3. Would the same semantics exist on CPU, Metal, and CUDA?
4. Does this introduce a host-side cheat?
5. Is the finalizer doing orchestration only, not semantic emulation?

If any answer is wrong, stop and redesign first.

---

## Recommended next move

The design pass is now written in:
- docs/architecture/execution-contracts.md

Next move should be:
1. introduce contract vocabulary into compiler internals
2. refactor builder/compiler so not every binary line lowers to the sweep contract
3. choose the first exact-binary shared contract for link
4. implement it across CPU, Metal, and CUDA
5. then restore the end-to-end regression

---

## Verification commands

Current baseline:
- go test -ldflags='-checklinkname=0' ./pkg/compute/kernel/cpu ./pkg/compute/firmware ./pkg/compute ./pkg/vm

Repo-wide known unrelated failures:
- make test
- currently fails in experiment/evaluator_test.go and pkg/core/config_test.go
- do not treat those as execution-contract regressions
