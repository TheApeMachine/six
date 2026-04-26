# The Six Programming Syntax

This document defines the canonical programming syntax for high-level, in-value programming. It represents a paradigm shift: **the complete unification of computation, routing, and state management into a single array-oriented mathematical syntax.**

By treating network operations as just another bitwise ALU instruction, this syntax allows a swarm of `Values` to execute Active Inference, Causal Modelling, and Autonomous Reprogramming natively on GPU/SIMD hardware—achieving a **total divorce from Go-side orchestration**.

---

## 1. Values: Programmable Data (The ABI)

The `Value` type comes from the idea that machine intelligence currently lacks its own distinct "language". A Value is a `[128]uint64` — exactly 1KB — that serves simultaneously as data, program, and identity. It is the atom of computation in Six.

```text
┌─────────────┬────────────┬────────────┬────────────┬──────────────┬──────────────┬─────────────┬──────┬──────┬─────┬──────────────┐
│   Tokens    │  Program   │  Signals   │  Context   │   Gradient   │  Properties  │   Assets    │ Prev │ Next │ ID  │   Affinity   │
│  1024 bits  │  1024 bits │  512 bits  │  512 bits  │   512 bits   │  1024 bits   │  3072 bits  │  64  │  64  │ 64  │   257 bits   │
│ words 0-15  │ words16-31 │ words32-39 │ words40-47 │  words48-55  │ words 56-71  │ words72-119 │ 120  │ 121  │ 122 │ words123-127 │
└─────────────┴────────────┴────────────┴────────────┴──────────────┴──────────────┴─────────────┴──────┴──────┴─────┴──────────────┘
```

**Symbolic vs. Absolute Addressing:** 
Six source code targets **symbolic regions** (e.g., `program`, `tokens`, `properties.surprisal`). The `pkg/compute/firmware` Compiler lowers these symbolic names to the canonical 1KB ABI (e.g., `16..31`, `0..15`, `68..68`). 

### Properties (Words 56-71)
Canonical **1024-bit** region for discrete tags, forward-transition statistics, and scalar witnesses.

| Word (abs) | Region offset | Symbolic Name    | Notes                                                                                                                                                                             |
|------------|---------------|------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| 56         | 0             | **labels**       | 4 × 16-bit slots packed low-to-high.                                                                                                                                              |
| 57         | 1             | **confidence**   | Overall confidence calculated from algorithm artifacts.                                                                                                                           |
| 58         | 2             | **epoch**        | +1 for any algorithm run.                                                                                                                                                         |
| 59         | 3             | **TTL**          | Time-to-live for ephemeral Values. 0 means dissolve.                                                                                                                              |
| 60         | 4             | **temperature**  | The scaler that determines generative "creativity".                                                                                                                               |
| 61         | 5             | **status**       | Value status enum (e.g., PENDING, READY, DONE).                                                                                                                                   |
| 62         | 6             | **probe window** | Window size for causal probes.                                                                                                                                                    |
| 63         | 7             | **probe depth**  | Re-stabilisation depth for causal hub probes.                                                                                                                                     |
| 64         | 8             | **community**    | Stable recruiter `ValueID` stamped onto Values accepted by in-band community recruitment.                                                                                         |
| 65         | 9             | **target**       | ValueID of an addressable target.                                                                                                                                                 |
| 66         | 10            | **role**         | In-band `ValueRole`.                                                                                                                                                              |
| 67         | 11            | **reference**    | ValueID to encounter before the target.                                                                                                                                           |
| 68         | 12            | **surprisal**    | Scalar reduction of the prediction error gap.                                                                                                                                     |
| 69         | 13            | **falsified**    | Witness register for Popperian hypothesis testing.                                                                                                                                |
| 70         | 14            | **stuck**        | Triggers autonomous reprogramming based on stagnation.                                                                                                                            |
| 71         | 15            | **continuation** | The ValueID to schedule next. Writing `id` (Word 122) here creates a recursive loop. Writing another ValueID creates a branch or sequence. Halts if 0. Replaces the old word 117. |

---

## 2. The Core Anatomy

Every instruction in Six follows an explicit data flow pipeline:

```text
[ (Target & Routing) <= (Computation) <= (Scope) ]
```

- **Target & Routing:** Where does the result go, and through what network topology?
- **Computation:** What bitwise math or reduction are we performing on the source memory?
- **Scope:** Across which population of Values is this instruction applied?
- **Predicate (Optional):** `? (Condition)` allows branchless conditional execution.

