<p align="center">
  <img src="docs/infographic.jpg" width="680" alt="Six Architecture Infographic" />
</p>

<h1 align="center">six</h1>

<p align="center">
  <strong>A holographic memory engine that replaces gradient descent with modular arithmetic.</strong><br/>
  257-bit Fermat field · Morton spatial indexing · Active inference cortex
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

The answer is a holographic data structure built on the 4th Fermat prime ($2^8 + 1 = 257$). In this field, every non-zero element has a multiplicative inverse, rotations are primitive operations with no dead zones, and collision between chords **is** compression.

The system does not predict the next token. It solves for the **longest span** — a Boundary Value Problem where the architecture extends a cantilever into unknown territory and locks onto the nearest stable resonance in its stored memory.

### The Three Pillars

| Pillar | One-Liner | Mathematical Basis |
|:---|:---|:---|
| **Collision IS Compression** | Overlapping chords form interference patterns, not conflicts. | Superposition in GF(257) preserves all constituent paths. |
| **Rotation IS Data** | Sequence order is encoded as coordinate transforms, not positional tags. | $f(x) = (ax + b) \bmod{257}$ — an affine group with ~65K distinct states. |
| **Resonance IS Retrieval** | Queries don't search; they vibrate at a frequency and follow constructive interference. | `popcount(A & B)` = dot product of binary vectors = Hamming similarity. |

---

## Architecture

The system is split into two layers that operate on the same underlying 512-bit chord substrate.

```
┌─────────────────────────────────────────────────────────┐
│                     CORTEX  (Z ≥ 1)                     │
│   Volatile working memory · Active inference graph      │
│   BVP cantilever · Frustration engine · Goal synthesis  │
├─────────────────────────────────────────────────────────┤
│                     BEDROCK  (Z = 0)                    │
│   Persistent holographic store · 257³ Fermat Cube       │
│   LSM spatial index · Morton-keyed retrieval            │
│   GF(257) rotational addressing · GPU resonance search  │
└─────────────────────────────────────────────────────────┘
```

### Bedrock — Persistent Memory (Z = 0)

The Bedrock is a **Thermodynamic Trie**: tokens are physically routed by identity and shifted by entropy-driven topological rotations into multi-level sorted arrays (LSM-tree).

- **Self-Addressing.** Each byte value (0–255) has a deterministic geometric address on a face of the 257-face cube. No hashing, no projection, no collision indirection.
- **GF(257) Affine Rotations.** A rotation is a coordinate transform $f(x) = (ax + b) \bmod{257}$ where $a$ is derived from the primitive root 3. Sequence position is encoded via $O(1)$ integer arithmetic. Multiple topological events compose into a single $(a, b)$ pair.
- **Parallel Resonance Search.** The GPU evaluates all candidate spans simultaneously via `popcount(context XOR candidate)` across contiguous chord arrays. The best match is found by parallel reduction — no tree traversal, no index lookups.

### Cortex — Working Memory (Z ≥ 1)

The Cortex is a volatile, task-specific inference engine that treats logic as a property of bitwise interference.

- **Boundary Value Problem (BVP) Solver.** Generation is framed as a cantilever extending from a known start state toward a goal phase. The solver bridges gaps by interpolating rotational trajectories.
- **Frustration Engine.** When the wavefront stalls (no chord in the spatial index aligns with the current phase), the frustration counter rises. At a threshold, the engine triggers backtracking or rerouting — mathematically: the target phase multiplied by the modular inverse of the current phase mod 257.
- **Macro Index.** Skip-chords ($2^k$-stride pointers) stored alongside byte-level chords enable logarithmic jumps through the spatial index, turning linear traversal into a fractal skip list.

### The 512-Bit Chord

Every datum in the system is a `Chord`: 8 × 64-bit words packed into a Cap'n Proto struct.

```
 Bits 0–256    │ The GF(257) Fermat Field — semantic/structural data
 Bit  256      │ Delimiter flag
 Bits 257–319  │ Guard Band: Opcode register (8-bit), control flags
 Bits 320–383  │ Residual phase carry (cross-wavefront state)
 Bits 384–511  │ Reserved
```

