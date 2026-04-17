# Execution Contracts

Goal: make the compiler responsible for semantic lowering and hardware shaping, while CPU / Metal / CUDA kernels remain dumb executors of lowered contracts.

This document replaces the accidental architecture where the signal-rotation sweep became the meaning of the kernel itself.

## Core rule

Kernels must never "understand" programs like link, affinity, cancel, merge, coupling, routing, or community logic.

Kernels should only do:
- read A
- read B
- apply OP
- write DST

Any shaping required to make that fast on SIMD / CUDA / Metal belongs to the compiler.

## Why this change is necessary

The current universal bitwise path hardcodes a 16-rotation sweep over B and folds A into four lanes. That is appropriate for signal-discovery programs where the point is to expose longest runs of zeros or ones, but it is not appropriate as the universal meaning of binary execution.

The Signals section of README.md makes the intended role clear:
- compare Values using XOR or AND
- observe the resulting signal
- longest zero-run or one-run reveals structure

That makes the sweep a program-specific lowering strategy, not a universal kernel semantic.

## Desired layering

### Layer 1: program semantics

Programs describe intent in config.yml source lines:
- compare these spans
- reduce these spans
- transfer these exact bits
- run a geometric product
- loop or branch via scheduler continuation

Programs do not directly describe hardware shape.

### Layer 2: compiler lowering

The compiler chooses a contract family per source line or per frame, and per target:
- exact transfer / overwrite
- sweep / signal discovery
- reduction
- geometric

The compiler may:
- pre-rotate B
- tile B cyclically
- pre-pack masks
- split one source program into multiple frames
- choose target-specific frame shapes

This is where optimization belongs.

### Layer 3: kernel execution

The kernel executes the already-lowered contract.

No kernel should contain semantic logic such as:
- "this is a link"
- "this is an affinity fold"
- "this is a cancel program"
- "this is community routing"

It only executes the contract presented by the frame.

### Finalizer

Finalizer is orchestration glue only.

Allowed responsibilities:
- inspect results already written in-band
- emit new Values when the completed program requires emission
- publish to orchestrator / gossip
- reschedule when scheduler metadata says so

Not allowed:
- emulating an ALU operation in Go
- repairing missing kernel semantics
- performing semantic computation that should have happened in-band

## Contract families

## Contract A: exact_binary

Intent:
- deterministic exact lane semantics
- exact overwrite / exact transfer / exact masked insert / exact bit placement

Examples:
- link
- exact property insertions
- exact scheduler or pointer-field updates
- exact state propagation where the destination must equal a computed bit pattern, not a signal signature

Kernel view:
- A is already aligned and packed for execution
- B is already aligned and packed for execution
- OP is exact binary semantics
- DST is the exact destination span

The kernel must not introduce any sweep, signature packing, or incidental reduction.

Compiler responsibilities:
- choose exact frame shape per target
- pre-align A and B for the destination lane
- precompute masks / shifts / inserts as needed
- lower one semantic operation into however many target-specific machine operations are needed

## Contract B: sweep_signal

Intent:
- structure discovery via aligned or rotated comparisons
- expose run structure used by Signals logic

Examples:
- cancel-like XOR comparisons
- merge-like AND comparisons
- affinity-like structural comparisons if they truly depend on sweep semantics
- unsupervised structural comparison

Kernel view:
- A and B are already arranged into a sweepable frame
- OP is the comparison operator for each sweep step
- DST receives raw signal output, not final semantics

Compiler responsibilities:
- pre-rotate or tile B if that helps the target
- choose sweep width and packing per hardware
- arrange frame so longest-run scanning has the needed raw material

Important: run scanning itself is not the kernel's universal meaning; it is the consumer of the produced signal.

## Contract C: reduce_binary

Intent:
- scalar or compact reductions

Examples:
- popcount
- numerator / denominator reductions for coupling
- scalar error or energy reductions

Kernel view:
- consume pre-lowered spans
- apply OP over the required domain
- emit reduced result directly to DST

Compiler responsibilities:
- determine whether reduction should happen directly or after an earlier signal frame
- choose packed reduction shape per target

## Contract D: geometric

Intent:
- geometric algebra lane execution

Examples:
- compose
- sandwich
- reverse

Kernel view:
- fixed geometric frame contract
- execute op over geometric lanes
- write result to DST

Compiler responsibilities:
- map source line into the geometric contract cleanly
- preserve exact lane meaning across CPU / Metal / CUDA

## Frame ABI direction

The current ABI mixes semantic meaning with one specific sweep lowering. That needs to be split.

Recommended direction:
- keep a small contract discriminator in the program region
- keep OP explicit
- keep A, B, and DST spans explicit
- allow per-contract auxiliary metadata fields for compiler-lowered shapes

