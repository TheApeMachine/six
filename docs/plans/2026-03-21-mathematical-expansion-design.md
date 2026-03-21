# Mathematical Expansion: Five-Layer Architecture Extension

> **Implementation order:** Layer 0 → Layer 4 → Layers 1, 2, 3 (concurrent)

**Goal:** Replace statistical approximations with exact discrete algebra, geometric reasoning, topological invariants, categorical structure, and parallel lattice evolution. Each layer builds on the previous; Layer 0 (GF(8191)) is the foundation everything else requires.

---

## Layer 0: Hyperdimensional Computing — GF(8191) Field Expansion

### Problem

The 257-bit core saturates after ~50 OR bundling operations. At 30% density (77 bits), `RecursiveFold` falls back to PhaseDial similarity. The Shannon ceiling at 45% gives ~11 bytes of active context before structural collapse.

### Solution

Replace GF(257) with GF(8191). 8191 = 2^13 − 1 (Mersenne prime).

**Why 8191:**
- Mersenne reduction: `(x & 0x1FFF) + (x >> 13)` executes in a single clock cycle
- 32× more room before saturation: ~368 bytes of active context at 45% Shannon ceiling
- 13-bit field elements fit in uint16, keeping shell word packing tight

### Value Layout

| Region | Current (GF(257)) | New (GF(8191)) |
|---|---|---|
| Core | 257 bits (C0–C4 bit 0) | 8,191 bits (blocks 0–127, block 127 bits 0–62) |
| Shell | 255 bits (C4 bits 1–63 + C5–C7) | 192 bits (blocks 128–130) |
| Total | 512 bits = 64 bytes | 8,384 bits = 1,048 bytes |
| Blocks | 8 × uint64 (fixed fields) | 131 × uint64 (inline Data blob) |

### Systems Engineering Mitigations

**1KB struct — memory bandwidth cliff:**

- All method receivers become pointer receivers (`*Value`). Passing 1KB by value overflows Go stack frames, forces heap escapes, and destroys L1 cache hit rate.
- Transient Values during graph folding use `sync.Pool` arena. Zero allocation on hot paths.
- Cap'n Proto schema uses `@0 :Data` (inline byte array), not `List(UInt64)`. Data is contiguous in the segment — no pointer chasing.
- GPU kernels use warp-level cooperative fetching: 32 threads load one 1KB struct into shared memory before executing math.

**Field arithmetic:**

```
FieldPrime     = 8191
FieldPrimitive = <computed at init: smallest g where g^(8190/p) ≠ 1 (mod 8191) for p ∈ {2,3,5,7,13}>
InverseBase    = 8189  (Fermat's little theorem: a^(p-2) ≡ a^(-1))
CoreBits       = 8191
CoreBlocks     = 128   (ceil(8191/64))
ShellBlocks    = 3
TotalBlocks    = 131
```

Mersenne modular reduction:
```
func mersenneReduce(x uint32) uint32 {
    x = (x & 0x1FFF) + (x >> 13)
    if x >= 8191 { x -= 8191 }
    return x
}
```

**BaseValue:** 5-sparse Golomb ruler stays 5-mark. Offsets distribute across 8191 positions via `(base*mul + add) % 8191`. Multiplier table: `modPow(primitiveRoot, b+1, 8191)`.

**Shell packing:** Affine field mask widens from 9 bits (0x1FF) to 13 bits (0x1FFF). All shift positions in shell.go recalculate. Trajectory/guard/route-hint fields shift accordingly.

**Saturation math:** 5 bits per byte × 8191 core bits = 1,638 bytes before 100% density. At 45% Shannon capacity = ~368 bytes of bundled context. Current: ~11 bytes. Improvement: 33×.

### Files Changed

| File | Change |
|---|---|
| `pkg/numeric/calculus.go` | FieldPrime=8191, Mersenne reduction, new primitive root |
| `pkg/numeric/prime.go` | No change (Primes are PhaseDial frequencies, independent of field) |
| `pkg/system/core/config.go` | CoreBits, CoreBlocks, ShellBlocks, TotalBlocks constants |
| `pkg/logic/lang/primitive/value.capnp` | `blocks @0 :Data` replacing c0..c7 |
| `pkg/logic/lang/primitive/value.go` | Block(i)/setBlock(i,v) against Data blob, pointer receivers, sync.Pool |
| `pkg/logic/lang/primitive/bitwise.go` | Loop over CoreBlocks, pointer receivers |
| `pkg/logic/lang/primitive/rotation.go` | RollLeft/RotationSeed over 8191 bits |
| `pkg/logic/lang/primitive/shell.go` | 13-bit affine masks, recalculated shifts |
| `pkg/logic/synthesis/macro/macro_index.go` | AffineKey as []uint64 or [131]uint64 |
| `pkg/logic/substrate/graph.go` | RecursiveFold density recalibration |
| `pkg/compute/kernel/cuda/resolver.cu` | Warp-cooperative 1KB struct loading |
| `pkg/compute/kernel/metal/resolver.metal` | Threadgroup-cooperative loading |
| All test files | Updated for new Value layout |