### Example: A Basic Local Sweep
```text
[ (program self) <= (rom.unsupervised | 0) ? (properties.stuck != 0) <= community ]
```
**Reads as:** "For all values in the `community`, if `stuck != 0`, write `rom.unsupervised` to their own `program` region."

---

## 3. Topology Routing (The Network is the ALU)

The routing keyword inside the Target block eliminates the need for Go-side orchestrators to move data.

| Keyword | Topology  | Behavior                                                                                                                            |
|---------|-----------|-------------------------------------------------------------------------------------------------------------------------------------|
| `self`  | Local     | The ALU writes the result into the same `Value` that executed the instruction.                                                      |
| `next`  | Ring      | The ALU shifts the result to the adjacent `Value` in the community ($i \to i+1 \pmod N$).                                           |
| `fold`  | Hypercube | The ALU runs the $O(\log_2 N)$ hypercube routing fold across the entire community, and writes the global consensus into the target. |
| `spawn` | Scatter   | Allocates a new `Value` frame in the community and writes the result into it.                                                       |

### Note on `fold` Semantics and Synchronization
`fold` requires an implicit **synchronization barrier** (Tick/Tock double-buffer or `__syncthreads()`). Furthermore, `fold` **must only be used with associative and commutative operators** (like `^`, `|`, `&`, `popcnt`) to guarantee deterministic convergence, unless strict ordered butterfly semantics are explicitly desired.

### Range Semantics
All numeric ranges in Six syntax are **half-open `[start, end)`**. 
For example, `16..24` targets 8 words starting at index 16 up to 23 (words 16, 17, 18, 19, 20, 21, 22, 23).

---

## 4. Mathematical Operators

These define the 4-bit truth tables applied by the Universal Bitwise kernel.

| Operator | Concept    | Description                                           |
|----------|------------|-------------------------------------------------------|
| `0`      | `false`    | Writes physical zeroes.                               |
| `&`      | `and`      | Intersection.                                         |
| `\`      | `aandnotb` | Novelty. Acts as an eraser.                           |
| `A`      | `a`        | Passthrough A.                                        |
| `/`      | `notandb`  | Reverse Novelty.                                      |
| `B`      | `b`        | Passthrough B.                                        |
| `^`      | `xor`      | Difference. Finds parity or structural gaps.          |
| `\|`     | `or`       | Union. Combines knowledge.                            |
| `~\|`    | `nor`      | Neither A nor B.                                      |
| `==`     | `xnor`     | Strict Equality.                                      |
| `~B`     | `notb`     | Inverts Operand B.                                    |
| `<-`     | `ifbthena` | Superset.                                             |
| `~A`     | `nota`     | Inverts Operand A.                                    |
| `->`     | `ifathenb` | Subset/Implies. Produces 1s where the rule is obeyed. |
| `~&`     | `nand`     | Inverted intersection.                                |
| `1`      | `true`     | Writes physical ones (`0xFF...`).                     |

---

## 5. Reduction Operators (Scalar Witnesses)

Intelligence relies on collapsing wide bit-vectors (like a 512-bit `signals` region) into scalar "witnesses" (like a surprisal score) to trigger state transitions. Truth-table operators alone cannot do this. Six includes explicit reduction intrinsics:

| Intrinsic     | Behavior                                                                                       |
|---------------|------------------------------------------------------------------------------------------------|
| `popcnt(A)`   | Returns the total number of set bits (1s) in region A.                                         |
| `any_zero(A)` | Returns 1 if *any* bit in region A is 0. (Useful for checking if `A -> B` implication failed). |
| `all_ones(A)` | Returns 1 if *all* bits in region A are 1.                                                     |

**Example:**
```text
[ (properties.surprisal self) <= popcnt(signals) <= community ]
```

---

## 6. Control Flow: Predicate Execution (`?`)

GPUs and SIMD engines suffer massive performance penalties when branching. We use **Predicated Execution** instead.
An instruction executes the math universally, but only commits its write to memory if the predicate condition evaluates to true.

### How Predicates Interact with Topology
When a predicated instruction uses a global topology like `fold`, the predicate **does not mask participation in the fold**. To preserve the $O(\log N)$ butterfly network, all `Values` must participate. The predicate **only masks the final write**.

```text
[ (gradient fold) <= (scratch ^ context) ? (properties.falsified != 0) <= community ]
```
**Reads as:** "Perform a community-wide fold of `scratch ^ context`. Then, *only* Values where `falsified != 0` will overwrite their own `gradient` with the result."

Predicate conditions may also reduce a region with `popcnt` and compare it
against an immediate threshold using `| N`. In predicate position this means
"at most N set bits"; it is not the bitwise OR operator.

```text
[ (properties.community next) <= (id[0,1]) ? (popcnt(affinity[0,5]) | 120) <= community ]
```
**Reads as:** "Write this Value's ID into the routed peer's community word only
while this Value's affinity region has no more than 120 set bits."

---

## 7. The Non-Negotiable Execution Contract

For Six source to be valid, it must lower to deterministic substrate operations. This syntax acts as a strict execution contract:

1. **Bounded Memory:** All reads and writes must be clamped to the 1KB boundaries of a `Value`. Dynamic addressing (`*`) that attempts to read/write out of bounds is clamped or masked to `0`.
2. **Conflict Resolution:** Multiple Values writing to the same target (e.g., via `next` or `spawn`) use deterministic conflict resolution. By default, simultaneous writes to the same address are combined using `OR`.
3. **Execution Order:** Reads observe the **pre-state** of the current clock tick. Writes are committed to a double-buffer and only become visible on the next tick.
4. **Scheduling (`properties.continuation`):** A program loop only continues if it writes its own ID (or a valid target address) to the `continuation` property word (word 71). Zeroing `continuation` halts execution for that Value. This completely replaces the legacy "word 117" logic.
5. **Allocation Bounds:** `spawn` allocates a new frame. If the arena is exhausted, the predicate fails silently (no-op), enforcing strict memory safety without Go-side panics.

Every grand claim of intelligence in Six must be backed by a concrete **scalar witness** (e.g., `surprisal`, `falsified`, `stuck`) governed by this exact contract.

---

## 8. Complex Autonomy Examples

These examples demonstrate how theoretical intelligence maps to physical scalar witnesses and ALU operations.

### Active Inference (Gap Closure)
Active inference "works" only if `properties.surprisal` decreases over repeated `continuation` passes, or the Value terminates.

```text
; 1. PERCEPTION: Measure the gap (tokens ^ context).
[ (signals self) <= (tokens ^ context) <= community ]