Conceptually:
- contract kind
- op kind
- mode / writeback mode
- srcA descriptor
- srcB descriptor
- dst descriptor
- optional auxiliary words for target-specific lowered shapes

Examples of auxiliary words:
- sweep descriptors
- pre-rotation tables
- mask descriptors
- packed reduction descriptors

The key is that these are compiler-lowered shape details, not semantic shortcuts.

## Program classification (current config)

The current `programs:` block in `cmd/cfg/config.yml` should be classified as follows.

### link
- Current source:
  - asset[0,1] asset[0,1] prev[0,1] or accumulate
  - asset[1,1] asset[1,1] next[0,1] or accumulate
- Intended semantics:
  - exact transfer of staged IDs into prev/next
- Correct family:
  - exact_binary
- Notes:
  - must not rely on signal sweep semantics
  - exact field installation is required

### affinity
- Current source:
  - tokens[0,16] tokens[0,16] affinity[0,5] xor accumulate
- Intended semantics today:
  - compiler/hardware-optimized structural comparison producing a routing fingerprint
- Most likely family:
  - sweep_signal
- Notes:
  - this is one of the programs where a sweep-friendly lowering likely makes sense

### popcount
- Current source:
  - affinity[0,5] affinity[0,5] affinity[4,1] xor reduce
- Correct family:
  - reduce_binary
- Notes:
  - may be direct reduce or a tiny exact/reduce contract depending on implementation

### coupling
- Current source:
  - tokens[0,16] affinity[0,5] signals[0,1] and reduce
  - tokens[0,16] affinity[0,5] signals[1,1] or reduce
- Correct family:
  - reduce_binary
- Notes:
  - two reductions, likely lowered as two frames

### beam_swarm_step
- Mixed program
- Likely families by line:
  - xor comparisons over spans: sweep_signal or exact_binary depending on whether signal structure is intended
  - scalar error collapse: reduce_binary
  - affinity morph step: exact_binary if exact mutation semantics are needed
- Recommendation:
  - classify line-by-line, not as one family

### surprisal
- Current source:
  - tokens vs context -> signals via xor accumulate
  - signals -> properties via xor reduce
- Correct families:
  - sweep_signal followed by reduce_binary

### falsification
- Current source:
  - tokens vs context -> signals via xor accumulate
  - signals -> properties via xor reduce
  - properties -> ttl via and accumulate
- Correct families:
  - sweep_signal
  - reduce_binary
  - exact_binary or direct binary writeback for TTL update

### temperature
- Current source:
  - properties[4,1] affinity[0,5] affinity[0,5] xor accumulate
- Most likely family:
  - exact_binary
- Notes:
  - if this is intended as direct perturbation of affinity, it should not inherit signal-sweep semantics by accident

### active_inference
- Mixed program
- Correct classification:
  - multiple families across lines
- Recommendation:
  - compile into multiple frames with explicit per-line contract choice

### causal_explore
- Current source mixes comparison, accumulation, and reduction
- Correct families:
  - sweep_signal + reduce_binary + exact_binary writeback

### causal_hub
- Same classification as causal_explore, with scheduler continuation
- Correct families:
  - sweep_signal + reduce_binary + exact_binary writeback

### unsupervised_learn
- Current source:
  - staged token spans compared into signals
  - signals reduced into properties
- Correct families:
  - sweep_signal + reduce_binary

### measure_field
- Current source:
  - asset[0,8] asset[0,8] signals[7,1] or reduce
- Correct family:
  - reduce_binary
- Notes:
  - if the source really wants a sweep-produced signature before reduction, then it is sweep_signal + reduce_binary, but the current semantic goal is scalar energy in signals[7]

## What not to do

1. Do not invent CPU-only semantic opcodes.
2. Do not let the backend decide semantics because a program is inconvenient.
3. Do not force exact programs into sweep lowering.
4. Do not restore end-to-end link tests until a shared exact contract exists across CPU / Metal / CUDA.
5. Do not use the finalizer to emulate missing ALU behavior.

## Implementation order

1. Introduce contract vocabulary into compiler internals.
2. Refactor builder/compiler so not every binary line lowers to the same sweep contract.
3. Implement the first shared exact_binary contract across CPU / Metal / CUDA.
4. Reclassify and relower `link` into exact_binary.
5. Restore end-to-end link -> affinity -> route regression test.
6. After that, begin community-local ValueID addressing design.

## Verification baseline

Current baseline command for the active surface:
- `go test -ldflags='-checklinkname=0' ./pkg/compute/kernel/cpu ./pkg/compute/firmware ./pkg/compute ./pkg/vm`

Known unrelated repo-wide failures:
- `experiment/evaluator_test.go`
- `pkg/core/config_test.go`

These should not be treated as execution-contract regressions.
