<p align="center">
  <img src="docs/infographic.jpg" width="680" alt="Six Architecture Infographic" />
</p>

<h1 align="center">six</h1>

<p align="center">
  <strong>A prime-indexed bit-field machine that replaces learned parameters with modular arithmetic.</strong><br/>
  8191-bit Mersenne field · Derived PGA motors · Divisibility-lattice reasoning
</p>

<p align="center">
  <a href="#quick-start">Quick Start</a> ·
  <a href="#core-thesis">Core Thesis</a> ·
  <a href="#the-value-type">The Value Type</a> ·
  <a href="#architecture">Architecture</a> ·
  <a href="#codebase-map">Codebase Map</a> ·
  <a href="#roadmap">Roadmap</a>
</p>

---

> [!NOTE]
> This is a research project under active development.
> The canonical architecture document is [`NEXTEST.md`](NEXTEST.md).
> This README is a condensed view grounded in the current codebase.

---

## Core Thesis

> **Can we reject gradient descent and backpropagation long enough to convince ourselves that we may not need them?**

The answer is a machine whose native medium is `primitive.Value` — an 8191-bit prime-indexed field in GF(8191). Each bit position represents the presence of one prime number. The bit pattern is simultaneously a square-free integer (product of active primes), a point on the divisibility lattice of the integers, and an affine motor `f(p) = scale·p + translate (mod 8191)` derived from the field. Every bitwise operation is an operation on prime factorizations, connected to all of number theory by the Fundamental Theorem of Arithmetic.

The system does not predict the next token. It resolves **spans** — a Boundary Value Problem where the architecture navigates a prime lattice from a start boundary toward structurally compatible continuations. The motor derived from the accumulated state navigates to a **region** of the lattice, not a single point. The result is a set of branches ranked by structural compatibility, not a collapsed prediction.

### Why GF(8191)?