; 2. SURPRISAL: Reduce the gap to a scalar witness.
[ (properties.surprisal self) <= popcnt(signals) <= community ]

; 3. NETWORK RESONANCE: Fold the gap across the community to find the global attractor.
[ (asset.pressure fold) <= (signals | signals) <= community ]

; 4. ACTION: Update the local belief (context) by drifting toward the community's pressure.
[ (context self) <= (context ^ asset.pressure) <= community ]
```

### Causal Modelling (Intervention & Falsification)
Falsification "works" only if a predicted-absent pattern produces a witness in `properties.falsified`, changing the routing or gradient.

```text
; 1. POPPERIAN TEST: Does 'tokens' strictly imply 'context'? (tokens -> context)
[ (signals self) <= (tokens -> context) <= community ]

; 2. THE WITNESS: If ANY 0s exist in the signals, the hypothesis is FALSIFIED.
[ (properties.falsified self) <= any_zero(signals) <= community ]

; 3. CAUSAL DRIFT: If falsified, push the community gradient away from this belief.
[ (gradient fold) <= (gradient ^ context) ? (properties.falsified != 0) <= community ]
```

### Autonomous Reprogramming (The Spark of Agency)
Reprogramming "works" only if a Value whose `properties.stuck` crosses a threshold installs a different compiled frame.

```text
; 1. TRIGGER: Check if Surprisal is maxed out AND we are running INFERENCE.
[ (properties.stuck self) <= (properties.surprisal == MAX & properties.program_id == INFERENCE) <= community ]

; 2. THE REWRITE: Fetch the UNSUPERVISED binary from ROM and overwrite the Program region.
[ (program self) <= (rom.unsupervised | 0) ? (properties.stuck != 0) <= community ]

; 3. STATE UPDATE: Mark the new program ID.
[ (properties.program_id self) <= (UNSUPERVISED) ? (properties.stuck != 0) <= community ]

