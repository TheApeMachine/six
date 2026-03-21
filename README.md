<p align="center">
  <img src="docs/infographic.jpg" width="680" alt="Six Architecture Infographic" />
</p>

<h1 align="center">six</h1>

<p align="center">
  <strong>A self-programming spatial VM that replaces gradient descent with modular arithmetic.</strong><br/>
  8191-bit Mersenne field · Topology-guided folding · Homoiconic value medium
</p>

<p align="center">
  <a href="#quick-start">Quick Start</a> ·
  <a href="#core-thesis">Core Thesis</a> ·
  <a href="#architecture">Architecture</a> ·
  <a href="#codebase-map">Codebase Map</a> ·
  <a href="#experiments">Experiments</a> ·
  <a href="#roadmap">Roadmap</a>
</p>

---

> [!NOTE]
> This is a research project under active development.
> Certain architectural decisions are built for speed, not for comfort.
> The project actively seeks critique and feedback.

---

## Core Thesis

This project started from a single question:

> **Can we reject gradient descent and backpropagation long enough to convince ourselves that we may not need them?**

The answer is a spatial virtual machine whose native medium is `primitive.Value` — an 8191-bit core in the Mersenne field GF(8191) wrapped in a shell carrying affine operators, control flow, trajectory, and routing. In this field every non-zero element has a multiplicative inverse, rotations are primitive operations with no dead zones, and collision between stored values **is** compression.

The system does not predict the next token. It solves for the **longest span** — a Boundary Value Problem where the architecture extends a cantilever into unknown territory and locks onto the nearest stable resonance in its stored memory.

The central architectural goal is **self-programmability**: `primitive.Value` is simultaneously data, instruction, operator, control flow, routing address, and memory. Learned behaviors are reified back into Values and executed natively, making the system's reasoning medium homoiconic.

### The Three Pillars

| Pillar | One-Liner | Mathematical Basis |
|:---|:---|:---|
| **Collision IS Compression** | Multiple byte sequences that share `(byte, localDepth)` collapse onto the same cell. The value carries continuation logic, not redundant identity. | A radix cell address `(byte, localDepth)` resets at each sequencer boundary. |
| **Rotation IS Data** | Sequence order is encoded as generative coordinate transforms, not positional tags. No XOR composition — rotation preserves structure. | $f(x) = (ax + b) \bmod{8191}$ — an affine group with ~67M distinct states. Rotation produces translation. |
| **Resonance IS Retrieval** | Queries don't search; they excite the field at a frequency and follow constructive interference. | `popcount(A & B)` = dot product of binary vectors = Hamming similarity. XOR is measurement, never storage. |

### Why Not Semantics?

This project does not treat language semantics as the machine's native substrate.

The reasoning layer is algebraic. A human vocabulary is roughly $10^5$ words. The 8191-bit native value type has $2^{8191}$ possible states — a vast representational space, but representational size alone does not guarantee practical capability. Capability still depends on whether operator hardening yields reusable transforms that generalize under real workloads.

> [!IMPORTANT]
> Semantics is treated as a projection layer for evaluation and interaction, not as the ontology of computation. The objective is to reason in the native monotype (`primitive.Value`) and then project results back into language for measurement.

> Six is a finite-field associative machine where computation is affine rotation, memory is a collision-compressed spatial lattice, and language is merely a projection layer for human interaction.

The system does not parse meaning. It does not build ontologies. It does not model synonyms or grammar rules internally. The projection layer exists so humans can read the output and evaluate experiments. It is the GUI, not the CPU.

The doctrine in three lines:

```
The address space is storage, not intelligence.
The value is intelligence, not payload.
Semantics is projection, not ontology of execution.
```

---

## Mathematical Foundations

### The Field: $\text{GF}(8191)$

$8191 = 2^{13} - 1$ is a Mersenne prime. This gives the field a critical hardware property — branchless reduction via bit masking:

$$a \bmod 8191 = (a \mathbin{\&} \text{0x1FFF}) + (a \gg 13) \quad \text{(iterated until result fits)}$$