---

## Layer 4: Cellular Automata — Active Wavefronts on the Forest

### Problem

The DMT Forest is a passive data store. Prompts execute sequentially through the Cantilever solver. There is no mechanism for the data to "evolve" or "simulate" logical consequences in parallel.

### Solution

Transform the Forest into a Lattice Gas Automaton with Active Wavefronts.

**Neighborhood function:** prefix proximity in the radix trie (siblings share a parent prefix, children extend the key).

**Update rule:** For each active cell V_i, compute XOR delta with each neighbor, look up the corresponding MacroOpcode, apply it. If the opcode is hardened (UseCount > 5), the update is deterministic; otherwise, record it as a candidate.

**Active Wavefronts:** Do NOT update the whole trie. Only apply CA rules to cells with non-zero "energy" (recent state change). Cells at local equilibrium sleep until a new signal propagates through their boundary. This is how optimized physics engines simulate fluid dynamics — sleep regions where the Laplacian is zero.

### Systems Engineering Mitigations

**Broadcast storm prevention:**
- Dirty-flag propagation: each Value gets a 1-bit "active" flag in its shell. Only active cells participate in CA ticks.
- Damping: after each update, energy decreases by a configurable decay factor. Cells that haven't changed in K ticks go to sleep.
- Merkle sync batching: CA updates accumulate in the WAL group commit buffer (from the mechanical sympathy plan) and flush as a single Merkle diff, not per-cell broadcasts.

**Convergence detection:**
- Track global Hamming distance between ticks. When ΔH < ε for M consecutive ticks, the lattice has reached equilibrium.
- The persistence barcode (Layer 2) provides a topological convergence criterion: when the barcode stabilizes, the shape has converged.

### Files Changed

| File | Change |
|---|---|
| `pkg/store/dmt/tree.go` | Active-flag per node, energy tracking |
| `pkg/store/dmt/forest.go` | CA tick loop, wavefront propagation, damping |
| `pkg/store/dmt/network.go` | Batched update broadcast, Merkle diff accumulation |
| `pkg/logic/lang/primitive/shell.go` | Active flag bit in shell word |
| New: `pkg/logic/automata/lattice.go` | CA update rule engine, neighborhood function |
| New: `pkg/logic/automata/wavefront.go` | Active wavefront tracker, convergence detection |

---

## Layer 1: Geometric Algebra — Grade-Restricted Multivectors

### Problem

PhaseDial uses complex128 (2D rotors). This limits spatial reasoning to 1D phase angles. The system cannot natively encode 3D rotations, translations, or projections.

### Solution

Replace PhaseDial's `[]complex128` with grade-restricted Clifford algebra multivectors. Use Projective Geometric Algebra (PGA) Cl(3,0,1) for unified rotation+translation as rotors.

**Why PGA over full Cl(N,0):**
- PGA: 16 components (2^4), but only grades 0+2 needed for rotors = 7 components
- Translations are rotors (not separate operations), eliminating the rotation/translation split
- No gimbal lock, no matrix decomposition

### Systems Engineering Mitigations

**32KB PhaseRotor prevention:**
- Restrict to even-grade subalgebra (rotors): 7 float64 per multivector instead of 16
- PhaseRotor: 512 × 7 = 3,584 float64 = 28KB (still large but manageable with SIMD)
- Custom AVX-512 / ARM NEON intrinsics for the sandwich product v' = RvR†
- Grade selection at compile time via build tags

**Geometric product optimization:**
- The sandwich product Rv R† for PGA rotors has a known closed-form expansion with ~40 multiplications (not the naive 16×16 = 256)
- Precompute the basis multiplication table at init

### Files Changed