; 4. EMISSION (SPAWNING): If the Value successfully learns something new (surprisal drops to 0), spawn it.
[ (value spawn) <= (value self) ? (properties.surprisal == 0 & properties.program_id == UNSUPERVISED) <= community ]
```

### Out-of-Corpus Generation (Synthesizing Novelty)
Six generates out-of-corpus novelty physically, by applying affine rotations to geometric coordinates (Morton-coded tokens) and using falsification to reject invalid structural mutations. *(Note: Falsification enforces structural validity; semantic usefulness requires grounding the geometry accurately to begin with).*

```text
; 1. THE AFFINE ROTATION: Rotate the current token vectors toward the prompt's context.
[ (tokens self) <= (tokens ^ prompt.context) <= community ]

; 2. THE BOOLEAN FILTER: Check if the mutated tokens break structural rules in Context.
[ (properties.falsified self) <= any_zero(tokens -> context) <= community ]

; 3. THE CULLING: Erase the generated token if it violates causal logic.
[ (value self) <= (0) ? (properties.falsified != 0) <= community ]

; 4. THE EMISSION: If the novel coordinate survived falsification, spawn it into the persistent sequence.
[ (value spawn) <= (value self) ? (properties.falsified == 0) <= community ]
```

---

## 9. The Minimal "Actually Works" Implementation Ladder

To prove that Go is no longer secretly orchestrating the intelligence, the compiler and kernel must be built via this strict, pragmatic ladder. We do not attempt Out-of-Corpus Generation on day one. 

1. **Local Deterministic ALU:** Compile one syntax line into the existing firmware frame format and verify exact word-level output for `self`.
2. **Predicate Commit:** Prove that all Values execute the same computation, but only matching Values commit the write.
3. **Scheduler Re-entry:** Prove `properties.continuation` re-enters the kernel queue and halts deterministically when zeroed.
4. **Peer Staging:** Prove one Value can receive another Value’s state into `asset` and run a local program over it.
5. **Next Routing:** Prove ring message passing (`next`) across a community with double-buffered writes.
6. **Fold Routing:** Prove only associative reductions (`OR`/`AND`/`XOR`/`popcnt`) across the hypercube with a strict synchronization barrier.
7. **Spawn:** Prove bounded allocation, ID assignment, TTL propagation, and deterministic failure behavior when the arena is full.
8. **One Cognitive Loop:** Implement active inference gap closure. Prove the community converges on a `surprisal` threshold using only resident programs, with Go merely dispatching the queue.

## Conclusion: Total Divorce from Go

By implementing this syntax inside an AST that lowers directly to 64-bit Kernel Instructions:

1. **`fold`, `next`, and `spawn`** replace Go's network routing and orchestrator loops.
2. **Predicate `?`** replaces Go's rule engine and state machine logic.
3. **Mathematical and Reduction operators** replace Go-side heuristic evaluations.
4. **Dynamic Addressing (`*`)** replaces Go-side variable management.

Go is reduced to a bootloader. It compiles the `SYNTAX.md`, allocates the RAM arena, hands it to the GPU/SIMD engine, and stops executing. The Substrate becomes the entire computer.

### Notation (authoring grammar)

The **bracket pipeline** in §2 is how programs are spelled in `config.yml` and tests today. The table below is the same algebra in the composable surface form (pipe / operation / feed).

```text
 ;      **comment**    ignored/stripped by compiler
[ ]     **pipe**       build something to be realized
{ }     **operation**  Reverse Polish notation over regions and ops
<=>     **feed**       take what this *is* (realize) and move in direction; the
                       bond is two-way. In bracket sources the feed is the `<=` token
                       (one arrow per site in the current scanner).
 !      **is not**     when something is not
 ?      **gate**       must open or the buck stops here
 A      **value**      the program runner
 B      **values**     hypercube gossip operands; implicit map over B
< >     **emit**       continuation / return
```

Bare `A`/`B` sources use the implicit mapped form: `[(B popcnt)] <= [(A B ^)]`
materializes the resident runner against each mapped `B` frame before reducing.
Region and property operands must be explicit `A(...)` or `B(...)`; a bare
region like `affinity[0,5]` is a compiler error because it has no frame owner.
Gates evaluate on the mapped source frame so a `B(...)` write is masked by that
candidate's own witnesses.

## Examples

```
; a simple example
[(B popcnt)] <= [(A B ^)] ; stages A to XOR with each B, materialize result
                          ; and get popcount of each *(implicit map)* B value

; recruit communities
<[{ A clear }]> [
    { A(affinity) B(affinity) ^ }
] <= [
    { A(status) done ^ }
] <= [
    { A(id) B(community) ^ }
] <= [
    { { A(affinity) popcnt } 120 ? }
] <= [
    { { A(affinity) B(affinity) ^ } 64 | }
]

