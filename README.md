# Six

> This is a research project under active development. 
> Certain code architectural decisions are built for speed, not for comfort. 
> Proper systems engineering is considered deferred until the architecture stabilizes. 
> Feedback is highly appreciated, ideally focused on the architecture as an alternative research model for machine intelligence.

This research project started from a simple question: "Can we reject gradient descent and back-propagation long enough to convince ourselves that we may not need them?"

## The Story

- This is an exploration of alternative machine intelligence architecture, the goal is discovery, not competition.

## Assumptions

Any attempt at new architectural work forces the adoption of a number of assumptions to set a clear direction. It seems prudent to clearly report the assumptions behind the attempt that this repository represents.

- **Overstated Semantic Complexity** poses that human language, in practice, contains less complexity than commonly assumed. Signals that are used to provide foundation for this assumtion come from Zipf's law and Claude Shannon's work. This architecture takes this a step further by rejecting operating at the language semantic level, or to pre-processing incoming data trying to force structure.

## Pillars

- **Inclusivity** should guarantee that anyone with consumer hardware should be enabled to compete at a serious and recognizable scale.
- **Multi-Modality** enforces that all data is treated the same, and the system has no concept of text, image, audio, etc.

## Guidance & Rosetta Stone

- **Forget Modality** if you try to think about text, images, audio, you are effectively reasoning against the system.
- **Do not isolate a single Value** the system does not orchestrate `Values` from a central point, everything is massively connected.
- **Do not try to control** `Fields` emerge from `Values` and `Fields` drive the `System` via `Values`.

---

## Architecture

The core of the architecture, stripped of all orchestration, consists of a global `Field` which contains many community `Fields` which contain many `Values`.

```text
          ┌──────────────────┐
          │ FIELD (GF 65537) │
       ┌─►│ The global field ├─────┐
       │  └─────────┬────────┘     │
Project│            │              │Top-down feedback
upwards│            │              │Bias
       │  ┌─────────▼───────────┐  │Attention
       └──┤ FIELD (GF 8191)     │◄─┘                                 
       ┌─►│ The community field ├──────────────────┐                 
       │  └─────────┬───────────┘                  │
Project│            │                              │
upwards│            │                              │Top-down feedback
       │  ┌─────────▼───────────────────────────┐  │Bias
       │  │ VALUE (GF 257 & GF 2)               │  │Attention
       └──┤ Native Programmable Reasoning Token │◄─┘                    
          └─────────────────────────────────────┘                    
```

### Field Hierarchy

The three finite fields form a phase hierarchy. Each layer aggregates the one below it; gossip carries the vectors peers need to reconstruct the same pressure field.

| Layer           | Field       | Phase state                                                  |
|-----------------|-------------|--------------------------------------------------------------|
| Value           | `GF(257)`   | Per-Value byte-phase, local interference, affine rotation    |
| Community Field | `GF(8191)`  | Regional aggregate from community members                    |
| Global Field    | `GF(65537)` | Global eigenphase aggregated across nodes via gossip digests |

### Data Flow

This is the actual end-to-end path the code implements today:

1. **Byte stream arrives** — `vm.Machine.Load` feeds a `data.Provider` into a `transport.Pipeline`.
2. **Tokenizer chunks** — `vm.Tokenizer` reads from a ring buffer, calls `primitive.NewValue` to mint one or more `Value` segments per chunk. Payload bytes are Morton-coded into 16-bit slot pairs in the token region.
3. **Segments are linked** — Multi-segment Values are chained via `PrevID` / `NextID`. The tokenizer also links successive chunks: the previous tail's `NextID` points to the new head, and the new head's `PrevID` points back.
4. **Program installed** — `programmer.Installer` writes the `"affinity"` program into each Value's program region. This is a compiled frame, not raw words.
5. **Published to Queue + Orchestrator** — Each minted Value is published to the `pool.Queue` (for backend execution) and the `vm.Orchestrator` (for community routing).
6. **Backend executes** — `compute.Backend` dispatches the Value's program to CPU, Metal, or CUDA. The ALU runs the program region against the token region and writes results to signals/context/gradient.
7. **Queue cascades** — If word 117 (`SchedulingNextProgramWord`) is non-zero after execution, the Queue re-publishes the referenced Value for another pass. `next self` makes a Value loop.
8. **Orchestrator groups** — When a Value settles (word 117 = 0), the Orchestrator's `Cycle` picks it up. `findCommunity` computes Hamming distance over the 5 affinity words against existing community fields. Close enough → join; too far or all saturated → spawn a new `GF(8191)` community in the first empty slot.
9. **Community emits actions** — When a community has ≥ 3 members and its dominant mode's concentration exceeds the resonance threshold, `emitActions` mints a new Value from the community's aggregate state, installs a program (`beam_swarm_step` or `active_inference`), and publishes it back to the Queue. The community's Value list is then cleared.

For prompts, mint segments with `primitive.NewValue`, install a program with `programmer.Installer`, then call `Machine.Prompt(segments...)`. Prompt routes the segments on the first `Orchestrator.Cycle` and keeps cycling (no further ingress) until at least one Value is returned — **belief gap ≤ `BeliefEpsilon`** inside a multi-member community (see `Orchestrator.Cycle`). Those returned Values are the resolved output. Use a deadline or cancel on `ctx` passed to `NewMachine` if the field might never reach epsilon.

## Values: Programmable Data

The `Value` type comes from the idea that machine intelligence currently lacks its own distinct "language" and that, to me at least, it seems like a missed opportunity when we force machines to reason using human language. I believe that severely constrains a system, locking it in human-level semantics.

A Value is a `[128]uint64` — exactly 1KB — that serves simultaneously as data, program, and identity. It is the atom of computation in Six.