| File | Change |
|---|---|
| New: `pkg/numeric/geometry/clifford.go` | Multivector type, geometric product, sandwich |
| New: `pkg/numeric/geometry/pga.go` | PGA-specific rotor construction, grade extraction |
| `pkg/numeric/geometry/phase.go` | PhaseDial → PhaseRotor migration, backward compat |
| `pkg/compute/kernel/cuda/resolver.cu` | GPU sandwich product kernel |
| `pkg/compute/kernel/metal/resolver.metal` | Metal sandwich product kernel |

---

## Layer 2: Topological Data Analysis — Persistent Homology

### Problem

RecursiveFold uses a single density threshold (0.30) to decide when to fall back to PhaseDial similarity. This is one scale. The "shape" of data exists across all scales.

### Solution

Sweep the filtration parameter from 0→1, tracking birth-death pairs of topological features (persistent homology).

### Systems Engineering Mitigations

**O(N³) matrix reduction prevention:**
- Real-time: only compute H_0 (connected components) via Union-Find in O(N α(N))
- Real-time: compute H_1 (1D loops) via incremental cycle detection during RecursiveFold
- Offline: compute H_2 (voids) during MacroIndex garbage collection, not on the hot path
- Filtration steps: 1/CoreBits granularity (8191 steps), but early-terminate when the barcode stabilizes

**Streaming persistence:**
- Maintain a running Union-Find as new Values enter the graph
- Birth events: when a new Value creates a new connected component
- Death events: when an AND operation merges two components
- Barcode updates: O(1) per event (append to birth-death list)

### Files Changed

| File | Change |
|---|---|
| New: `pkg/logic/topology/persistence.go` | Persistence barcode, birth-death tracking |
| New: `pkg/logic/topology/unionfind.go` | Weighted Union-Find with path compression |
| `pkg/logic/substrate/graph.go` | RecursiveFold sweeps filtration, emits persistence events |
| `pkg/logic/synthesis/macro/macro_index.go` | GarbageCollect computes offline H_2 |

---

## Layer 3: Category Theory — Procrustes-Aligned Functors

### Problem

The MacroIndex stores affine opcodes within a single modality. There is no formal mechanism to map structure between modalities (English↔Python, text↔music).

### Solution

Build formal functors between MacroIndex instances using Procrustes alignment on PhaseRotor embeddings.

### Systems Engineering Mitigations

**NP-hard functor discovery prevention:**
- Do NOT brute-force match 8.8 billion states
- Embed each MacroIndex's AffineKey space into the 512-dim PhaseRotor space
- Use SVD-based Procrustes alignment to find the optimal rotation R that minimizes ||R·A − B||²
- Once aligned, the functor becomes nearest-neighbor lookup in the rotated space
- SVD on 512×512 matrices: O(512³) ≈ 134M ops, runs in ~1ms on modern CPU

**Functor validation:**
- Composition preservation check: for sampled morphism pairs (f, g), verify F(g∘f) = F(g)∘F(f)
- Natural transformation coherence: for anchored concepts, verify the naturality square commutes
- Reject functors that fail validation on >5% of sampled pairs

### Files Changed

| File | Change |
|---|---|
| New: `pkg/logic/category/functor.go` | Functor type, Procrustes alignment, validation |
| New: `pkg/logic/category/natural.go` | Natural transformation, coherence checking |
| `pkg/logic/synthesis/macro/macro_index.go` | Export AffineKey→PhaseRotor embedding |
| `pkg/numeric/geometry/procrustes.go` | SVD-based Procrustes solver |

---

## Implementation Sequence

### Phase 1: GF(8191) Foundation (Layer 0)
1. Field arithmetic (Mersenne reduction, primitive root)
2. Value layout (Cap'n Proto Data, pointer receivers, sync.Pool)
3. Bitwise operations (loop over CoreBlocks)
4. BaseValue redesign (8191-bit Golomb ruler)
5. Shell packing (13-bit fields)
6. All consumers (AffineKey, MacroIndex, RecursiveFold, GPU)
7. Full test suite verification

### Phase 2: Cellular Automata (Layer 4)
1. Shell active-flag bit
2. Neighborhood function on radix trie
3. CA update rule engine
4. Active Wavefront tracker
5. Damped propagation with convergence detection
6. Merkle-batched distributed broadcast

### Phase 3: Concurrent (Layers 1, 2, 3)
- Layer 1: Clifford algebra type → PGA rotors → PhaseRotor migration → GPU kernels
- Layer 2: Union-Find → filtration sweep → streaming persistence → offline H_2
- Layer 3: PhaseRotor embedding → Procrustes SVD → functor validation → cross-modal lookup