$8191 = 2^{13} - 1$ is a Mersenne prime. Mersenne primes have a unique hardware property: modular reduction collapses to a bit shift and an add — `(a & 0x1FFF) + (a >> 13)` — bypassing the CPU division unit entirely. This makes field arithmetic as cheap as ordinary bitwise operations while retaining the algebraic guarantees of a prime field: every non-zero element has a multiplicative inverse (Fermat's Little Theorem), there are no dead zones, no rounding errors, and no special cases.

### The Three Pillars

| Pillar | One-Liner | Mathematical Basis |
|:---|:---|:---|
| **AND IS GCD** | Shared structure extraction is a single bitwise AND. The result is the prime factorization of the Greatest Common Divisor. | `A AND B = {shared primes}` → `GCD(A, B)` |
| **OR IS LCM** | Superposition of two Values produces the Least Common Multiple. The minimal composite containing both inputs. | `A OR B = {union of primes}` → `LCM(A, B)` |
| **Motor IS Navigation** | Every Value derives an affine transform in GF(8191). The data tells you where to go next. No index, no search. | `f(p) = scale·p + translate (mod 8191)` |

---

## The Value Type

The `primitive.Value` is a named array of 128 `uint64` words (1024 bytes). It implements `io.ReadWriteCloser`. It is a value type: copy by assignment, slice with `v[:]`, pass sub-ranges to operations. No struct, no pointer, no hidden fields — the type IS the memory layout.

```
 Bits 0–8190     │ GF(8191) Mersenne core — prime-indexed bit field
                 │ Bit k = presence of k-th prime number
                 │ (128 × uint64 words, last word masked to 8191 bits)
 Bit 8191        │ Control frame flag — in-band signaling
                 │ (sits outside the prime field, safe for pipeline control)
```

### What A Value Is

A Value is a bit field of 8191 positions. Position 0 represents prime 2, position 1 represents prime 3, position 2 represents prime 5, and so on. A Value with bits `{0, 2, 4}` represents the number `2 × 5 × 11 = 110`. By the Fundamental Theorem of Arithmetic, this representation is unique — no two different sets of primes produce the same product.

A "base" Value (one byte projected into the field via `BaseValue(b)`) activates the bit positions corresponding to that byte's prime factors. Bytes 0 and 1 have no prime factors and produce zero Values.

### Why Primes, Not Plain Bit Positions

Without primes, bit positions are arbitrary labels. AND means "shared labels." OR means "union of labels." There is no algebraic depth connecting the positions.

With primes, every bitwise operation becomes an operation on the divisibility lattice of the integers:

| Operation | Set Meaning | Number-Theoretic Meaning |
|:---|:---|:---|
| `AND` | Shared bit positions | GCD — largest common divisor |
| `OR` | Union of bit positions | LCM — smallest common multiple |
| `A & ~B` (Material Nonimplication) | Bits in A not in B | Unique factor residue: `A / GCD(A, B)` |
| `XOR` | Bits in exactly one | Symmetric difference: `LCM / GCD` |
| `(A AND B) == A` | Subset check | Divisibility: does A divide B? |
| `A AND B == 0` | Disjoint sets | Coprimality: GCD = 1, completely independent |

These are not analogies. They are direct consequences of the Fundamental Theorem of Arithmetic: every integer has a unique prime factorization, so every operation on prime sets is an operation on integers.

### The Shell: A Value As A Transformation

A Value is not only a number. It is simultaneously a transformation — an affine motor `f(p) = scale·p + translate (mod 8191)` derived deterministically from its bit pattern. Scale is the product of active prime indices mod 8191. Translate is their sum mod 8191. The motor is derived, not stored. The bit pattern IS the motor. There is no separate storage, no external override.

Motors compose in O(1): `f₂(f₁(p)) = (a₂·a₁)·p + (a₂·b₁ + b₂)`. Motors invert in O(1): `f⁻¹(p) = a⁻¹·(p - b)`. The affine group has `8190 × 8191 ≈ 67M` distinct transforms.

When motors compose during sequence accumulation, commutativity breaks. The motor transforms each incoming Value before bitwise combining, so "cat" after "the" produces a different state than "cat" after "dog" — the motor that acted on it was different. This gives sequence sensitivity without positional encodings.

### Operations On Values

There are exactly 16 binary boolean operations on two bit fields plus motor operations:

| # | Name | Formula | Invertible |
|---|---|---|---|
| 0 | Contradiction | `0` | No |
| 1 | NOR | `~(A \| B)` | No |
| 2 | Converse Nonimplication | `~A & B` | No |
| 3 | NOT | `~A` | Yes |
| 4 | Material Nonimplication | `A & ~B` | No |
| 5 | NOT (second operand) | `~B` | Yes |
| 6 | XOR | `A ^ B` | Yes |
| 7 | NAND | `~(A & B)` | No |
| 8 | AND | `A & B` | No |
| 9 | XNOR | `~(A ^ B)` | Yes |
| 10 | Identity B | `B` | — |
| 11 | Material Conditional | `~A \| B` | No |
| 12 | Identity A | `A` | — |
| 13 | Converse Implication | `A \| ~B` | No |
| 14 | OR | `A \| B` | No |
| 15 | Tautology | `1` | No |
| 16 | Motor Apply | `motor(A) applied to B` | Yes |
| 17 | Motor Invert | `motor⁻¹(A) applied to B` | Yes |
| 18 | Motor Compose | `motor(A) ∘ motor(B) applied to B` | Yes |

All 19 operations are implemented in the codebase. In the ISA, each instruction IS a Value with a single prime-position bit set — the truth table index IS the structural index. Operations 0–15 map directly to the universal 16-row truth table. Operations 16–18 are motor transforms outside the truth table.

Each `Op` implementation contains a hyper-optimized fast-path for the 128-word layout: fixed-size array projection plus explicit 8-way unrolling for branchless SIMD (AVX/NEON) codegen.

---

## Architecture

The system is organized as `io.ReadWriteCloser` pipelines where every component reads, transforms, and writes Values. The universal interface is Go's standard `io` package: `io.Copy`, `io.Pipe`, `io.MultiWriter`, `io.TeeReader`.

```
┌─────────────────────────────────────────────────────────────┐
│              NETWORK PLANE  (pkg/network/)                  │
│   UniConn: IPC · UDP multicast · QUIC                       │
│   One Value = one UDP datagram = one Ethernet frame         │
├─────────────────────────────────────────────────────────────┤
│              TRANSPORT PLANE  (pkg/transport/)              │
│   Stream: ring-buffer pipe with control frame interception  │
│   Pipeline: sequential io.ReadWriter chain                  │
│   Resonator: XOR-residual feedback loop with motor reify    │
│   FlipFlop · Pump · Feedback · Graph · Sink                 │
├─────────────────────────────────────────────────────────────┤
│              COMPUTE PLANE  (pkg/compute/)                  │
│   CPU / Metal / CUDA backends — same op set                 │
│   Typed Go slices for SSA bounds-check elimination          │
├─────────────────────────────────────────────────────────────┤
│              VALUE PLANE  (pkg/primitive/)                  │
│   The native programmable monotype · GF(8191) core          │
│   Derived PGA motor (scale, translate) from bit pattern     │
│   16 truth-table ops + 3 motor ops · RollLeft               │
│   Möbius inversion · Prime sieve · BaseValue projection     │
│   ISA: instructions ARE prime-lattice points                │
└─────────────────────────────────────────────────────────────┘
```

### Value Plane — `pkg/primitive/`

The foundation. Everything else operates on Values.

- **`Value`** — 8191-bit prime-indexed field as `[128]uint64`. Implements `io.ReadWriteCloser`. On little-endian architectures, `Read`/`Write` is a single `copy`/`memmove`.
- **`Motor()`** — derives `(scale, translate)` from the bit pattern via a hybrid strategy: sparse words (<4 bits) use bit-scanning with Mersenne `mod8191`; dense words (≥4 bits) use a precomputed `motorTable` with dual ILP accumulators. Wins or ties at every measured density from 10 to 4000 active bits.
- **`ApplyMotor`, `ComposeMotor`, `InvertMotor`** — GF(8191) affine algebra. Compose is closed and associative. Invert uses the extended Euclidean algorithm.
- **`Primes[8191]`** — first 8191 primes, generated once at init via Sieve of Eratosthenes. `PrimeIndex` maps back from prime to bit position.
- **`BaseValue(b)`** — projects a byte into the field by activating its prime factor positions. Precomputed at init for all 256 byte values.
- **`operation.Op`** — `func(a, b, dst []uint64)`. All 16 truth-table operations plus motor ops, each with a SIMD-optimized 128-word fast path.
- **`operation.Bitwise`** — `io.ReadWriteCloser` accumulator. Expects 3 frames (instruction + 2 operands) on its ring buffer, applies the selected `Op`, returns the result.
- **`operation.RollLeft`** — circular shift exploiting the Mersenne-prime property: 8191 = 128×64 − 1, so the shift reduces to an exact overlap of two flat sequences. Fully branchless.
- **`algebra.Mobius`** — Möbius function for square-free numbers on the prime lattice. `μ(n) = (−1)^k` where k is the popcount parity.

### Transport Plane — `pkg/transport/`

Everything composes via `io.ReadWriteCloser`.

- **`Stream`** — async pipe backed by a ring buffer. Writes complete immediately when the buffer has space; reads block only when empty. In-band control frame interception: if a 1024-byte chunk is flagged as a control frame (bit 8191 set), the Stream consumes it to rewire its own operational pipeline — never emitting it to the caller.
- **`Pipeline`** — chains `io.ReadWriter` components. Data flows through all components in sequence via `io.Copy`. Nestable: a Pipeline is itself an `io.ReadWriter`.
- **`Resonator`** — the feedback loop. Anchored to a prompt Value, it computes `XOR(prompt, output)` as the lattice distance after each cycle. PopCount zero means convergence — the composition fully satisfies the prompt. A repeated residual (same Value in the `visited` map) means the motor entered an orbit — the trajectory is exhausted and the caller should branch. `Reify()` collapses the accumulated motor trace into a single tool Value that can be written back to the corpus.
- **`FlipFlop`** — synchronous round-trip: `io.Copy(to, from)` then `io.Copy(from, to)`. No goroutines.
- **`Pump`** — feedback loop around a Pipeline and a Stream. Goroutine runs FlipFlop repeatedly until `Close`.
- **`Feedback`** — tee pattern: reads from `forward`, copies to `backward` via `io.TeeReader`.
- **`Graph`** — directed graph of `io.ReadWriteCloser` nodes and edges. Registry holds processed data.
- **`Sink`** — null device. Writes are dropped, reads return EOF.

### Compute Plane — `pkg/compute/`

Three backends, one interface. All implement the same operation set: `BitwiseOr`, `BitwiseAnd`, `BitwiseXor`, `BitwiseAndNot`, `BitwiseNand`, `BitwiseNor`, `BitwiseXnor`, `ConverseNonimplication`, `BitwiseNot`, `MotorApply`, `MotorInvert`, `MotorCompose`, `RollLeft`.

- **`cpu.Backend`** — always available. Uses typed Go slices for SSA bounds-check elimination.
- **`metal.Backend`** — Apple Silicon (build tag `darwin && cgo`). Embeds compiled `backend.metallib`. Metal 3.1 shaders for all operations.
- **`cuda.Backend`** — NVIDIA GPUs (build tag `cuda && cgo`). Same operation set. Stub returns `CUDAErrorUnavailable` on unsupported platforms.
- **`Backend`** — top-level wrapper. Embeds a `transport.Stream` and holds all three kernel backends.

### Network Plane — `pkg/network/`

A Value is 1024 bytes. A standard Ethernet MTU is 1500 bytes. A UDP datagram over IPv4 has 28 bytes of header. One Value = one datagram = one Ethernet frame. No fragmentation, no reassembly, no buffering.

- **`UniConn`** — unified connection. `io.ReadWriteCloser` that delegates to a concrete transport selected at construction via options: `UniConnWithIPC`, `UniConnWithUDP`, `UniConnWithQUIC`.
- **`IPC`** — Unix domain socket transport. Listen or dial by path. Same-machine, cross-process.
- **`UDPMulticast`** — LAN multicast. One datagram = one Value. One machine sends, every machine on the network receives. Each machine ANDs the operation with its own summary Value to self-select.
- **`QUIC`** — QUIC transport with bidirectional stream for WAN communication. Reliable, multiplexed, UDP-based.

---

## Mathematical Foundations

### Affine Rotations

The system's core operation is an affine transform over the field:

$$f(x) = (a \cdot x + b) \bmod{8191}, \quad a \in [1, 8190],\; b \in [0, 8190]$$

- **Composition** is $O(1)$: $a' = a_2 \cdot a_1 \bmod{8191}$, $b' = (a_2 \cdot b_1 + b_2) \bmod{8191}$
- **Inversion** is $O(1)$: $f^{-1}(y) = a^{-1}(y - b) \bmod{8191}$

### Bidirectional Resolution

The motor is invertible. Given a fragment, forward navigation uses the motor to find continuations; backward navigation uses the inverse motor to find prefixes. Both directions are multi-branch. Both are O(1) on the GPU. This is the BVP framing: a fragment has two boundaries, and the system resolves spans from both edges.

### Composition Via Residual Tracking

When no single stored sequence satisfies the prompt, Material Nonimplication tracks the unresolved request: `prompt & ~output_so_far` gives the exact set of primes the output has not yet accounted for. As output accumulates, the residual shrinks in resolved primes and retains unsatisfied ones. When the current continuation is exhausted, the residual's motor navigates to the next matching region. There is no explicit switch signal — the unsatisfied structure takes over navigation automatically.

### Möbius Inversion

For square-free numbers (all Values), `μ(n) = (−1)^k` where k is the popcount. The Möbius inversion formula recovers individual contributions from composites. OR does not destroy information on the prime lattice — the divisors of a composite are all Values whose primes are subsets, and inclusion-exclusion is algebraically exact.

---

## Codebase Map

### `pkg/primitive/` — The Native Value Type

| File | What It Does |
|:---|:---|
| [`value.go`](pkg/primitive/value.go) | 8191-bit `[128]uint64` field, `io.ReadWriteCloser`, ISA instruction constants, `Set`, `Has`, `PopCount`, `IsZero`, `Equal`, `Clamp`, `IsInstruction`, `SetInstruction` |
| [`motor.go`](pkg/primitive/motor.go) | `Motor()` derivation (hybrid sparse/dense), `ApplyMotor`, `ComposeMotor`, `InvertMotor`, `mod8191`, precomputed `motorTable` |
| [`prime.go`](pkg/primitive/prime.go) | `Primes[8191]`, `PrimeIndex`, `BaseValue(b)`, `BaseValueInto(dst, b)`, sieve init |
| [`operation/bitwise.go`](pkg/primitive/operation/bitwise.go) | All 16 truth-table `Op`s + `RollLeft` + `Bitwise` accumulator (`io.ReadWriteCloser`) |
| [`operation/motor.go`](pkg/primitive/operation/motor.go) | `MotorApply`, `MotorInvert`, `MotorCompose` as `Op` functions |
| [`algebra/mobius.go`](pkg/primitive/algebra/mobius.go) | Möbius function for the square-free lattice |

### `pkg/transport/` — Pipeline Composition

| File | What It Does |
|:---|:---|
| [`stream.go`](pkg/transport/stream.go) | Ring-buffer pipe with in-band control frame interception |
| [`pipeline.go`](pkg/transport/pipeline.go) | Sequential `io.ReadWriter` chain via `io.Copy` |
| [`resonator.go`](pkg/transport/resonator.go) | XOR-residual feedback loop, convergence/orbit detection, motor trace `Reify` |
| [`flipflop.go`](pkg/transport/flipflop.go) | Synchronous round-trip between two `io.ReadWriter`s |
| [`pump.go`](pkg/transport/pump.go) | Feedback loop goroutine driving FlipFlop on a Pipeline + Stream |
| [`feedback.go`](pkg/transport/feedback.go) | Tee pattern via `io.TeeReader` |
| [`graph.go`](pkg/transport/graph.go) | Directed graph of `io.ReadWriteCloser` nodes |
| [`sink.go`](pkg/transport/sink.go) | Null device |

### `pkg/compute/` — GPU/CPU Backends

| File | What It Does |
|:---|:---|
| [`backend.go`](pkg/compute/backend.go) | Top-level wrapper embedding Stream + all three kernel backends |
| [`kernel/cpu/backend.go`](pkg/compute/kernel/cpu/backend.go) | CPU fallback: typed Go slices, SSA-safe |
| [`kernel/metal/backend.go`](pkg/compute/kernel/metal/backend.go) | Apple Silicon Metal 3.1 shaders (build: `darwin && cgo`) |
| [`kernel/cuda/backend.go`](pkg/compute/kernel/cuda/backend.go) | CUDA kernels (build: `cuda && cgo`) |

### `pkg/network/` — Transport Hierarchy

| File | What It Does |
|:---|:---|
| [`conn.go`](pkg/network/conn.go) | `UniConn` — unified `io.ReadWriteCloser` over IPC/UDP/QUIC |
| [`ipc.go`](pkg/network/ipc.go) | Unix domain socket transport |
| [`udp.go`](pkg/network/udp.go) | UDP multicast: one datagram = one Value |
| [`quic.go`](pkg/network/quic.go) | QUIC bidirectional stream for WAN |

### `pkg/core/` — Configuration & Validation

| File | What It Does |
|:---|:---|
| [`config.go`](pkg/core/config.go) | Viper-backed configuration (`Cfg` global, `Get[T]`) |
| [`validate/validate.go`](pkg/core/validate/validate.go) | `Require` — constructor-time nil checks |

### `pkg/errnie/` — Error Handling

| File | What It Does |
|:---|:---|
| [`logger.go`](pkg/errnie/logger.go) | `Error(err, keyvals...)` — log and return; `InitLogger` |

### `test/integration/` — Empirical Verification

| File | What It Tests |
|:---|:---|
| [`claims_test.go`](test/integration/claims_test.go) | XOR/XNOR invertibility, AND/OR non-invertibility, Hamming distance, motor composition, motor invertibility, Material Nonimplication residues, parallel AND reduction |
| [`classification_test.go`](test/integration/classification_test.go) | Parallel AND reduction across Value sets, pairwise GCD layers, cancellation-depth properties |
| [`primes_test.go`](test/integration/primes_test.go) | Prime sieve correctness, BaseValue projection, GCD/LCM via AND/OR, divisibility, coprimality |
| [`motors_test.go`](test/integration/motors_test.go) | Motor derivation, composition, inversion, orbit detection |
| [`lattice_test.go`](test/integration/lattice_test.go) | Divisibility lattice properties, Material Nonimplication, XOR as LCM/GCD |
| [`fuzz_test.go`](test/integration/fuzz_test.go) | Randomized property testing across operations |

---

## The Feedback Loop

It is not brute-force search. It is brute-force reasoning.

The system uses the 2^8191 state-space to experiment with compositions the way a mind experiments with thoughts: manifest an idea, observe the outcome, re-align. The Resonator is the entire controller. There is no reward function, no loss landscape, no external evaluator. The residual IS the error signal, and it carries its own navigation.

```
1. PROMPT     A prompt Value enters the Resonator.
              The Resonator emits it as the initial navigation signal.

2. COMPOSE    The pipeline processes the signal through the transport graph.
              Each intermediate Value contributes a motor to the chain.

3. MEASURE    XOR(prompt, output) — the lattice distance, as a Value.
              Not a scalar loss. A structured error with specific primes
              and its own motor pointing at the region where the fix lives.

4. NAVIGATE   The residual's motor navigates to the correction region.
              The system doesn't search. It follows the structure of
              its own mistake. The error tells you WHAT's wrong and
              WHERE to look.

5. CONVERGE   PopCount zero → the composition fully satisfies the prompt.
              Repeated residual → orbit detected, trajectory exhausted.
              if !residual.IsZero() { continue }  — that's a while loop,
              not a reward function.

6. REIFY      Resonator.Reify() collapses the motor trace into a single
              (scale, translate) and encodes it as a tool Value.
              Written back to the corpus, future prompts with high GCD
              against the tool navigate to it automatically. Tools
              compose into bigger tools. The loop builds tools. The
              tools are found by the loop.

              ┌──────────────────────────────────────┐
              │  The loop is the only tool.          │
              │  The loop builds tools.              │
              │  The tools are found by the loop.    │
              └──────────────────────────────────────┘
```

Whatever structure the system builds in the lattice — trees, clusters, hierarchies, something with no human name — is whatever the 2^8191 space supports given the data that flows through it. The residuals decide. The primes decide. The motors decide. The system does not need to be told what structure to build. It needs a nonzero residual and a while loop.

---

## Distributed Substrate

The Value is the wire format. 1024 bytes, fixed size, no serialization, no schema. One Value = one UDP datagram = one Ethernet frame. The `UniConn` abstracts over the transport hierarchy:

| Scale | Transport | Latency |
|:---|:---|:---|
| Same process | Go channel / direct call | Nanoseconds |
| Same machine | IPC (Unix domain socket) | Microseconds |
| Same LAN | UDP multicast | Sub-millisecond |
| Wide area | QUIC | Network-dependent |

**Self-routing via AND**: each machine maintains a summary Value (OR of all stored Values). Route an operation by ANDing it against each summary. Non-zero → relevant. Zero → skip. 128 uint64 ANDs per machine — nanoseconds. The number theory IS the routing algorithm.

---

## Quick Start

### Prerequisites

- **Go 1.26+**
- **Metal** (macOS) or **CUDA** toolkit (Linux) for GPU acceleration (optional — CPU fallback always available)

### Build

```bash
# Compile Metal shaders (macOS)
make metal

# Compile CUDA kernels (Linux with NVIDIA GPU)
make cuda

# Both
make build
```

### Run Tests

```bash
# All tests
go test ./...

# Integration tests only
go test ./test/integration/

# With benchmarks
go test -bench=. ./pkg/primitive/...
```

### Project Structure

```
six/
├── cmd/                             # CLI entry points (Cobra)
├── pkg/
│   ├── primitive/                   # The native Value type
│   │   ├── value.go                 # 8191-bit field, io.ReadWriteCloser, ISA
│   │   ├── motor.go                 # Derived affine motor, compose, invert
│   │   ├── prime.go                 # Sieve, BaseValue, PrimeIndex
│   │   ├── operation/               # 16 truth-table Ops + motor Ops + RollLeft
│   │   └── algebra/                 # Möbius inversion
│   ├── transport/                   # io.ReadWriteCloser pipeline composition
│   │   ├── stream.go                # Ring-buffer pipe, control frame interception
│   │   ├── pipeline.go              # Sequential ReadWriter chain
│   │   ├── resonator.go             # XOR-residual feedback, motor reify
│   │   ├── flipflop.go              # Synchronous round-trip
│   │   ├── pump.go                  # Feedback loop goroutine
│   │   ├── feedback.go              # TeeReader pattern
│   │   ├── graph.go                 # Directed node graph
│   │   └── sink.go                  # Null device
│   ├── compute/                     # GPU/CPU backends
│   │   ├── backend.go               # Top-level wrapper
│   │   └── kernel/
│   │       ├── cpu/                 # Always-available CPU backend
│   │       ├── metal/               # Apple Silicon Metal 3.1
│   │       └── cuda/                # NVIDIA CUDA
│   ├── network/                     # Transport hierarchy
│   │   ├── conn.go                  # UniConn (unified io.ReadWriteCloser)
│   │   ├── ipc.go                   # Unix domain sockets
│   │   ├── udp.go                   # UDP multicast
│   │   └── quic.go                  # QUIC for WAN
│   ├── core/                        # Configuration and validation
│   └── errnie/                      # Error handling
├── test/integration/                # Empirical verification
├── docs/                            # Design documents
├── NEXTEST.md                       # Canonical architecture document
└── Makefile                         # Build and test
```

---

## Roadmap

### Implemented

- [x] 8191-bit prime-indexed Value type as `[128]uint64` with `io.ReadWriteCloser`
- [x] All 16 truth-table operations with SIMD-optimized 128-word fast paths
- [x] Motor derivation from bit pattern (hybrid sparse/dense with precomputed table)
- [x] Motor composition, application, and inversion in GF(8191)
- [x] `RollLeft` circular shift exploiting Mersenne-prime property
- [x] ISA where instructions ARE prime-lattice points (truth-table index = structural index)
- [x] In-band control frames for pipeline reconfiguration via instruction flag (bit 8191)
- [x] `Stream` with ring-buffer pipe and control frame interception
- [x] `Resonator` feedback loop: XOR residual, convergence detection, orbit detection, motor `Reify`
- [x] `Pipeline`, `FlipFlop`, `Pump`, `Feedback`, `Graph`, `Sink` transport components
- [x] CPU + Metal + CUDA compute backends with identical operation sets
- [x] `UniConn` unified transport: IPC, UDP multicast, QUIC
- [x] Möbius inversion for the square-free lattice
- [x] Prime sieve (Eratosthenes) for first 8191 primes with `BaseValue` precomputation
- [x] Integration tests: claims verification, classification, primes, motors, lattice, fuzz

### In Development

- [ ] **Corpus storage.** Persistent Value collections enabling real data ingestion, retrieval via GCD, and Reify write-back so the feedback loop can accumulate tools.
- [ ] **Composition orchestration.** Connect the Resonator's residual-tracking loop to a corpus so Material Nonimplication drives multi-fragment composition end-to-end.
- [ ] **Tool accumulation.** Automatically feed Reify products back into the corpus so successful motor traces become reusable tools. Tools compose into bigger tools. Whatever structure emerges is the system's own discovery.

### Research Horizon

- [ ] **Cross-modal ingestion.** Text, image, and sensor data as raw bytes → BaseValue → same lattice. Structural overlap via shared prime factors replaces learned embedding alignment.
- [ ] **Distributed corpus.** Summary-Value routing across UDP multicast (LAN) and QUIC (WAN) for peer-to-peer lattice queries with no central coordinator.
- [ ] **Autonomous curiosity.** The system scans its own corpus for low-resonance gaps and synthesizes new tool Values during idle cycles without human prompting.

---

## License

See [LICENSE](LICENSE) for details.