; structural compose
<[
    { B(prev) B(id) ^ }                              ; map over Bn ([]*primitive.Value) and write the mapped id to prev region
    { B(next) B(id) ^ }                              ; map over Bn ([]*primitive.Value) and write the mapped id to next region
] <= [
    { A(status) done ^ }                             ; set status of A to done
    { B(tokens) B(signals[0, 1]) ^ }                 ; map over Bn ([]*primitive.Value) and write signals[0, 1] to tokens region
    { B(tokens) B(signals[1, 1]) ^ }                 ; map over Bn ([]*primitive.Value) and write signals[1, 1] to tokens region
    { B(tokens) B(signals[2, 1]) ^ }                 ; map over Bn ([]*primitive.Value) and write signals[2, 1] to tokens region
]> [                                                 ; emit new values to the substrate
    { B(signals) }                                   ; map over Bn ([]*primitive.Value) and write results of previous pipe to signals region
] <= [
    { B(tokens[0,16]) B(tokens[0,16]) & }            ; map over Bn ([]*primitive.Value) tokens applying AND
    { B(tokens[0,16]) { B(tokens[0,16] 8 <<) } & }   ; map over Bn ([]*primitive.Value) tokens and rotated tokens applying AND
    { B(tokens[0,16]) { B(tokens[0,16] 16 <<) } & }  ; map over Bn ([]*primitive.Value) tokens and rotated tokens applying AND
    { B(tokens[0,16]) { B(tokens[0,16] 24 <<) } & }  ; map over Bn ([]*primitive.Value) tokens and rotated tokens applying AND
    { B(tokens[0,16]) { B(tokens[0,16] 32 <<) } & }  ; map over Bn ([]*primitive.Value) tokens and rotated tokens applying AND
    { B(tokens[0,16]) { B(tokens[0,16] 40 <<) } & }  ; map over Bn ([]*primitive.Value) tokens and rotated tokens applying AND
    { B(tokens[0,16]) { B(tokens[0,16] 48 <<) } & }  ; map over Bn ([]*primitive.Value) tokens and rotated tokens applying AND
    { B(tokens[0,16]) { B(tokens[0,16] 56 <<) } & }  ; map over Bn ([]*primitive.Value) tokens and rotated tokens applying AND
    { B(tokens[0,16]) { B(tokens[0,16] 64 <<) } & }  ; map over Bn ([]*primitive.Value) tokens and rotated tokens applying AND
    { B(tokens[0,16]) { B(tokens[0,16] 72 <<) } & }  ; map over Bn ([]*primitive.Value) tokens and rotated tokens applying AND
    { B(tokens[0,16]) { B(tokens[0,16] 80 <<) } & }  ; map over Bn ([]*primitive.Value) tokens and rotated tokens applying AND
    { B(tokens[0,16]) { B(tokens[0,16] 88 <<) } & }  ; map over Bn ([]*primitive.Value) tokens and rotated tokens applying AND
    { B(tokens[0,16]) { B(tokens[0,16] 96 <<) } & }  ; map over Bn ([]*primitive.Value) tokens and rotated tokens applying AND
    { B(tokens[0,16]) { B(tokens[0,16] 104 <<) } & } ; map over Bn ([]*primitive.Value) tokens and rotated tokens applying AND
    { B(tokens[0,16]) { B(tokens[0,16] 112 <<) } & } ; map over Bn ([]*primitive.Value) tokens and rotated tokens applying AND
    { B(tokens[0,16]) { B(tokens[0,16] 120 <<) } & } ; map over Bn ([]*primitive.Value) tokens and rotated tokens applying AND
]
```

## Specification

- A program is read/interpreted from bottom to top
- A program is a sequence of `pipe` objects
- A program can have an optional `emit` stage

### Pipe `[ ]`

- A pipe is a staging area for work that is to be realized
- A pipe is materialized once it is given a direction to materialize to (`<=` `=>`)
- A pipe is read/interpreted from right to left

### Operation `{ }`

- An operation is the staging primitive inside of a pipe
- It uses Reverse Polish Notation, where the first element is `dst` and the last element is `op`
- An operation can have an optional `data`/`src` element in between `dst` ans `op`
- Multiple operations at the same nesting level implies potential concurency, with fan-in at the next `<=`
- An operation is read/interpreted from left to right

### Emit `< >`

- An emit stage is a sequence of `pipe` objects