Key operations and their algebraic meaning:

| Operation | Code | Meaning |
|:---|:---|:---|
| `chord.OR(other)` | Bitwise OR | Superposition — LCM in prime space |
| `chord.AND(other)` | Bitwise AND | Intersection — GCD in prime space |
| `chord.XOR(other)` | Bitwise XOR | Cancellation — residue after interference |
| `ChordHole(target, existing)` | `target & ~existing` | Gap detection — bits needed but absent |
| `ActiveCount()` | `popcount(all words)` | Energy / density of the chord |
| `RollLeft(n)` | Circular shift mod 257 | Positional encoding via rotation |
| `Rotate3D()` | $x \to (3(3(x+1) \bmod 257)+1) \bmod 257$ | Full affine orbit — SO(3) analogue |
| `RotationSeed()` | Affine hash → `(a, b)` | Structural fingerprint for routing |

---

## Codebase Map

The theoretical layers map directly to the repository structure:

### Layer 1 — Data Substrate

> Chord primitives, Morton indexing, and the GF(257) field.

| Concept | File | What It Does |
|:---|:---|:---|
| 512-bit Chord | [`pkg/store/data/chord.go`](pkg/store/data/chord.go) | `BaseChord`, `RollLeft`, `Rotate3D`, `OR`, `AND`, `XOR`, `ChordHole`, `ActiveCount` |
| Chord (Cap'n Proto schema) | [`pkg/store/data/chord.capnp`](pkg/store/data/chord.capnp) | Wire format: 8 × `uint64` words |
| Morton Keys | [`pkg/store/data/morton.go`](pkg/store/data/morton.go) | 3D interleaving: `[Z:sequence \| Y:symbol \| X:position]` |
| Opcodes | [`pkg/store/data/opcode.go`](pkg/store/data/opcode.go) | Guard-band instruction encoding |
| GF(257) Numerics | [`pkg/numeric/core.go`](pkg/numeric/core.go), [`prime.go`](pkg/numeric/prime.go) | Modular arithmetic, Fermat constants |
| GF Rotation | [`pkg/numeric/geometry/gf_rotation.go`](pkg/numeric/geometry/gf_rotation.go) | Affine transform $(a, b)$ in the field |
| Phase Geometry | [`pkg/numeric/geometry/phase.go`](pkg/numeric/geometry/phase.go) | Phase distance, phase wrapping |
| Eigenmode | [`pkg/numeric/geometry/eigenmode.go`](pkg/numeric/geometry/eigenmode.go) | Co-occurrence eigenvectors for ambiguity handling |

### Layer 2 — Spatial Index & Wavefront

> Morton-keyed LSM storage and the wavefront search engine.

| Concept | File | What It Does |
|:---|:---|:---|
| Spatial Index | [`pkg/store/lsm/spatial_index.go`](pkg/store/lsm/spatial_index.go) | Insert, Lookup, Decode — the persistent holographic store |
| Wavefront Search | [`pkg/store/lsm/wavefront.go`](pkg/store/lsm/wavefront.go) | Multi-headed propagation, phase-locked traversal, amplitude decay |
| Wavefront Carry | [`pkg/store/lsm/wavefront_carry.go`](pkg/store/lsm/wavefront_carry.go) | Cross-line residual phase persistence |
| Phase Anchors | [`pkg/store/lsm/phase_util.go`](pkg/store/lsm/phase_util.go) | Drift correction at synchronization checkpoints |
| Skip-Chords | [`pkg/store/lsm/skip.go`](pkg/store/lsm/skip.go) | Power-of-2 stride pointers for logarithmic jumps |

### Layer 3 — Sensory Processing

> Byte-stream segmentation and tokenization.

| Concept | File | What It Does |
|:---|:---|:---|
| Tokenizer | [`pkg/system/process/tokenizer/server.go`](pkg/system/process/tokenizer/server.go) | Bytes → graph edges (Cap'n Proto RPC server) |
| Sequencer (MDL) | [`pkg/system/process/sequencer/mdl.go`](pkg/system/process/sequencer/mdl.go) | Online Minimum Description Length boundary detection |
| Sequencer (Sequitur) | [`pkg/system/process/sequencer/sequitur.go`](pkg/system/process/sequencer/sequitur.go) | Grammar-based hierarchical chunking |
| Fast Window | [`pkg/system/process/fastwindow.go`](pkg/system/process/fastwindow.go) | Adaptive sliding window with variance heuristics |
| Distribution | [`pkg/system/process/distribution.go`](pkg/system/process/distribution.go) | Stream statistics for boundary decisions |
| Calibrator | [`pkg/system/process/calibrator.go`](pkg/system/process/calibrator.go) | Phase/variance calibration across chunks |

### Layer 4 — Logic & Reasoning

> Grammar parsing, semantic algebra, and the graph substrate.

| Concept | File | What It Does |
|:---|:---|:---|
| Grammar Parser | [`pkg/logic/grammar/parser.go`](pkg/logic/grammar/parser.go) | S-V-O extraction, phase computation for prompts |
| Semantic Engine | [`pkg/logic/semantic/server.go`](pkg/logic/semantic/server.go) | Fact injection, modular-inverse query (algebraic cancellation) |
| Graph Substrate | [`pkg/logic/substrate/graph.go`](pkg/logic/substrate/graph.go) | Recursive fold over chord paths — the cortex workbench |
| AST | [`pkg/logic/substrate/ast.go`](pkg/logic/substrate/ast.go) | Abstract syntax tree for structural decomposition |

### Layer 5 — Synthesis & Goal-Directed Reasoning

> BVP solving, frustration-driven backtracking, macro indexing.

| Concept | File | What It Does |
|:---|:---|:---|
| BVP Cantilever | [`pkg/logic/synthesis/bvp/cantilever.go`](pkg/logic/synthesis/bvp/cantilever.go) | Span extension toward a goal phase via rotational interpolation |
| Frustration Engine | [`pkg/logic/synthesis/goal/frustration.go`](pkg/logic/synthesis/goal/frustration.go) | Energy accumulation → backtrack trigger. The "snap" = algebraic wraparound at `(target × modInverse(current)) mod 257` |
| Macro Index | [`pkg/logic/synthesis/macro/macro_index.go`](pkg/logic/synthesis/macro/macro_index.go) | Skip-chord registry for multi-scale navigation |

### Layer 6 — Compute Backend

> GPU/Metal/CPU dispatch for parallel resonance resolution.

| Concept | File | What It Does |
|:---|:---|:---|
| Dispatch | [`pkg/compute/kernel/dispatch.go`](pkg/compute/kernel/dispatch.go) | Backend selection (Metal, CUDA, CPU fallback) |
| CUDA Kernel | [`pkg/compute/kernel/cuda/resolver.cu`](pkg/compute/kernel/cuda/resolver.cu) | `resolve_resonance_kernel`: GF(257) distance via `atomicMax` reduction |
| Metal Kernel | [`pkg/compute/kernel/metal/`](pkg/compute/kernel/metal/) | Apple Silicon equivalent of the CUDA resolver |
| Distributed | [`pkg/compute/kernel/distributed.go`](pkg/compute/kernel/distributed.go) | Multi-node coordination over WebSocket |

### Layer 7 — Machine & Runtime

> The top-level orchestrator that wires everything together.

| Concept | File | What It Does |
|:---|:---|:---|
| Machine | [`pkg/system/vm/machine.go`](pkg/system/vm/machine.go) | `Prompt()`: the 6-stage pipeline (mask → tokenize → lookup → enrich → fold → decode) |
| Booter | [`pkg/system/vm/booter.go`](pkg/system/vm/booter.go) | RPC server lifecycle (Cap'n Proto pipe connections) |
| Prompter | [`pkg/system/vm/input/`](pkg/system/vm/input/) | Holdout masking for evaluation prompts |

### Experiments

> Empirical validation using real datasets via [GoConvey](https://github.com/smartystreets/goconvey) BDD tests.

| Experiment | File | What It Tests |
|:---|:---|:---|
| Text Classification | [`experiment/task/classification/text.go`](experiment/task/classification/text.go) | Language identification from byte chords |
| Blind Classification | [`experiment/task/classification/blind.go`](experiment/task/classification/blind.go) | Classification without labeled training data |
| Out-of-Corpus Generation | [`experiment/task/textgen/outofcorpus.go`](experiment/task/textgen/outofcorpus.go) | Generating text not present in the training set |
| Prose Chaining | [`experiment/task/textgen/prose_chaining.go`](experiment/task/textgen/prose_chaining.go) | Multi-sentence coherence via phase carry |
| bAbI Benchmark | [`experiment/task/logic/babi_benchmark.go`](experiment/task/logic/babi_benchmark.go) | Multi-hop reasoning (Where is X?) via algebraic cancellation |
| Semantic Algebra | [`experiment/task/logic/semantic_algebra.go`](experiment/task/logic/semantic_algebra.go) | S-V-O injection and modular-inverse query |
| Pipeline Harness | [`experiment/task/pipeline.go`](experiment/task/pipeline.go) | Standard test harness — all experiments use the full `vm.Machine` |

---

## The Prompt Pipeline

When you call `machine.Prompt("Where is Roy?")`, the following deterministic pipeline executes:

```
  ┌─────────────┐
  │  1. Prompter │  Holdout masking (evaluation mode)
  └──────┬──────┘
         ▼
  ┌─────────────┐
  │ 2. Tokenizer │  Bytes → GF(257) chords → graph edges
  └──────┬──────┘
         ▼
  ┌──────────────────┐
  │ 3. Spatial Lookup │  Morton-keyed nearest-neighbor in Hamming space
  └──────┬───────────┘
         ▼
  ┌───────────────────────────────┐
  │ 4. Enrichment (best-effort)   │
  │    Grammar  → S-V-O parse     │
  │    Semantic → modular query    │
  │    BVP      → cantilever span  │
  └──────┬────────────────────────┘
         ▼
  ┌─────────────────┐
  │ 5. Graph Fold    │  Recursive chord interference across paths
  └──────┬──────────┘
         ▼
  ┌─────────────────┐
  │ 6. Decode        │  Chords → bytes (reverse BaseChord lookup)
  └──────┬──────────┘
         ▼
       Result
```

**The algebraic cancellation in action:**

Given stored facts: `Roy ⊕ is_in ⊕ Kitchen`, `Sandra ⊕ is_in ⊕ Garden`

Prompt: "Where is Roy?" → Phase = `Roy × is_in`

The GPU computes: `StoredPhase × modInverse(Roy) × modInverse(is_in) mod 257`

Shared structure (`is_in`) cancels. `Roy` cancels. The residue resonates with `Kitchen`.

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
│   ├── errnie/                   # Error handling utilities
│   ├── logic/
│   │   ├── grammar/              # S-V-O grammar parser
│   │   ├── semantic/             # Fact store + modular-inverse queries
│   │   ├── substrate/            # Graph VM (cortex workbench)
│   │   └── synthesis/            # BVP, frustration, macro index
│   ├── numeric/                  # GF(257) math, geometry, phase
│   ├── store/
│   │   ├── data/                 # Chord, Morton, Value primitives
│   │   └── lsm/                  # Spatial index, wavefront, skip-chords
│   ├── system/
│   │   ├── core/                 # Configuration
│   │   ├── process/              # Tokenizer, sequencer, calibrator
│   │   ├── pool/                 # Worker pool + broadcast groups
│   │   └── vm/                   # Machine orchestrator + booter
│   ├── telemetry/                # Observability
│   └── validate/                 # Invariant checks
├── experiment/                   # Experiment harness + task definitions
│   └── task/                     # classification, textgen, logic, scaling
├── test/integration/             # End-to-end integration tests
├── paper/                        # LaTeX research paper (auto-generated)
├── docs/                         # Design documents + diagrams
└── Makefile                      # Build, test, and paper generation
```

---

## Experiments

All experiments use the full `vm.Machine` pipeline with real data (HuggingFace datasets). No oracles, no faked results.

Experiments are structured as GoConvey BDD tests in `experiment/task/pipeline_test.go`. Each task:

1. Boots the full machine (tokenizer, spatial index, semantic engine, graph, BVP)
2. Ingests a real dataset
3. Runs prompts through the 6-stage pipeline
4. Asserts observable outputs via `gc.So`
5. Generates LaTeX artifacts for the research paper

Current experiment categories:

| Category | Tasks | Status |
|:---|:---|:---|
| **Classification** | Language identification, blind classification | ✅ Active |
| **Text Generation** | Out-of-corpus, compositional, prose chaining, text overlap | ✅ Active |
| **Logic & Reasoning** | bAbI benchmark, semantic algebra | ✅ Active |
| **Scaling** | Throughput and latency under increasing corpus size | 🔬 In progress |

---

## Roadmap

### ✅ Implemented

- [x] GF(257) affine rotation group with primitive root 3
- [x] 512-bit chord substrate (Cap'n Proto, zero-copy RPC)
- [x] Morton-keyed LSM spatial index with 3D interleaving
- [x] Wavefront search with multi-headed propagation and phase carry
- [x] MDL + Sequitur dual-track sequence boundary detection
- [x] S-V-O grammar parser and semantic fact engine
- [x] BVP cantilever solver with rotational interpolation
- [x] Frustration engine with energy-threshold backtracking
- [x] Macro index for multi-scale skip-chord navigation
- [x] CUDA + Metal + CPU compute backends
- [x] Full experiment harness with LaTeX paper generation
- [x] Phase anchors for drift correction
- [x] Guard band opcode register and residual carry

### 🔧 In Development

- [ ] **Multi-headed frustration.** Parallel wavefront heads that independently explore alternative rotational trajectories when the primary path stalls.
- [ ] **Logic garbage collection.** Automatic pruning of expired cantilever spans and dead wavefront heads to bound working memory.
- [ ] **Rotation opcodes.** Extending the guard-band instruction set from basic control flow to a full rotational VM bytecode.
- [ ] **Distributed phase sync.** Peer-to-peer index merging across nodes with latency-aware timeout and phase reconciliation.
- [ ] **Spatial paging strategy.** Prefetching Morton clusters into GPU shared memory to respect PCIe bandwidth constraints.
- [ ] **AVX-512 register tiling.** Explicit SIMD layout for the 257-bit field to avoid L1 cache spills during multi-path frustration.

### 🔭 Research Horizon

- [ ] **Ontological transposition.** Using rotational group isomorphisms to translate between domain-specific chord encodings.
- [ ] **Synthetic phase grammars.** Deriving grammar rules directly from phase-transition statistics rather than hand-coded S-V-O patterns.
- [ ] **Cross-modal alignment.** Extending the guard band to carry modality tags (text, audio, image) for unified chord representation.

---

## Hardware Considerations

> [!IMPORTANT]
> The zero-copy memory architecture currently assumes unified GPU memory provides near-zero latency traversal. This is an idealization. Random access across deeply scattered Morton branches will incur **page-fault stalls** and **PCIe bus saturation** on discrete GPUs. The spatial paging strategy (see Roadmap) is required before multi-gigabyte indices are practical.

The 257-bit field fits inside a 512-bit hardware word. Since $257 = 2^8 + 1$, modular reduction simplifies to:

```
result = (val & 0xFF) - (val >> 8)    // branchless on GPU
```

This makes GF(257) arithmetic run at the speed of native bitwise operations on any architecture supporting 256-bit or wider SIMD registers.

---

## Citation

This project is documented in a companion research paper generated automatically from experiment results. See [`paper/`](paper/) for the LaTeX source.

---

## License

See [LICENSE](LICENSE) for details.