$$\forall\, a \in [1, 8190]: \quad a^{-1} \equiv a^{8189} \pmod{8191} \quad \text{(Fermat's Little Theorem)}$$

Every non-zero element has a multiplicative inverse. No dead zones, no special cases, no division hardware needed.

### Affine Rotations (The Native Instruction Set)

The system's core operation is an affine transform over the field:

$$f(x) = (a \cdot x + b) \bmod{8191}, \quad a \in [1, 8190],\; b \in [0, 8190]$$

- **Composition** is $O(1)$: applying $f_1$ then $f_2$ yields a single $(a', b')$ pair:

$$a' = a_2 \cdot a_1 \bmod{8191}, \quad b' = (a_2 \cdot b_1 + b_2) \bmod{8191}$$

- **Inversion** is $O(1)$: $f^{-1}(y) = a^{-1}(y - b) \bmod{8191}$

The affine group has $8191 \times 8190 = 67{,}124{,}890$ distinct transforms.

### Higher-Dimensional Value Geometry

The 8191-bit core is a sparse occupancy map over a prime basis. Core index `k` maps to `numeric.Primes[k]`, so each active bit corresponds to one prime frequency component. The shell (3 additional 64-bit blocks) carries the affine operator, opcode, trajectory, guard radius, route hint, and flags.

This is why the system can treat bitwise operators as algebra over prime-factor structure:

- `OR` behaves as superposition over basis occupancy (LCM-style composition).
- `AND` behaves as strict shared structure extraction (GCD-style intersection).
- `Hole(a, b) = a & ~b` isolates what remains after exact basis cancellation.
- `XOR` is measurement only — comparing a projected rotation against a stored anchor.

### Morton Addressing & Radix Compression

A cell address packs byte identity and boundary-local depth into a single 64-bit key:

$$\text{CellKey} = \text{Pack}(\underbrace{v}_{\text{byte value (X)}},\; \underbrace{d}_{\text{local depth (Y)}})$$

The local depth $d$ **resets to 0** at each sequencer boundary. This means distinct byte sequences that share $(v, d)$ collapse onto the same cell:

$$|\text{cells}| \ll |\text{tokens}| \quad \Rightarrow \quad \text{compression ratio} = \frac{|\text{tokens}|}{|\text{cells}|}$$

### Algebraic Cancellation

Given stored facts as multiplicative braids in $\text{GF}(8191)$:

$$\phi_{\text{stored}} = (\text{Roy} \cdot \text{is}\_\text{in} \cdot \text{Kitchen}) \bmod{8191}$$

A prompt asking "Where is Roy?" computes the modular inverse cancellation:

$$\text{result} = \phi_{\text{stored}} \cdot \text{Roy}^{-1} \cdot \text{is}\_\text{in}^{-1} \bmod{8191}$$

If the factorization is exact and the encoding remains collision-free for the participating factors, the shared structure cancels algebraically:

$$\text{result} = \text{Kitchen} \bmod{8191}$$

In this idealized case, retrieval is resolved by algebraic calculation rather than an explicit lexical scan.

### Frustration (The Drive Signal)

When the system "believes" it should be at target phase $\phi_t$ but is actually at current phase $\phi_c$, the frustration is the **unresolved rotation**:

$$\Delta = \phi_t \cdot \phi_c^{-1} \bmod{8191}$$

When $\Delta = 1$, the system is phase-locked (frustration zeroed). When $\Delta \neq 1$, the system searches its macro-index for a tool whose rotation matches $\Delta$.

### Tool Synthesis & Hardening

When the cantilever hits a gap it cannot bridge, the missing tool is:

$$Z = \phi_{\text{goal}} \cdot \phi_{\text{failed}}^{-1} \bmod{8191}$$

If applying $Z$ consistently bridges similar gaps, it is hardened as a permanent macro-opcode. Hardened opcodes are encoded as `primitive.Value` with `OpcodeMacro`, making them first-class citizens of the native medium — the system programs itself.

---

## Architecture

The system is organized around `primitive.Value` as its universal medium, with four conceptual planes implemented across two package trees.

```
┌─────────────────────────────────────────────────────────┐
│            PROJECTION PLANE (Human Interface)           │
│   Byte recovery via un-Morton · Experiment evaluation   │
├─────────────────────────────────────────────────────────┤
│           LOGIC PLANE  (pkg/logic/)                     │
│   Topology-guided recursive fold · BVP cantilever       │
│   Multi-stage prompt resolution · Macro skip index      │
│   Category-theoretic functors · Persistence diagrams    │
├─────────────────────────────────────────────────────────┤
│           VALUE PLANE  (pkg/logic/lang/primitive/)      │
│   The homoiconic native monotype · GF(8191) core        │
│   Shell: opcodes, affine, trajectory, routing, guards   │
│   OpcodeMacro: learned operators as first-class Values  │
│   Stored values are local operators, not byte shadows   │
├─────────────────────────────────────────────────────────┤
│           ADDRESS PLANE  (pkg/store/dmt/)               │
│   Radix-trie forest · Morton-keyed deterministic lookup │
│   Collision = compression (radix cell collapse)         │
│   Batch RPC transport (Cap'n Proto)                     │
├─────────────────────────────────────────────────────────┤
│           COMPUTE PLANE  (pkg/compute/)                 │
│   CPU/GPU/Metal dispatch · Typed Go slices (SSA-safe)   │
│   Distributed Cap'n Proto routing                       │
└─────────────────────────────────────────────────────────┘
```

### Address Plane — `pkg/store/dmt/`

The storage layer is a radix-trie forest ([`forest.go`](pkg/store/dmt/forest.go)) backed by `hashicorp/go-immutable-radix`. It holds Morton-keyed cells. Each cell address is `(byte_value, local_depth)` where the depth resets at every sequencer boundary — this is what makes collision compressive rather than destructive.

- **Self-Addressing.** Each byte value (0–255) determines the X coordinate. The Y coordinate is the local depth within the current chunk. The byte is recovered from the key, not from the stored value.
- **Batch Transport.** The `ForestServer` accepts `WriteBatch` RPCs that deliver all keys in a single Cap'n Proto call, eliminating per-key RPC overhead.
- **O(1) Retrieval.** Given a coordinate, the lookup is a direct Morton key dereference.

### Value Plane — `pkg/logic/lang/primitive/`

The value at each cell is the system's **native monotype**: a `primitive.Value` ([`value.go`](pkg/logic/lang/primitive/value.go)). This is the machine's actual reasoning substrate. It is not a byte fingerprint. It is a local operator — and when it carries `OpcodeMacro`, it is a learned program.

```
 Bits 0–8190     │ GF(8191) Mersenne core — the native execution state
                 │ (128 × uint64 blocks + partial last block)
 Shell block 0   │ Residual phase carry (cross-wavefront state)
 Shell block 1   │ Opcode register: instruction, jump, branches, terminal
                 │ Route hint (bits 8-15): continuation class for dispatch
 Shell block 2   │ Affine operator: scale (13 bits), translate (13 bits)
                 │ Trajectory: from/to phase snapshots (13 bits each)
                 │ Guard radius (7 bits), active flag, operator flags
```

The 8191-bit core lives inside a hardware jacket sized for GPU alignment. `CoreActiveCount()` measures the Mersenne field; `ActiveCount()` spans core + shell.

Key operations:

| Operation | Code | Role |
|:---|:---|:---|
| `SetAffine()` / `Affine()` | $f(x) = ax + b \pmod{8191}$ | Store/retrieve the local affine operator. |
| `ApplyAffinePhase()` | phase → transformed phase | Execute the affine transform on a running state. |
| `SetTrajectory()` / `Trajectory()` | phase → phase snapshot | Local orbit memory for steering the next hop. |
| `SetRouteHint()` / `RouteHint()` | 8-bit device class | Route bias for interpreter dispatch. |
| `SetGuardRadius()` / `GuardRadius()` | modular drift budget | Tolerance for re-alignment and backtracking. |
| `SetProgram()` / `Program()` | opcode, jump, branches, terminal | Threaded-code instruction in the opcode block. |
| `EncodeMacroOperator()` | scale, translate, key → Value | Encode a learned affine operator as a first-class Value. |
| `IsMacroOperator()` / `MacroAffine()` | — | Identify and extract learned operators from Values. |
| `OR(other)` | Bitwise OR | Superposition — accumulate context. |
| `AND(other)` | Bitwise AND | Intersection — find shared structure. |
| `Hole(a, b)` | `a & ~b` | Gap detection — what is needed but absent. |
| `XOR(other)` | Bitwise XOR | **Measurement only.** Never used for composition. |

> [!WARNING]
> **XOR is measurement, not storage.** If XOR is used to compose or persist data, Shannon entropy enters the system and destroys the generative properties of the rotational algebra.

### Logic Plane — `pkg/logic/`

The logic layer comprises several interacting subsystems:

- **Topology-Guided Recursive Fold.** [`substrate/graph.go`](pkg/logic/substrate/graph.go) builds a persistent hierarchical graph by discovering shared structural invariants. Merge ordering is determined by Jaccard similarity (via [`topology/persistence.go`](pkg/logic/topology/persistence.go)), not arbitrary midpoint splits. At each level: `AND` extracts the shared label, `Hole` extracts directional residues, and the fold recurses on the residues until no shared structure remains. The result is a `FoldGraph` of persistent `FoldNode`s queryable at prompt time via `FoldLookup`.

- **BVP Cantilever Solver.** [`synthesis/bvp/cantilever.go`](pkg/logic/synthesis/bvp/cantilever.go) implements multi-stage prompt resolution: (1) exact lexical continuation from the stored corpus, (2) operator-mediated bridging via the MacroIndex (with approximate nearest-neighbor fallback). Successful bridges feed `RecordCandidateResult` to close the hardening loop.

- **Macro Index.** [`synthesis/macro/macro_index.go`](pkg/logic/synthesis/macro/macro_index.go) stores discovered affine operators indexed by their full geometric `AffineKey` (XOR delta of start/goal Values). Exact lookup is supplemented by approximate nearest-neighbor search in PhaseDial embedding space: hardened opcodes get projected into 512-D complex vectors via `EmbedKey`, and cosine similarity finds the closest known operator for novel gaps.

- **HAS (Holographic Auto-Synthesizer).** [`synthesis/has.go`](pkg/logic/synthesis/has.go) forges affine tools during ingestion and performs query-mask matching during inference.

- **Category-Theoretic Functors.** [`category/functor.go`](pkg/logic/category/functor.go) maps morphisms between MacroIndex categories via Procrustes alignment in PhaseDial space, enabling cross-domain operator transfer.

- **Topological Persistence.** [`topology/persistence.go`](pkg/logic/topology/persistence.go) computes Betti numbers and persistence diagrams over Value streams via Jaccard-similarity filtration, tracking connected components (H₀), loops (H₁), and their birth-death events.

### Interpreter — `pkg/system/vm/processor/`

The [`InterpreterServer`](pkg/system/vm/processor/interpreter.go) is a register-machine that executes programs encoded as `[]primitive.Value`. Each Value is simultaneously an instruction and an operand:

| Opcode | Behavior |
|:---|:---|
| `OpcodeNext` | Advance `pc++`, apply affine transform to running phase. |
| `OpcodeJump` | `pc += Jump()`, non-zero jump offset. |
| `OpcodeBranch` | Fork: evaluate all candidates via `BatchEvaluateInto`, pick lowest-residue winner. |
| `OpcodeReset` | Reset phase to identity, `pc++`. |
| `OpcodeHalt` | Emit accumulated state, stop. |
| **`OpcodeMacro`** | Apply a learned affine operator (scale, translate from the Value's shell) to the running phase. This is how the system executes its own discoveries. |

The interpreter records an `ExecutionStep` trace for every instruction. Successful traces can be reified back into a single `OpcodeMacro` Value via `ReifyTrace()`, composing the affine chain into one operator. This closes the self-programming loop: observe → execute → trace → reify → harden → execute natively.

### Machine — `pkg/system/vm/`

The [`Machine`](pkg/system/vm/machine.go) is the top-level orchestrator. It wires the tokenizer, forest, graph, cantilever, HAS, macro index, and interpreter through a Cap'n Proto RPC router ([`cluster/`](pkg/system/cluster/)).

- **Ingest** (`SetDataset`): streams raw bytes through the tokenizer via batch RPC, stores Morton keys in the Forest and Graph, triggers topology-guided recursive folding, and feeds HAS with boundary pairs for tool synthesis.
- **Prompt** (`Prompt`): delegates to the CantileverServer for multi-stage resolution (exact → operator-mediated → error).

---

## Codebase Map

### Layer 1 — Primitive Value

> The homoiconic native monotype and GF(8191) field.

| Concept | File | What It Does |
|:---|:---|:---|
| Native Value | [`pkg/logic/lang/primitive/value.go`](pkg/logic/lang/primitive/value.go) | 8191-bit core + 3-block shell, Cap'n Proto backed |
| Bitwise Ops | [`pkg/logic/lang/primitive/bitwise.go`](pkg/logic/lang/primitive/bitwise.go) | `OR`, `AND`, `XOR`, `Hole`, zero-alloc `*Into` variants |
| Opcode ISA | [`pkg/logic/lang/primitive/opcode.go`](pkg/logic/lang/primitive/opcode.go) | Next, Jump, Branch, Reset, Halt, **Macro** |
| Shell Layout | [`pkg/logic/lang/primitive/shell.go`](pkg/logic/lang/primitive/shell.go) | Affine, trajectory, guard radius, active flag |
| Route Hint | [`pkg/logic/lang/primitive/route.go`](pkg/logic/lang/primitive/route.go) | Device dispatch class in opcode block |
| Program Compiler | [`pkg/logic/lang/primitive/program.go`](pkg/logic/lang/primitive/program.go) | Morton keys → `SequenceCell` → `[]Value` |
| GF(8191) Numerics | [`pkg/numeric/calculus.go`](pkg/numeric/calculus.go) | Mersenne reduction, field primitives, Primes table |
| Phase Geometry | [`pkg/numeric/geometry/phase.go`](pkg/numeric/geometry/phase.go) | PhaseDial: 512-D complex embeddings, cosine similarity |

### Layer 2 — Storage

> Radix-trie forest and Morton addressing.

| Concept | File | What It Does |
|:---|:---|:---|
| Forest (Radix Trie) | [`pkg/store/dmt/forest.go`](pkg/store/dmt/forest.go) | Immutable radix trie insert/lookup |
| Forest RPC Server | [`pkg/store/dmt/server/server.go`](pkg/store/dmt/server/server.go) | Cap'n Proto `Write`, `WriteBatch`, `Done`, `Branches` |
| Morton Keys | [`pkg/store/data/morton.go`](pkg/store/data/morton.go) | `Pack(symbol, depth)` / `Unpack(key)` |

### Layer 3 — Sensory Processing

> Byte-stream segmentation and boundary detection.

| Concept | File | What It Does |
|:---|:---|:---|
| Tokenizer | [`pkg/system/process/tokenizer/server.go`](pkg/system/process/tokenizer/server.go) | Bytes → Morton keys via `Write`, `WriteBatch`, `Done` |
| Sequencer (Sequitur) | [`pkg/system/process/sequencer/sequitur.go`](pkg/system/process/sequencer/sequitur.go) | Grammar-based hierarchical chunking |

### Layer 4 — Logic & Synthesis

> Recursive folding, BVP solving, operator learning, topological analysis.

| Concept | File | What It Does |
|:---|:---|:---|
| Graph Substrate | [`pkg/logic/substrate/graph.go`](pkg/logic/substrate/graph.go) | Topology-guided recursive fold, `FoldLookup`, `ExactContinuation` |
| Topological Persistence | [`pkg/logic/topology/persistence.go`](pkg/logic/topology/persistence.go) | Jaccard-filtration persistence diagrams, `Sweep`, Betti numbers |
| UnionFind | [`pkg/logic/topology/unionfind.go`](pkg/logic/topology/unionfind.go) | Weighted union-find with path compression |
| BVP Cantilever | [`pkg/logic/synthesis/bvp/cantilever.go`](pkg/logic/synthesis/bvp/cantilever.go) | Multi-stage prompt: exact → operator-mediated, with hardening feedback |
| HAS | [`pkg/logic/synthesis/has.go`](pkg/logic/synthesis/has.go) | Ingestion-time tool forging, inference-time query-mask evaluation |
| Macro Index | [`pkg/logic/synthesis/macro/macro_index.go`](pkg/logic/synthesis/macro/macro_index.go) | Exact + approximate (PhaseDial NN) operator lookup, hardening, `RecordResult` RPC |
| Category Functors | [`pkg/logic/category/functor.go`](pkg/logic/category/functor.go) | Cross-domain operator transfer via Procrustes alignment |

### Layer 5 — Compute Backend

> CPU/GPU/Metal dispatch for parallel resolution.

| Concept | File | What It Does |
|:---|:---|:---|
| Dispatch | [`pkg/compute/kernel/dispatch.go`](pkg/compute/kernel/dispatch.go) | Backend selection (Metal, CUDA, CPU fallback) |
| CUDA Kernel | [`pkg/compute/kernel/cuda/resolver.cu`](pkg/compute/kernel/cuda/resolver.cu) | GF(8191) distance resolution |
| Metal Kernel | [`pkg/compute/kernel/metal/`](pkg/compute/kernel/metal/) | Apple Silicon equivalent |
| Distributed | [`pkg/compute/kernel/distributed.go`](pkg/compute/kernel/distributed.go) | Strict no-fallback remote dispatch |

### Layer 6 — Machine & Runtime

> The top-level orchestrator and interpreter.

| Concept | File | What It Does |
|:---|:---|:---|
| Machine | [`pkg/system/vm/machine.go`](pkg/system/vm/machine.go) | `SetDataset` (batch ingest), `Prompt` (multi-stage resolution) |
| Booter | [`pkg/system/vm/booter.go`](pkg/system/vm/booter.go) | RPC server lifecycle and capability registration |
| Interpreter | [`pkg/system/vm/processor/interpreter.go`](pkg/system/vm/processor/interpreter.go) | Register machine with OpcodeMacro dispatch, execution tracing, trace reification |
| Cluster Router | [`pkg/system/cluster/`](pkg/system/cluster/) | Capability-based service routing |
| Worker Pool | [`pkg/system/pool/`](pkg/system/pool/) | Bounded-wait job dispatch with scaler |

---

## The Self-Programming Loop

The defining architectural feature is that learned behaviors become native instructions. The loop:

```
1. OBSERVE    Ingest data → tokenize → Morton keys → Values
                          → topology-guided recursive fold (graph)
                          → HAS boundary synthesis (MacroIndex candidates)

2. PROMPT     Query arrives → exact continuation?
              No → operator-mediated bridging (MacroIndex lookup)
                → exact match? → apply
                → no exact → approximate NN in PhaseDial space → apply
              Report bridge result → RecordCandidateResult

3. HARDEN     Repeated successful bridges increment UseCount
              UseCount > threshold → opcode.Hardened = true
              Hardened opcodes get PhaseDial embeddings for NN search
              MacroOpcode.ToValue() encodes as primitive.Value with OpcodeMacro

4. EXECUTE    Interpreter encounters OpcodeMacro in program
              → applies learned affine operator to running phase
              → records execution trace (ExecutionStep per instruction)

5. REIFY      Successful trace → ReifyTrace()
              → composes affine chain into single (scale, translate)
              → encodes as new OpcodeMacro Value
              → feeds back into MacroIndex for future use

              ┌──────────────────────────────────┐
              │  Value = Data = Instruction =    │
              │  Operator = Route = Memory       │
              │  The medium programs itself.     │
              └──────────────────────────────────┘
```

---

## Quick Start

### Prerequisites

- **Go 1.25+**
- **Cap'n Proto** compiler (for schema regeneration)
- **Metal** (macOS) or **CUDA** toolkit (Linux/Windows) for GPU acceleration

### Build

```bash
# Regenerate Cap'n Proto bindings and compile GPU kernels
make build

# Or just the Cap'n Proto schemas
make capnp

# Or just Metal shaders (macOS)
make metal
```

### Run Experiments

```bash
# Run all experiments (generates LaTeX paper artifacts)
make paper

# Run a single experiment
make pprof EXP=Text_Classification

# Run tests
go test ./...
```

### Project Structure

```
six/
├── cmd/                          # CLI entry points
├── pkg/
│   ├── compute/kernel/           # GPU backends (CUDA, Metal, CPU)
│   ├── errnie/                   # Error handling + state tracking
│   ├── logic/
│   │   ├── lang/primitive/       # The native Value monotype
│   │   ├── substrate/            # Topology-guided recursive fold graph
│   │   ├── topology/             # Persistence diagrams, UnionFind
│   │   ├── synthesis/            # BVP cantilever, HAS, MacroIndex
│   │   └── category/             # Functors, natural transformations
│   ├── numeric/                  # GF(8191) math, geometry, phase
│   ├── store/
│   │   ├── data/                 # Morton coder, dataset providers
│   │   └── dmt/                  # Radix-trie forest, server RPC
│   ├── system/
│   │   ├── core/                 # Configuration constants
│   │   ├── cluster/              # Capability-based service routing
│   │   ├── process/              # Tokenizer, sequencer
│   │   ├── pool/                 # Worker pool + broadcast groups
│   │   └── vm/                   # Machine orchestrator, interpreter
│   ├── telemetry/                # Observability
│   └── validate/                 # Invariant checks
├── experiment/                   # Experiment harness + task definitions
│   └── task/                     # classification, textgen, logic
├── paper/                        # LaTeX research paper (auto-generated)
├── docs/                         # Design documents
└── Makefile                      # Build, test, and paper generation
```

---

## Experiments

All experiments use the full `vm.Machine` pipeline with real data. No oracles, no faked results.

Experiments are structured as GoConvey BDD tests in `experiment/task/pipeline_test.go`. Each task:

1. Boots the full machine (tokenizer, forest, graph, cantilever, HAS, macro index)
2. Ingests a real dataset via batch transport
3. Runs prompts through the multi-stage resolution pipeline
4. Asserts observable outputs via `gc.So`
5. Generates LaTeX artifacts for the research paper

---

## Roadmap

### Implemented

- [x] GF(8191) Mersenne field with branchless reduction
- [x] 8191-bit value substrate (Cap'n Proto, zero-copy RPC)
- [x] Morton-keyed radix-trie forest
- [x] MDL + Sequitur dual-track sequence boundary detection
- [x] BVP cantilever solver with multi-stage prompt resolution
- [x] Topology-guided recursive folding with persistent FoldNode hierarchy
- [x] Macro index with exact + approximate (PhaseDial NN) operator lookup
- [x] Hardening feedback loop: prompt-time bridge results → RecordCandidateResult
- [x] MacroOpcode as Value (OpcodeMacro): learned operators as first-class Values
- [x] Execution trace recording and reification (ReifyTrace → composed affine Value)
- [x] Interpreter macro dispatch: native execution of learned operators
- [x] Batch RPC transport (WriteBatch) for tokenizer, forest, and graph
- [x] Category-theoretic functors for cross-domain operator transfer
- [x] Topological persistence diagrams (H₀, H₁, Betti numbers)
- [x] CUDA + Metal + CPU compute backends
- [x] Full experiment harness with LaTeX paper generation
- [x] Operator shell: affine, trajectory, route hints, guard radii, flags
- [x] Autonomous tool building: gap detection → synthesis → bridging → hardening → native execution
- [x] Tool composition: interpreter executes Value sequences (series); `ReifyTrace` composes traces into single operators
- [x] Recursive meta-tools: `ReifyTrace` condenses traces containing `OpcodeMacro` steps into new operators (tools that build tools)

### In Development

- [ ] **Graph fold storage.** Persist FoldNode hierarchy into the Forest so fold products survive across sessions and enable graph-structural prompt resolution.
- [ ] **Execution trace hardening.** Automatically feed ReifyTrace products into the MacroIndex so the interpreter's successful traces become available as macro operators.
- [ ] **Distributed phase sync.** Peer-to-peer index merging across nodes with latency-aware timeout and phase reconciliation.
- [ ] **Spatial paging strategy.** Prefetching Morton clusters into GPU shared memory to respect PCIe bandwidth constraints.

### Research Horizon

- [ ] **Cross-modal anchoring.** Text, image, and sensor data share a label phase. The anchor is multiplicatively injected during ingest. Query inverts the label, and all modalities that carry it resonate simultaneously.
- [ ] **Autonomous curiosity.** The system scans its own index for low-resonance gaps and synthesizes new macro-opcodes during idle cycles without human prompting.

---

## Citation

This project is documented in a companion research paper generated automatically from experiment results. See [`paper/`](paper/) for the LaTeX source.

---

## License

See [LICENSE](LICENSE) for details.