```text
┌─────────────┬────────────┬────────────┬────────────┬──────────────┬──────────────┬─────────────┬──────┬──────┬─────┬──────────────┐
│   Tokens    │  Program   │  Signals   │  Context   │   Gradient   │  Properties  │ Reserved/K  │ Prev │ Next │ ID  │   Affinity   │
│  1024 bits  │  512 bits  │  512 bits  │  512 bits  │   512 bits   │   512 bits   │  4096 bits  │  64  │  64  │ 64  │   257 bits   │
│ words 0-15  │ words16-23 │ words24-31 │ words32-39 │  words40-47  │  words48-55  │ words56-119 │ 120  │ 121  │ 122 │ words123-127 │
└─────────────┴────────────┴────────────┴────────────┴──────────────┴──────────────┴─────────────┴──────┴──────┴─────┴──────────────┘
```

- **Token region**: Raw input data, packed into 16-bit Morton slots. Each slot couples the payload byte with a geometry-derived position code, so the same substrate can ingest any source that can be projected onto an N-dimensional lattice.
- **Affinity region**: A 257-bit locality-sensitive hash (5 independent SimHash projections, with the final word masked to one bit) that fingerprints the content. This determines which community the Value joins.
- **Program region**: Packed bits the compute kernels interpret (e.g. universal bitwise sweep with per-rotation opcodes in the program words). **Authoring** does not hand-edit raw words: you write lines of source (see below), the programmer **`Compiler`** fills this region from a compiled **`Frame`**. When Values encounter each other, their programs run — no external interpreter needed.
- **Properties region** (words 48–55): 512-bit **canonical property band** — discrete tags, forward-transition statistics, and related state (for example eigenmode / Markov phases over property symbols). Legacy uses (TTL, noise, probe ABI) may still occupy fixed words inside this span until callers migrate.
- **Context / Gradient / Signals**: 64-byte execution lanes. Boolean code treats them as words; geometric code treats them as 8-lane PGA multivectors.
- **Prev/Next**: Linked-list pointers for chaining **segments** of a multi-segment Value (long payloads) and for maintaining sequence order across tokenizer chunks. Values always know their original ordering.
- **ID**: 64-bit unique identifier, assigned by atomic counter at mint time.
- **Word 117** (`SchedulerNextProgramWord`): scheduler next program — `pkg/compute/programmer` and **`Executable.Execute`** write the **ValueID** that should run **after** this frame completes. Zero means settled. `next self` means re-enter the Queue for another ALU pass.

### Properties

Canonical 512 bit region, spanning words 48 to 55. The constants below are defined in `pkg/compute/kernel/layout.go` and are the authoritative word map.

| Word (absolute) | Region offset | Name                                           | Notes                                                                                                                  |
|-----------------|---------------|------------------------------------------------|------------------------------------------------------------------------------------------------------------------------|
| 48              | 0             | **labels**                                     | 4 × 16-bit slots packed low-to-high: slot 0 = bits 0–15, slot 1 = bits 16–31, slot 2 = bits 32–47, slot 3 = bits 48–63 |
| 49              | 1             | **confidence**                                 | Reserved for future use                                                                                                |
| 50              | 2             | **epoch**                                      | Reserved for future use                                                                                                |
| 51              | 3             | **TTL** (`PropertiesTTLWord`)                  | Time-to-live for ephemeral Values; zero means dissolve                                                                 |
| 52              | 4             | **noise** (`PropertiesNoiseWord`)              | Physical noise injected into affinity by the `temperature` program                                                     |
| 53              | 5             | **probe state** (`PropertiesProbeStateWord`)   | Packed probe kind + lifecycle status (see `kernel.PackProbeState`)                                                     |
| 54              | 6             | **probe window** (`PropertiesProbeWindowWord`) | `PackRegionRef` over token words for causal probes                                                                     |
| 55              | 7             | **probe depth** (`PropertiesProbeDepthWord`)   | Re-stabilisation depth for causal hub probes                                                                           |

### Program Authoring (`pkg/compute/programmer`)

Named programs live in **`cmd/cfg/config.yml`** under **`programs:`** as multi-line strings. At runtime, `core.Cfg.Programs` exposes that text; **`NewProgram(nameOrSource)`** resolves a string against that map when the key exists, otherwise treats the string as full source.

Pipeline in order:

1. **`Program.Load()`** — splits non-blank lines and **`strings.Fields`** each line into columns.
2. **`Parser.Parse()`** — returns **`([]Token, *Continuation, error)`**. Operation lines use five fields: **`srcA` `srcB` `dst` `op` `mode`** (region refs like `tokens[0,2]`, `affinity[0]`, `signals[0]`; ops such as `xor`, `popcount`, `and`, `or`; modes `accumulate` or `reduce`). Optionally, **after all op lines**, a single trailing directive may name the **next program by ValueID**: **`next <uint64>`** or **`next self`** (self = reschedule this Value's own ID — recursion / re-entry).
3. **`NewCompiler(tokens, WithContinuation(cont))`** — holds tokens and the optional continuation; **`Compile(CompilerTarget)`** dispatches to CPU / Metal / CUDA lowering and returns **`[]Frame`**. Each **`Frame`** carries a **`Program [64]uint64`**; **`Frame.writeIntoProgramRegion`** copies the configured program word span into a **`primitive.Value`**.
4. **`Executable`** — optional **`WithInputs([]*Value)`** copies **`inputs[0]`**'s full wire into each emitted Value before the frame overwrites the program region. After minting one Value per frame, **`Execute`** writes **word 117** on each Value: **non-final Values** in the batch point to the **following emitted Value's ID** (implicit chain across a multi-frame compile); the **final** Value uses the parsed **`next`** line when present (**literal ID** anywhere in the system, **`next self`**, or omit for no trailing hop). An optional **finalizer** can emit follow-on Values; **`Finalize`** runs on one post-execution Value.

So: **one compiled frame → one program region on one Value**; **N frames → N Values**; **chaining** is expressed both **within a batch** (frame *i* → frame *i+1*) and **after the last frame** (arbitrary ValueID, including self).

### The ALU

The Boolean ALU path is a linear sweep across the `Program` region, where the data in the `Token` region is split up, and then used to perform bitwise operations between two spans. Important to understand is that `Values` are not mutated during this process, the operations are done on copies of the data, and purely emit a `Signal` as the result of each operation, which is written to the `Signals` region.

The physical layout in the program words (including packed per-rotation opcodes and the steady **A** vs rotated **B** token spans) is what the CPU / Metal / CUDA kernels consume after authoring has been lowered into those bits. Rotations advance in steps that match the kernel contract (e.g. 8-bit steps for the universal bitwise path). The high-level **source lines** you edit under `programs:` are **not** the same as raw machine words — the compiler bridges them.

The region-program ABI now lowers each five-column source line into a single Value frame descriptor. Program word 16 carries the descriptor marker, ALU op, and mode; program words 17-19 pack `srcA`, `srcB`, and `dst` region references. The ALU executes those references directly against the Value frame: `accumulate` writes bitwise results back into the destination region, while `reduce` writes popcount scalars into destination lanes. Multi-line programs still materialize as chained Values through word 117, so recursion is `next self` rather than a host-side loop.

Once the `Value` comes out of the `ALU` those `Signals` are used to emit new `Values` which are linked via the `PrevID`, `NextID`, and `ValueID` regions.

The linear sweep is a deliberate limitation over loops and branching, as it is sympathetic to the hardware, eliminating thread divergence on the GPU and enabling parallelism via SIMD on the CPU.

To recover the ability for loops and branching, a final instruction can be written (need to take some of the reserved region) to mark a `Value` for a loop (re)cycle, or branch traversal. When the `Value` comes out of the `ALU` and is marked as such, it is then (re)placed onto a priority `Queue` in the orchestrator and fed back into the `ALU` for another run.

### Geometric ALU

The Boolean ALU keeps the low 4-bit truth-table opcodes exactly as-is. The high nibble now has a Projective Geometric Algebra lane for 64-byte multivectors:

| Opcode | Operation | Frame contract                            |
|--------|-----------|-------------------------------------------|
| `0x10` | Compose   | `Signals = Context · Gradient`            |
| `0x20` | Sandwich  | `Signals = Context · Gradient · Context†` |
| `0x30` | Reverse   | `Signals = Context†`                      |

`Context`, `Gradient`, and `Signals` are each 512-bit regions, so each holds one `pkg/core/numeric/geometry.Multivector` without changing the 1024-byte `Value` stride. The kernel dispatch preserves the full opcode byte before falling back to the Boolean low nibble, so geometric opcodes cannot collapse to `FALSE`. CPU, Metal, and CUDA now all expose the same PGA lane; the native kernels read the high nibble in-band and write their 8-lane result back into `Signals`. CPU uses hand-written ARM64 and AMD64 assembly for the PGA product, CUDA executes the lane as native `float64`, and Metal preserves the 64-bit frame ABI while converting to native `float32` arithmetic at the GPU boundary because Apple Metal does not expose double precision in shader code.

Each newly minted `Value` derives a stable `primitive.FrameMultivector` from its payload and writes it into `Context`. Boolean code can still inspect the same lanes through `ContextVector`, but the geometric path treats the region as a continuous coordinate. The programmer layer can now emit first-class `GeometricIntent` operands, so a `Value` can carry its rotor and target into the compute substrate instead of relying on an external interpreter.

### Signals

When two Values are paired, their token regions are compared using bitwise operations. The results are never written back. They are treated purely as **signal**. The signal dictates which new Values get emitted.

Two operations produce two kinds of signal:

**Cancel (XOR → longest zero-run)**

XOR produces zeros wherever two Values encode the same information. The longest contiguous zero-run reveals the largest shared component.

Given three sentences encoded as Values:

```
Value A:  [Sandra] [is in the] [Garden]
Value B:  [Roy]    [is in the] [Kitchen]
Value C:  [Harold] [is in the] [Kitchen]
```

When Value A is paired with Value B, the XOR of their token regions produces zeros across the bit-span where `is in the` is encoded, because that substring is identical in both. Shorter zero-runs also appear for any incidentally shared bits, but the **longest run wins** and becomes the decisive signal.

The cancel signal emits three new Values:

- `{is in the}` — the shared component, extracted as a structural label
- `[Sandra]` — left residue, linked forward through the label
- `[Roy]` — right residue, linked forward through the label

Repeating this across all pairs builds a graph (using PrevID, and NextID, based on ValueID, this should be by emitting new Values, not by mutating existing ones):

```
{is in the}   -points to→ [Sandra, Roy, Harold]
[Sandra]      -points to→ [Garden]
[Roy, Harold] -points to→ [Kitchen]
[Kitchen]     -points to→ [Roy, Harold]
```

This structure can now answer queries through the same mechanism. The prompt `"Where is Roy?"` cancels `{is}` against the existing `{is in the}` label, which points to `[Sandra, Roy, Harold]`. Then `[Roy]` cancels against that set, resolving to `[Kitchen]`.

**Merge (AND → longest one-run)**

AND produces ones only where both Values agree. The longest contiguous one-run reveals a convergence point, where two Values share dense overlapping structure. Merge emits Values that consolidate the shared region, linking the sources through it.

In the example above, when `[Roy]{is in the}[Kitchen]` is paired with `[Harold]{is in the}[Kitchen]`, the `AND` of their token regions produces a long one-run across `[Kitchen]`, because both Values agree densely there. The merge signal consolidates them: `[Kitchen]` becomes a single node pointing back to both `[Roy]` and `[Harold]`.

**The longest sequential run is always the decisive signal.** Both operations produce multiple runs of varying lengths. `geometry.ScanZeroRun` and `geometry.ScanOneRun` detect the longest zero-run and one-run respectively over the 8 signal words; `geometry.RunLabel` maps the winning run's start position to a deterministic 16-bit label hash. Shorter runs are available for inter-cluster exchange.

---

## Firmware

> The "programmable Value" story has weight only if the system's autonomous behaviors — self-labeling, community crystallization, unsupervised learning — are expressed as **programs that run inside Values**, not as Go code that operates on Values from outside. The programs here are authored in the same five-column source that any user program uses, compiled by `pkg/compute/programmer`, and executed on the same ALU. Where the ALU's rotation-sweep contract makes a computation inexpressible as firmware (see "ALU constraint" note below), the orchestrator performs the operation directly — that is not a retreat from the principle; it is honesty about what the sweep engine can and cannot encode.

### Label Packing (w48)

Word 48 of the Properties region is the **label word**. It holds four 16-bit label slots packed into a single `uint64`:

```text
┌─────────────────┬─────────────────┬─────────────────┬─────────────────┐
│     slot 3      │     slot 2      │     slot 1      │     slot 0      │
│   bits 63–48    │   bits 47–32    │   bits 31–16    │   bits 15–0     │
│  soft label 3   │  soft label 2   │  soft label 1   │  dataset label  │
│  (UL round 3)   │  (UL round 2)   │  (UL round 1)   │  (mint time)    │
└─────────────────┴─────────────────┴─────────────────┴─────────────────┘
```

Slots are packed **low-to-high**: slot 0 occupies bits 0–15 (the lowest 16 bits), slot 3 occupies bits 48–63 (the highest). `kernel.PackClassificationLabelSlots` and `kernel.UnpackClassificationLabelSlots` are the canonical accessors. Slot 0 is reserved for the dataset-provided label (written by the tokenizer when the data provider supplies one). Slots 1–3 are written by the unsupervised learning pass. Zero means unlabeled.

**ALU constraint — label injection:** Inserting a 16-bit value into a specific bit-lane of a 64-bit word requires a clean full-word OR operation. The ALU kernel (`universalBitwiseV2`) is a 16-rotation LSH sweep that produces a byte-level signature — not a clean word-level bitwise operation. The byte-lane packing means the kernel's output is an LSH artifact, not the expected slot value. Until a word-level write instruction is added to the ALU, unsupervised learner Values keep their own result in `signals`/`properties`; they do not mutate source Values from Go.

### Field Crystallization

Each community field maintains a **crystallization score** measured by `vm.measureCrystallization`. The score is composed from three metrics computed by iterating the live Value population:

| Metric         | Semantics                                                 | Computation                                  |
|----------------|-----------------------------------------------------------|----------------------------------------------|
| `Coverage`     | Fraction of members with ≥ 1 non-zero label slot          | `labeled / total`                            |
| `Consensus`    | `1 − normalized_Shannon_entropy` of label distribution    | Over all non-zero slot values across members |
| `LabelDensity` | Mean fraction of the four available slots that are filled | `slotSum / (total × 4)`                      |

```
Crystallization.Score = Coverage × Consensus × LabelDensity
```

When a community's `Coverage` is below `crystallizationFloor` (0.35), `Unsupervised.Cycle` runs a labeling pass on it.

**`measure_field` (firmware resident):** In parallel, a permanent resident Value running the `measure_field` program provides an LSH-based label-energy signal. The orchestrator stages up to 8 member label words (w48) into `reserved[0,8]` before each dispatch. The program runs an OR sweep over those staged words and writes a popcount of the resulting 64-byte LSH signature into `signals[7]`. Higher popcount = more label bits set across the community = higher crystallisation energy. The field's `Cycle` reads `signals[7]` to steer the GF(8191) eigenmode.

```yaml
measure_field: |
  reserved[0,8]  reserved[0,8]  signals[7,1]  or  reduce
  next self
```

### Global Crystallization and the Spawn Trigger

The orchestrator's `Unsupervised.Cycle` iterates every community in the root field and schedules a bounded labeling pass on any community whose `Coverage` is below the floor. This is the **single Go-side trigger** for the unsupervised learning pipeline. The comparison work runs as queued learner Values carrying the `unsupervised_learn` program; each learner is aimed back at the source pair through `PrevID`/`NextID` and keeps its result in-band. The trigger caps learner emission per community and per root-field cycle so the global field creates continuous pressure instead of materializing the full O(U²) pair graph at once.

When `Coverage >= crystallizationFloor` across all communities, `Cycle` returns without spawning anything. The system is quiescent.

### Firmware Programs

The following named programs are defined under `programs:` in `config.yml`. Programs that manipulate full 64-bit words cleanly (LSH fingerprinting, signal accumulation, reductions) are implemented as firmware. Operations that require word-level writes outside the LSH sweep (label injection) are implemented by the orchestrator directly and are documented here as constraints.

#### `affinity`

Computes the 5-word LSH fingerprint over the token region and XOR-accumulates the rotation-sweep signature into the affinity words. This is the routing primitive — the affinity fingerprint determines which community a Value joins.

```
tokens[0,16] tokens[0,16] affinity[0,5] xor accumulate
```

#### `unsupervised_learn`

XOR-compare two peer token regions staged into a learner Value. Before each dispatch the orchestrator copies peer A's tokens (words 0–15) into `reserved[0,16]` and peer B's tokens into `reserved[16,16]` (absolute words 56–71 and 72–87 respectively). The learner is published to the Queue as tracked work. The ALU runs the XOR sweep across the two 16-word spans, accumulating the 64-byte LSH signature into `signals[0,8]`, then reduces that signature into `properties[1]` as the learner's in-band self-knowledge metric.

```
reserved[0,16]  reserved[16,16]  signals[0,8]  xor  accumulate
```

#### `measure_field`

Permanent community resident. See **Field Crystallization** above.

#### `inject_labels` (orchestrator-side)

Not implemented as a rotation-sweep program. Current unsupervised learning does not inject labels into source Values from Go. Settled learner Values keep their own result in-band until a future ALU word-write extension can express source label-slot updates as firmware.

### How Programs See Peer Data — the Reserved Scratchpad

The ALU has a strict single-Value contract: every program line operates on regions of the **currently executing Value** only. Peer data enters through the **Reserved region (words 56–119)**, which the orchestrator uses as a staging scratchpad before dispatch.

```text
peer A tokens[0,15]  → executing Value's reserved[0,16]   (words 56–71)
peer B tokens[0,15]  → executing Value's reserved[16,16]  (words 72–87)
```

The program then runs against those staged words with existing instructions — no new opcodes. The orchestrator selects peers and performs the copy; the ALU stays stateless and single-Value.

### Unsupervised Learning Pipeline

```text
orchestrator.Cycle() calls Unsupervised.Cycle(root) after field.Cycle()
│
▼
For each community with Coverage < 0.35, within the cycle's learner budget:
├─ labelCommunity(community)
│    │
│    ├─ scheduleLearner(peerA, peerB) for all pairs
│    │    orchestrator stages peerA[0:15] → learner.reserved[0,16]
│    │                        peerB[0:15] → learner.reserved[16,16]
│    │    Queue runs learner with unsupervised_learn program
│    │    learner.signals[0,8] carries the shared-structure signature
│    │    learner.properties[1] carries the reduced self-knowledge metric
│
▼
measure_field residents update signals[7] in parallel via next-self loop
```

---

## The Field: Feedback, Bias, Attention, Communication Hub

The field is not one thing. It is simultaneously top-down feedback, compositional bias, an attention mechanism, and the communication substrate for the gossip protocol. Understanding it requires abandoning the idea that attention is a single operation applied to semantic units — the field operates below that level entirely, on the raw population of Values.

**Fields emerge from Values.** A community of Values — say, a group currently running beam search — produces local results. Those beams are passed upward to the GF(8191) community field, which attempts to compose longer beams from the partial beams it receives. Beams that successfully compose reward their contributing Values with amplified attention and bias. Values whose beams did not participate in a successful composition receive a top-down signal that breaks their current beams, preventing them from getting stuck in a local minimum. This is not a metaphor for attention — it is the mechanism. The field rewards productive trajectories and disrupts unproductive ones.

**The affine rotation is the attention mechanism.** Each successful step in a task (text generation, classification, anything that progresses) is a click on the affine rotation in GF(p). These rotations are reversible: if generation drifts in the wrong direction, the rotation clicks backward through history to find the original point of divergence. If a better trajectory is spotted but the current path cannot reach it because of how the sequence started, the system drops one level — from GF(8191) to GF(257) — where the scale is much smaller, rewinds the rotation at the beginning of the generated output, and via backtracking unlocks the better trajectory. The hierarchical field structure makes this practical: coarse corrections at the top, fine-grained rewinding at the bottom.

**Eigenmodes sequence without collision.** The co-occurrence matrix over active Values produces eigenmodes — natural clusters of Values that are evolving together. These eigenmodes provide sequencing: they determine which Values should be composed next, which are ready to emit, and which should wait. Crucially, eigenmodes are orthogonal by construction, so multiple sequencing tasks can run in parallel on the same field without interfering with each other.

> The eigenmodes also have an alternative implementation, which can be found and considered in `pkg/core/numeric/geometry/eigenmode_toroidal.go`.

**The PhaseDial aligns perspectives.** Each field carries a PhaseDial — a 512-dimensional complex vector that encodes the structural fingerprint of its Value population. When two fields need to coordinate (across communities, across nodes, across the global mesh), PhaseDial similarity determines whether they are looking at the same problem from compatible angles. Misaligned perspectives are not suppressed — they are rotated toward alignment when the evidence supports it, or left alone when they represent genuinely different aspects of the state.

**Gossip makes the field a communication substrate.** The gossip protocol does not just propagate statistics — it propagates the field itself. Each digest carries the node's GF(8191) phase snapshot, so remote nodes can reconstruct compatible pressure fields without centralizing data. Phase is the shared coordinate system. This turns the layered field hierarchy into a message-passing network where updates propagate at gossip speed rather than waiting for direct observation. The field is the medium through which the distributed system maintains coherence.

**Values already know their order.** Values are linked via `PrevID` and `NextID`, so the original sequence is always recoverable. The field does not impose ordering on Values — it selects which orderings to amplify, which to break, and which to compose into longer structures. The causal graph is the `PrevID`/`NextID` residency pattern of the live population; the field is what shapes which patterns survive.

### Geometry Library (`pkg/core/numeric/geometry`)

The field hierarchy is backed by a substantial geometry package:

| Module                  | Purpose                                                                                                                |
|-------------------------|------------------------------------------------------------------------------------------------------------------------|
| `field.go`              | `Field` type — GF(p) phase vectors with `Rotate`, `Dominant`, `Dot`, `AccumulateProjected`, `AggregateFromLowerFields` |
| `eigenmode.go`          | `Eigenmode` detection — greedy clustering via coupling functions, `DetectModes`, `PhaseAlignment`                      |
| `eigenmode_toroidal.go` | Toroidal eigenmode variant for wrap-around phase spaces                                                                |
| `phasedial.go`          | `PhaseDial` — 512-dimensional complex vector for perspective alignment                                                 |
| `gf_rotation.go`        | `GFRotation` — uint16 pair in GF(257) for kernel-level affine addressing                                               |
| `pga.go`                | Projective Geometric Algebra — multivector products, sandwich, reverse                                                 |
| `procrustes.go`         | Orthogonal Procrustes alignment between manifolds                                                                      |
| `clifford.go`           | Clifford algebra primitives                                                                                            |
| `scanner.go`            | Signal scanning — longest runs, signal extraction                                                                      |
| `phase.go`              | Phase utilities                                                                                                        |

---

## Reasoning as Gap Closure

Six does not compute answers from inputs. It holds a **believed resolution** and acts to close the distance between that belief and the incoming state. This is the same principle Karl Friston formalised as the **free energy principle** — or equivalently, **active inference** and **predictive coding**. The system is a prediction machine, cognition is the minimisation of prediction error, perception updates the belief, action updates the world, and both directions use the same mechanism. Six implements active inference *in the substrate* rather than *on top of it*: there is no separate inference engine, no loss function, no gradient step, and no epoch. There is only the closure of a phase gap, and the closure itself is the learning.

### The attractor is the local eigenmode

When a prompt Value lands on the Orchestrator, its affinity routes it into a community. That community already has a **dominant eigenmode** — the cluster of Values currently phase-aligned in `GF(257)`, `GF(8191)`, and `GF(65537)`. The eigenmode *is* what the field is already attending to at that coordinate, and it is adopted as the **attractor** for the incoming Value. No separate "goal" is constructed; the goal is whatever belief the field is already holding in the region where the prompt arrived.

The gap between the prompt's affinity and the eigenmode is the drive. The explore program steps the prompt through Morton-space each pass, and every step closes a little more of that gap. Convergence does two things at once:

1. **The prompt moves toward the mode** — the Value ends up resembling what the system already believed about that region.
2. **The mode shifts toward the prompt** — a new Value has joined the cluster, so the next query arriving at this coordinate will find a slightly revised eigenmode.

Both beliefs revise by the act of resolution. Perception and learning are the same operation, executed once.

```text
          ┌────────────────────┐
          │ Local eigenmode    │
          │ (current belief)   │
          └──────────▲─────────┘
                     │ mode drifts
                     │ toward prompt
                     │
      field pressure │
closes the phase gap │
          ┌──────────┴─────────┐
          │ Prompt / Values    │
          │ drifting toward    │
          │ the attractor      │
          └────────────────────┘
```

### Multiple perspectives via phase rotation

Phase rotations in `GF(257)` / `GF(8191)` / `GF(65537)` are *literal angular views* on the same state — applying a rotor moves the observer to a different slice of the same information without changing what is encoded. This gives Six a native mechanism for parallel perspectives: emit *N* phase-rotated copies of the local eigenmode as *N* attractor Values. Each attractor creates a different pressure field; *N* populations race toward their own attractor simultaneously; the convergence profile across the *N* races is the answer to "how does this prompt look from each angle."

Counterfactual reasoning lives inside this rotation space. A "what if the belief were angled this way instead" query is nothing more than an attractor placed at a non-dominant rotation of the current eigenmode.

### Counterfactual, falsification, and causal structure

Classical causal modelling treats the counterfactual as a structural equation and falsification as a separate statistical test. Six collapses both into the same mechanism.

**Counterfactuals are perturbations.** A "what if *X* had happened" query is an **ephemeral Value** encoding *X*, published with a low TTL. Its explore program runs, it emits descendants, the descendants inherit a decremented TTL through `PrevID`, and after a bounded number of hops the whole cascade self-terminates. The answer to the what-if is the population snapshot at the moment the cascade dies — "this is how the live state would have looked if *X* had landed here." Nothing is mutated; the ephemeral lineage leaves no permanent trace. The same substrate that runs real queries runs counterfactuals — only the initial TTL differs.

**Falsification is cancellation with the sign flipped.** The normal cancel signal (`XOR` → longest **zero-run**) rewards agreement: a big shared substring produces a big zero-run and two Values are treated as related. Falsification uses the same `XOR` against a *predicted-absent* pattern. If the hypothesis claims "if *X*, then NOT *Y*," the explore program `XOR`s the downstream Value against the predicted-absent *Y* and looks for a long **one-run** rather than a zero-run. A long one-run confirms disagreement — the claim held. A long zero-run means *Y* appeared where it was predicted absent — the hypothesis is refuted, and the refuting Value is published as evidence.

This gives Popperian falsification a natural substrate. A hypothesis is a Value whose program runs an `XOR` against a predicted-absent successor pattern. **Sharp hypotheses** — narrow, specific claims — produce clean, decisive `XOR` signals when tested. **Vague hypotheses** produce mushy signals and get field-suppressed. Falsifiability becomes a **survival trait in the population**. Popper is not imposed on top of the system; he drops out of the substrate.

**Causal edges are not stored.** They are the `PrevID` / `NextID` residency pattern of the live population itself. When Value *A* reliably precedes Value *B* across many cancel/merge events, that *is* the causal edge from *A* to *B*. Edge weight is how frequently the pair co-occurs in emission lineages. Discovery is emission. Intervention is: drop a Value into the system and observe how the downstream population reshapes under the field. A causal graph query is a walk over `PrevID` / `NextID` links in the live state — the same operation used to chain segments of a long payload. **The graph and the data are the same structure.**

| Concept                         | Substrate mechanism                                                |
|---------------------------------|--------------------------------------------------------------------|
| Believed resolution             | Local eigenmode of the landing community                           |
| Gap as drive                    | Affinity + phase distance between prompt and eigenmode             |
| Perception update               | Prompt converges toward the mode                                   |
| World update                    | Mode shifts as the new Value joins the cluster                     |
| Multiple perspectives / what-if | Phase-rotated attractor Values, one population race per rotation   |
| Counterfactual                  | Ephemeral Value with low TTL, cascade self-terminates after N hops |
| Falsification                   | `XOR` against predicted-absent pattern, long one-run = claim held  |
| Causal edge                     | `PrevID` → `NextID` residency in the live population               |
| Causal discovery                | Emission lineage of cancel / merge signals                         |
| Intervention                    | Publishing a Value and observing downstream drift                  |

### Ephemerality and TTL

Ephemeral Values are the mechanism that lets Six ask questions without polluting state. A **`ttl` lane** lives in the **`properties`** region (historically the same word span as the old `meta` band). It is decremented on every explore step; when it reaches zero the program writes `next 0` into word 117 and terminates. Emissions inherit the parent's TTL through `PrevID`, so an ephemeral lineage dies out within a bounded horizon. Real (non-ephemeral) Values are born with a saturated TTL and are never decremented — they persist until the field prunes them. Counterfactual and falsification queries are just ephemeral Values; the machinery is identical to a normal query, the only difference is the starting TTL.

Because a hypothesis query and a real observation share the same substrate, Six can interleave the two freely. A stream of real observations updates the field. In-between, ephemeral queries probe the field without disturbing it. The field itself cannot tell a query from an observation until the query dies — which means the same dynamics that handle real-world inference handle hypothetical reasoning for free.

---

## Composable I/O — Value, Field, and Conn as `io.ReadWriteCloser`

`primitive.Value`, `geometry.Field`, and `gossip.Conn` all implement `io.ReadWriteCloser`. This is not a serialization convenience — it is the **routing substrate**. Because every participant in the system speaks the same interface, the entire Go standard library's I/O combinators become first-class routing primitives with no additional glue.

### The Ring Buffer as Transport

The lock-free `data.Ring` (Vyukov MPMC, `RingCapacity = 65536` slots) already stores `[128]uint64` frames — the exact stride of a `primitive.Value`. Every `io.ReadWriteCloser` implementation sits on top of one or two `data.Ring` instances: an intake ring (written by `Write`) and an emission ring (drained by `Read`). No goroutines are allocated — `Read` and `Write` are pure atomic operations on head and tail pointers. All goroutine allocation is centralized in `pool.Queue`.

```text
Write(p []byte) → deserialize into [128]uint64 → Ring.Push(ptr)
Read(p []byte)  → Ring.Pop() → serialize [128]uint64 into p
Close()         → cancel intake ring context
```

### Routing as I/O Composition

Because every node is an `io.ReadWriteCloser`, routing is pure stdlib composition:

```go
// Field emits to local queue and two remote fields simultaneously
route := io.MultiWriter(localQueue, remoteField1, remoteField2)

// ALU output is consumed locally and propagated to gossip at the same time
tee := io.TeeReader(emittedValue, gossipConn)
io.Copy(localQueue, tee)

// Directed Value-to-Value handoff: message output → target Reserved (pre-staged before ALU dispatch)
pr, pw := io.Pipe()
io.Copy(pw, messageValue)    // message writes its Signals bytes
io.Copy(targetReserved, pr)  // target reads into Reserved before dispatch
```

No routing tables. No special-cased dispatch paths. The topology is expressed entirely in which `io.ReadWriteCloser` values are passed to which stdlib combinators at connection time.

### Affinity as Address — the `AffinityFilter`

Targeting a specific community or node does not require a routing table. The affinity words (123–127) of each Value are its address. An `AffinityFilter` is a thin `io.Writer` wrapper that inspects those words before forwarding:

```go
type AffinityFilter struct {
    dst    io.Writer
    target [5]uint64
    budget int       // max Hamming distance to forward
}

func (f *AffinityFilter) Write(p []byte) (int, error) {
    if geometry.AffinityHammingDistance(affinityFrom(p), f.target) > f.budget {
        return len(p), nil  // not for us — drop cleanly, not an error
    }
    return f.dst.Write(p)
}
```

Gossip flood reduces naturally to affinity-filtered forwarding: a `Conn` wraps each of its remote peers in an `AffinityFilter` and writes to `io.MultiWriter` over them. Values addressed to a distant community propagate hop-by-hop through peers whose affinity is progressively closer to the target. No DHT, no routing table update protocol.

### Plasticity — the Peer List as Learnable Weights

Each `Field` and `Conn` maintains a `PriorityRoute`: a slice of `io.ReadWriteCloser` peers paired with a floating-point success score. The score is updated on each emission cycle using an exponential moving average over a `successSignal` derived from downstream crystallization improvement.

```go
type ScoredPeer struct {
    dst   io.ReadWriteCloser
    score float64  // EMA of downstream crystallization delta
}

type PriorityRoute []ScoredPeer
```

After each `Cycle`, the slice is re-sorted descending by score. Writes go to all peers; reads prefer the highest-scoring path. Peers whose score drops below a floor are pruned. New peers are admitted when gossip delivers a Value whose source affinity is within budget and whose source field has high crystallization.

**The peer list IS the weight matrix.** Re-sorting IS learning. The communication topology and the computational topology converge without a separate training phase: communities that consistently produce successful emissions become more connected to each other, forming fast paths. Communities that produce nothing become isolated and eventually pruned. This is Hebbian learning at the infrastructure layer — connections that fire together wire together — falling out of the I/O composition model with no additional mechanism.

### Fast Paths

A fast path between two `Field`s is not a special object. It is two `Field`s that have each other at index 0 of their `PriorityRoute` because their shared history of successful emissions has pushed their scores to the top. The fast path emerges from the score dynamics. Removing it is automatic: if the emission success stops, the score decays and the peer slides down the sort order.

### `gossip.Conn` as `io.ReadWriteCloser`

```go
type Conn struct {
    intake   *data.Ring      // lock-free MPMC, no goroutine
    affinity [5]uint64       // this node's fingerprint
    peers    PriorityRoute   // sorted by emission success, reordered each Cycle
}

func (conn *Conn) Write(p []byte) (int, error)  // push into intake ring
func (conn *Conn) Read(p []byte) (int, error)   // pop from intake ring
func (conn *Conn) Close() error
```

`GossipConn` interface collapses to `io.ReadWriteCloser`. The `receiveWithVisited` cycle-detection map is eliminated — cycles cannot form when forwarding is affinity-filtered, because a Value arriving back at its origin has affinity too similar to be forwarded outward again (it falls below the budget floor and is dropped cleanly).

### `geometry.Field` as `io.ReadWriteCloser`

```go
func (field *Field) Write(p []byte) (int, error)
// Deserialize p into a Value, push into the intake ring.
// The next Cycle drains the ring into community routing.

func (field *Field) Read(p []byte) (int, error)
// Pop the next emission from the output ring.
// Community emissions (emitFromCommunity) push into the output ring after Cycle.

func (field *Field) Close() error
```

Fields can now be wired directly into any I/O pipeline. `emitFromCommunity` writes into both the local `pool.Queue` and the field's `PriorityRoute` in one `io.MultiWriter` call. The global field drives community fields the same way — it writes its pressure downward through `io.MultiWriter(community0, community1, ...)`, and each community's `Write` method receives that pressure as a Value that enters its intake ring.

---

## Compute Substrate

Values execute their programs on a multi-substrate backend that automatically selects the best available hardware:

1. **CPU**: Universal bitwise executor with SIMD affinity distance kernels and hand-written ARM64/AMD64 assembly for the PGA product. Supports all 16 boolean truth-table operations plus geometric `Compose`, `Sandwich`, and `Reverse`.

2. **Apple Metal**: GPU compute shaders for macOS. Compiled from Metal Shading Language at build time. The geometric lane preserves the 64-bit frame ABI and uses native `float32` arithmetic in the shader.

3. **NVIDIA CUDA**: GPU kernels for NVIDIA hardware. Generated via cgo bindings. The geometric lane uses native `float64`.

The `compute.Backend` load-balancer probes available substrates at startup and routes work to whichever has the least in-flight depth and lowest exponential moving average service time. When all accelerators are saturated, work overflows to CPU.

---

## Network Transport

Six provides pluggable transport for distributing Values across machines:

- **QUIC**: Reliable, encrypted WAN transport with bi-directional streaming, and congestion control.
- **UDP**: Lightweight datagram transport for local-network gossip.
- **IPC**: Inter-process communication for co-located nodes.

Each transport implements `io.ReadWriteCloser` and carries Values as opaque 1KB frames.

---

## Configuration

Six is configured via YAML (default: `cmd/cfg/config.yml`, overridable via `$HOME/.six/config.yml`):

```yaml
system:
  batchSize: 10000
  batchWindow: 500        # microseconds
  queueSize: 20000

value:
  words: 128
  bytes: 1024
  region:
    tokens:     { start: 0,   bits: 1024 }
    program:    { start: 16,  bits: 512 }
    signals:    { start: 24,  bits: 512 }
    context:    { start: 32,  bits: 512 }
    gradient:   { start: 40,  bits: 512 }
    properties: { start: 48,  bits: 512 }
    prev:       { start: 120, bits: 64 }
    next:       { start: 121, bits: 64 }
    id:         { start: 122, bits: 64 }
    affinity:   { start: 123, bits: 257 }
```

**`programs:`** blocks hold **programmer source** (the five-column line format above), loaded into `core.Cfg.Programs` and parsed by **`pkg/compute/programmer`**, so substrate behavior can be tuned without rebuilding the binary. Lowering from tokens to frames is still evolving alongside the kernels.

---

## Infrastructure

The project includes a Docker Compose stack for observability and data management:

| Service                          | Purpose                                    | Port       |
|----------------------------------|--------------------------------------------|------------|
| Elasticsearch (3-node cluster)   | Log aggregation, experiment telemetry      | 9200       |
| Elasticsearch ML nodes (3 nodes) | Machine-learning-capable ES nodes          | —          |
| Kibana                           | Log visualization and dashboards           | 5601       |
| MinIO                            | S3-compatible object storage               | 9000, 9001 |
| LakeFS                           | Data versioning over MinIO                 | 8000       |
| Custom Interactive Visualizer    | Deep real-time system inspection/debugging | 3000       |

Structured logs are shipped to Elasticsearch via a bulk indexer with configurable flush intervals. Trace-level logging operates independently of the global log level, allowing production systems to emit detailed traces without noise in standard output.

---

## Building

```bash
# Full build (generates primitives, compiles Metal shader, generates CUDA bindings)
make build

# Run tests (linker flag required: pool uses go:linkname into runtime)
make test
# or: go test -ldflags='-checklinkname=0' ./...

# Generate experiment paper
make paper
# The pipeline emits current experiment results; gate pass/fail is stored in
# the result snapshots so paper generation is not blocked by research baselines.

# CPU profile an experiment
make pprof EXP=Text_Classification
```

**Requirements**: Go 1.26+. Metal shader compilation requires macOS with Xcode. CUDA requires NVIDIA toolkit. Both are optional — the highly optimize SIMD CPU backend is always available.

---

## Usage

```go
/*
Create a machine, which acts as a convenient wrapper
around the system.
*/
machine, _ := vm.NewMachine(ctx)
defer machine.Close()

/*
Load a dataset — Values are minted, linked, programmed, and
published to the queue and orchestrator automatically.
Datasets must follow the Provider interface.
*/
machine.Load(dataset)

/*
Prompt — segments + program on first cycle, then cycles until gap closure
(or ctx cancel/deadline). Returned Values are the resolution.
*/
segments, _ := primitive.NewValue([]byte("the cat sat on the"))
installer := programmer.Installer{}
for _, seg := range segments {
	_ = installer.InstallProgram(seg, "beam_swarm_step")
}
resolved, err := machine.Prompt(segments...)
_ = resolved
_ = err
```

---

## Project Structure

```text
six/
├── cmd/                    # CLI commands (root, init, paper)
│   └── cfg/config.yml      # Default configuration
├── pkg/
│   ├── primitive/           # Value type, Morton coding, affinity LSH
│   ├── compute/             # Multi-substrate load balancer
│   │   ├── programmer/      # Program source → tokens → frames → Values
│   │   └── kernel/
│   │       ├── cpu/         # SIMD-optimized bitwise executor + ARM64/AMD64 asm
│   │       ├── cuda/        # NVIDIA GPU kernels
│   │       └── metal/       # Apple Metal GPU shaders
│   ├── core/                # Configuration, data structures
│   │   └── numeric/
│   │       └── geometry/    # Field, PhaseDial, eigenmode, PGA, Procrustes
│   ├── pool/                # Lock-free thread pool + tiered work queue
│   ├── vm/                  # Machine, Orchestrator, Tokenizer
│   ├── gossip/              # Gossip protocol types (Conn, Digest)
│   ├── network/             # QUIC, UDP, IPC transports
│   ├── transport/           # Pipeline, Stream framing
│   ├── viz/                 # Event bus, WebSocket server, timeline
│   └── errnie/              # Structured logging + Elasticsearch shipping
├── experiment/              # Experiment tasks, data loaders, paper generation
│   ├── data/                # HuggingFace + local data providers
│   ├── task/                # Classification, codegen, logic, phasedial, scaling, textgen
│   ├── projector/           # LaTeX/chart templates
│   └── trialmap/            # Trial visualization
├── visualizer/              # React/Three.js 3D visualizer
├── paper/                   # LaTeX paper + generated figures
├── docker-compose.yml       # Elasticsearch, Kibana, MinIO, LakeFS
├── Makefile                 # Build targets
└── main.go                  # Entry point
```

---

## Status

Six is research software under active development. The core mechanisms work and are tested, but the system is not yet production-ready. Current focus areas:

- Field dynamics: wiring community fields to actually drive Value-level behavior via affine rotation and eigenmode sequencing
- Gossip integration: connecting the digest protocol to the network transports
- Multi-node distributed execution across network boundaries
- GPU acceleration of Value program execution at scale

---

## License

See repository for license details.
