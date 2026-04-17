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

The idea here is that the community fields "emerge" from the Values, and the global field emerges from the community fields. This emergence should happen because of how information is projected upwards. The Value has regions that tell us something about it, and projecting this upwards allows the field above it to aggregate many of these data points into an accumulated state, which should allow the field to make desicions on what to do next. "What to do" is mostly expressed as the emission of new (ephemeral) Values that can run algorithms as in-Value/in-band programs.

The gossip mechanism allows Values, Community Fields, and the Global Field to exchange information fast. These communication pipes can be weighted by a learning mechanism, such as reinforcement learning.

### Field Hierarchy

The three finite fields form a phase hierarchy. Each layer aggregates the one below it; gossip carries the vectors peers need to reconstruct the same pressure field.

| Layer           | Field       | Phase state                                                  |
|-----------------|-------------|--------------------------------------------------------------|
| Value           | `GF(257)`   | Per-Value byte-phase, local interference, affine rotation    |
| Community Field | `GF(8191)`  | Regional aggregate from community members                    |
| Global Field    | `GF(65537)` | Global eigenphase aggregated across nodes via gossip digests |

> To be clear, it is the Affinity region that uses GF(257) and rotating it using affine rotation can tell us something about the noise level of the Value.

### Data Flow

This is the actual end-to-end path the code implements today:

1. **Byte stream arrives** — `vm.Machine.Load` feeds a `data.Provider` into a `transport.Pipeline`.
2. **Tokenizer chunks** — `vm.Tokenizer` reads from a ring buffer, calls `primitive.NewValue` to mint one or more `Value` segments per chunk. Payload bytes are Morton-coded into 16-bit slot pairs in the token region.
3. **Segments are linked** — Multi-segment Values are chained via `PrevID` / `NextID`. The tokenizer also links successive chunks: the previous tail's `NextID` points to the new head, and the new head's `PrevID` points back.
4. **Program installed** — `compute.Backend.Dispatch` runs the firmware rule chain (`firmware.NewExecutable`, …), lowering compiled frames into each Value's program region — not raw hand-written words.
5. **Published to Queue + Orchestrator** — Each minted Value is published to the `pool.Queue` (for backend execution) and the `vm.Orchestrator` (for community routing).
6. **Backend executes** — `compute.Backend` dispatches the Value's program to CPU, Metal, or CUDA. The universal-bitwise ALU reads the operand regions named by the program words and writes into the destination region on the same frame; the geometric lane handles high-nibble PGA ops separately.
7. **Queue / scheduling** — If word 117 (`SchedulingNextProgramWord`) is non-zero after execution, the work queue can re-publish that Value for another pass (`next` / `next self` in firmware). This is orthogonal to the ALU kernels, which only execute the current program slice.
8. **Orchestrator → Field** — `vm.Orchestrator.Cycle` copies each input Value's wire frame through a **`gossip.Conn`** (Vyukov ring) and into the root **`mesh.Field.Write`**, which either **routes** into a child community (`findCommunity` over the five affinity words) or **stores** on a leaf. The field's **`Read`** path is the same **`gossip.Conn`** framing Values for downstream `io.Copy`.

For prompts, mint segments with `primitive.NewValue`, install firmware through `compute.Backend.Dispatch` / `firmware.NewExecutable` (or your config `value.rules` chain), then call `Machine.Prompt(segments...)`. **`Prompt` currently forwards to a single `Orchestrator.Cycle`** (see `pkg/vm/machine.go`). Belief-gap closure and multi-cycle prompt loops are described in comments there and in `BEHAVIOR.md`; use a deadline or cancel on `ctx` if work must be bounded.

## Values: Programmable Data

The `Value` type comes from the idea that machine intelligence currently lacks its own distinct "language" and that, to me at least, it seems like a missed opportunity when we force machines to reason using human language. I believe that severely constrains a system, locking it in human-level semantics.

A Value is a `[128]uint64` — exactly 1KB — that serves simultaneously as data, program, and identity. It is the atom of computation in Six.

```text
┌─────────────┬────────────┬────────────┬────────────┬──────────────┬──────────────┬─────────────┬──────┬──────┬─────┬──────────────┐
│   Tokens    │  Program   │  Signals   │  Context   │   Gradient   │  Properties  │   Assets    │ Prev │ Next │ ID  │   Affinity   │
│  1024 bits  │  512 bits  │  512 bits  │  512 bits  │   512 bits   │  1024 bits   │  3584 bits  │  64  │  64  │ 64  │   257 bits   │
│ words 0-15  │ words16-23 │ words24-31 │ words32-39 │  words40-47  │ words48-63   │ words64-119 │ 120  │ 121  │ 122 │ words123-127 │
└─────────────┴────────────┴────────────┴────────────┴──────────────┴──────────────┴─────────────┴──────┴──────┴─────┴──────────────┘
```

- **Token region**: Raw input data, packed into 16-bit Morton slots. Each slot couples the payload byte with a geometry-derived position code, so the same substrate can ingest any source that can be projected onto an N-dimensional lattice.
- **Affinity region**: A 257-bit locality-sensitive hash (5 independent SimHash projections, with the final word masked to one bit) that fingerprints the content. This determines which community the Value joins.
- **Program region**: Packed bits the compute kernels interpret for the **universal bitwise** path: the **low nibble** of the first program word selects the 4-input truth-table opcode; the kernel **broadcasts** that nibble across all 16 internal rotation slots (one schedule—no separate rotation-table word). The next word is **mode** (`accumulate` vs `reduce`); **operand references** occupy the following configured words (`srcA`, `srcB`, `dst` — see `pkg/compute/kernel/layout.go` and `core.Cfg.Value.Region.Program`). **Authoring** does not hand-edit raw words: you write lines of source (see below), `pkg/compute/firmware` **`Compiler`** fills this region from a compiled **`Frame`**. When Values encounter each other, their programs run — no external interpreter needed.
- **Properties region** (words 48–63): 1024-bit **canonical property band** — discrete tags, forward-transition statistics, and related state (for example eigenmode / Markov phases over property symbols). Fixed slots (TTL, noise, probe ABI, community id, firmware status) use the same word indices as `pkg/compute/kernel/layout.go`.
- **Asset region** (words 64–119): 3584-bit scratch and bundled payload (the space that remains before Prev/Next/ID/Affinity); scheduler word 117 and kernel frame metadata at words 118–119 live in this span.
- **Context / Gradient / Signals**: 64-byte execution lanes. Boolean code treats them as words; geometric code treats them as 8-lane PGA multivectors.
- **Prev/Next**: Linked-list pointers for chaining **segments** of a multi-segment Value (long payloads) and for maintaining sequence order across tokenizer chunks. Values always know their original ordering.
- **ID**: 64-bit unique identifier, assigned by atomic counter at mint time.
- **Word 117** (`SchedulerNextProgramWord`): scheduler next program — `compute.Backend` materializes this from lowered firmware (`next` / `next self`); **`Executable.Execute`** runs after install. Zero means settled. `next self` means re-enter the Queue for another ALU pass.

### Properties

Canonical **1024-bit** region, spanning words **48 to 63** (see `value.region.properties` in `cmd/cfg/config.yml`). The leading words below match `pkg/compute/kernel/layout.go`; words **58–63** are reserved for forward-compatible extensions.

| Word (absolute) | Region offset | Name                                           | Notes                                                                                                                      |
|-----------------|---------------|------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------|
| 48              | 0             | **labels**                                     | 4 × 16-bit slots packed low-to-high: slot 0 = bits 0–15, slot 1 = bits 16–31, slot 2 = bits 32–47, slot 3 = bits 48–63     |
| 49              | 1             | **confidence**                                 | Overall confidence calculated from the results of any algorithm that leaves an arifact (classification, beam search, etc.) |
| 50              | 2             | **epoch**                                      | +1 for any algorithm run                                                                                                   |
| 51              | 3             | **TTL** (`PropertiesTTLWord`)                  | Time-to-live for ephemeral Values; zero means dissolve, one means it can take one action, etc.                             |
| 52              | 4             | **temperature** (`PropertiesNoiseWord`)        | The scaler that determines how "creative" the model will be while executing algorithms                                     |
| 53              | 5             | **probe state** (`PropertiesProbeStateWord`)   | Packed probe kind + lifecycle status (see `kernel.PackProbeState`)                                                         |
| 54              | 6             | **probe window** (`PropertiesProbeWindowWord`) | `PackRegionRef` over token words for causal probes                                                                         |
| 55              | 7             | **probe depth** (`PropertiesProbeDepthWord`)   | Re-stabilisation depth for causal hub probes                                                                               |
| 56              | 8             | **community** (`PropertiesCommunityWord`)      | Community index after `mesh.Field` routing (see `mesh.communityIDOffset`)                                                  |
| 57              | 9             | **status** (`PropertiesStatusWord`)            | Value status: IDLE, READY, BUSY, WAITING, DONE, OK, ERROR                                                                  |
| 58–63           | 10–15         | **fieldId**                                    | The community `Field`'s ID this `Value` belongs to                                                                         |

### Program Authoring (`pkg/compute/firmware` — config-time DSL only)

Named programs live in **`cmd/cfg/config.yml`** under **`programs:`** as multi-line strings. At runtime, `core.Cfg.Programs` exposes that text; **`NewProgram(nameOrSource)`** resolves a string against that map when the key exists, otherwise treats the string as full source.

Pipeline in order:

1. **`Program.Load()`** — splits non-blank lines and **`strings.Fields`** each line into columns.
2. **`Parser.Parse()`** — returns **`([]Token, *Continuation, error)`**. Operation lines use five fields: **`srcA` `srcB` `dst` `op` `mode`** (region refs like `tokens[0,2]`, `affinity[0]`, `signals[0]`; ops such as `xor`, `popcount`, `and`, `or`; modes `accumulate` or `reduce`). Optionally, **after all op lines**, a single trailing directive may name the **next program by ValueID**: **`next <uint64>`** or **`next self`** (self = reschedule this Value's own ID — recursion / re-entry).
3. **`NewCompiler(tokens, WithContinuation(cont))`** — holds tokens and the optional continuation; **`Compile(CompilerTarget)`** dispatches to CPU / Metal / CUDA lowering and returns **`[]Frame`**. Each **`Frame`** carries a **`Program [64]uint64`**; **`Frame.writeIntoProgramRegion`** copies the configured program word span into a **`primitive.Value`**.
4. **`Executable`** — optional **`WithInputs([]*Value)`** copies **`inputs[0]`**'s full wire into each emitted Value before the frame overwrites the program region. After minting one Value per frame, **`Execute`** writes **word 117** on each Value: **non-final Values** in the batch point to the **following emitted Value's ID** (implicit chain across a multi-frame compile); the **final** Value uses the parsed **`next`** line when present (**literal ID** anywhere in the system, **`next self`**, or omit for no trailing hop). An optional **finalizer** can emit follow-on Values; **`Finalize`** runs on one post-execution Value.

So: **one compiled frame → one program region on one Value**; **N frames → N Values**; **chaining** is expressed both **within a batch** (frame *i* → frame *i+1*) and **after the last frame** (arbitrary ValueID, including self).

### The ALU

The Boolean **universal bitwise** path reads three **region references** from the configured program words: **A**, **B**, and **destination**. Operand data is read from those spans inside the **same** `[128]uint64` frame. The kernel folds **A** into four lanes, tiles **B** across an internal 16-step sweep, applies the truth-table opcode defined by the **program word’s low nibble** (repeated at every step), and packs a 64-byte signature; **mode** chooses whether that signature is **XOR-accumulated** into the destination words or **reduced** to a single popcount written at `dst[0]`. So the ALU **does** write back into whatever region the compiled program names as `dst` (often `signals`, sometimes `affinity`, etc.) — it is not a side-effect-free pure read of tokens.

The high-level **source lines** under `programs:` are **not** the same as raw machine words — the compiler lowers them into the program region; CPU, Metal, and CUDA share the same interpretation: opcode nibble, mode word, then packed `PackRegionRef` operands (`pkg/compute/kernel`).

Scheduling hops (**word 117**, `next` / `next self` in firmware) are still part of the toolchain for multi-step programs, but the **kernels themselves** only execute the universal-bitwise (and geometric) opcodes described here — they do not interpret a separate “region program” table in the reserved band.

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

Each newly minted `Value` derives a stable `primitive.FrameMultivector` from its payload and writes it into `Context`. Boolean code can still inspect the same lanes through `ContextVector`, but the geometric path treats the region as a continuous coordinate. The firmware compiler can now emit first-class `GeometricIntent` operands, so a `Value` can carry its rotor and target into the compute substrate instead of relying on an external interpreter.

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

> The "programmable Value" story has weight only if the system's autonomous behaviors — self-labeling, community crystallization, unsupervised learning — are expressed as **programs that run inside Values**, not as Go code that operates on Values from outside. The programs here are authored in the same five-column source that any user program uses, compiled by `pkg/compute/firmware`, and executed on the same ALU. Where the ALU's **signature-sweep** contract (byte-packed LSH output) makes a computation inexpressible as firmware (see "ALU constraint" note below), higher-level code keeps results in-band on `signals`/`properties` — that is honesty about what the sweep engine can and cannot encode.

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

**ALU constraint — label injection:** Inserting a 16-bit value into a specific bit-lane of a 64-bit word requires a clean full-word OR operation. The universal bitwise kernel is a 16-step LSH-style sweep that produces a **byte-packed signature** — not a direct 16-bit lane write. The packing means the raw sweep output is not the same shape as a single classification slot. Until a word-level write instruction exists in the ALU, unsupervised learner Values keep their own result in `signals`/`properties`; they do not mutate source Values from Go.

### Field Crystallization

Each community field maintains a **crystallization score** computed by **`mesh.MeasureFieldMetrics`** over the live members (and eigenmode snapshot when present), stored via **`Field.refreshMetrics`** on ingest and leaf **`Cycle`**. Routing parents expose a **weighted rollup** of child metrics (`RollupFieldMetrics`). The score is composed from three metrics:

| Metric         | Semantics                                                 | Computation                                  |
|----------------|-----------------------------------------------------------|----------------------------------------------|
| `Coverage`     | Fraction of members with ≥ 1 non-zero label slot          | `labeled / total`                            |
| `Consensus`    | `1 − normalized_Shannon_entropy` of label distribution    | Over all non-zero slot values across members |
| `LabelDensity` | Mean fraction of the four available slots that are filled | `slotSum / (total × 4)`                      |

```
Crystallization.Score = Coverage × Consensus × LabelDensity
```

When a community's `Coverage` is below `crystallizationFloor` (0.35), higher-level rules (e.g. unsupervised / orchestration) can run a labeling pass on it — the field itself only **measures** and **emits** pressure through the same I/O paths.

**`measure_field` (firmware resident):** A resident Value can run the `measure_field` program so peers stage label-bearing state through the gossip substrate (`Conn.Write` → `StageAssetFrom`), with `asset[0,8]` carrying routed peers' compressed state. The YAML line OR-reduces into `signals[7,1]` as an in-band energy readout. Separately, **`mesh.Field`** observability comes from **`MeasureFieldMetrics` + eigenmode detection** (`refreshMetrics` / `Cycle` on leaves), not from Go reading that resident's `signals[7]` on every tick.

```yaml
measure_field: |
  asset[0,8]  asset[0,8]  signals[7,1]  or  reduce
  next self
```

### Global Crystallization and the Spawn Trigger

When a community's `Coverage` falls below the floor, the field emits an ephemeral carrier Value aimed (by affinity) at the learners it wants to activate. Delivery goes through the same gossip substrate every other Value uses: `io.Copy` from the emitting field into a `gossip.Conn` whose bundle is the target learner population, with `io.MultiWriter` fanning the frame across additional peers where fast paths exist. `Conn.Write` calls `StageAssetFrom` on every receiver, so the learner wakes up with the emitter's Signals+Context+Gradient+Properties already in its asset window — no Go-side registry lookup, no orchestrator-side token copy. The field's carrier emission is the trigger; the community-wide pressure arises from many carriers landing in parallel rather than a central loop materializing the O(U²) pair graph.

When `Coverage >= crystallizationFloor` across all communities, no carriers are emitted and the system is quiescent. Carriers are minted with **`primitive.Emit`** (see **`mesh.Field.BuildPressureCarrier`**) so pressure metadata and firmware install stay on the same wire path as ordinary Values.

### Firmware Programs

The following named programs are defined under `programs:` in `config.yml`. Programs that manipulate full 64-bit words cleanly (LSH fingerprinting, signal accumulation, reductions) are implemented as firmware. Operations that require word-level writes outside the LSH sweep (label injection) stay in-band on the learner Value or in Go helpers — see the ALU constraint above.

#### `affinity`

Computes the 5-word LSH fingerprint over the token region and XOR-accumulates the universal-bitwise signature into the affinity words. This is the routing primitive — the affinity fingerprint determines which community a Value joins.

```
tokens[0,16] tokens[0,16] affinity[0,5] xor accumulate
```

#### `unsupervised_learn`

Peer-similarity readout. The peer under comparison is delivered into the learner through the gossip substrate: `io.Copy` from the peer into a `gossip.Conn` that bundles the learner as its receiver. `Conn.Write` invokes `StageAssetFrom`, which copies the peer's contiguous Signals+Context+Gradient+Properties block into the learner's `asset[0,32]`:

```text
asset[0,8]   ← peer.signals
asset[8,8]   ← peer.context
asset[16,8]  ← peer.gradient
asset[24,8]  ← peer.properties (canonical 8-word band)
```

The program XORs the learner's own context against the peer's context (`asset[8,8]`) and OR-reduces the 64-byte signature into `properties[1,1]` as the in-band similarity metric. Shared structure surfaces as a long zero-run; divergence as a long one-run.

```
context[0,8] asset[8,8]   signals[0,8]    xor accumulate
signals[0,8] signals[0,8] properties[1,1] or  reduce
```

#### `episodic_replay`

Same primitive as `unsupervised_learn`, different routing convention: the carrier is the sequence predecessor (chosen by `PrevID` residency) rather than an unrelated community member. The chain delta — how this Value diverges from the one that preceded it — is the XOR of local context against the predecessor's staged context, reduced into `properties[1,1]`. No `next self`: one observation per delivered predecessor; multi-hop walks are chains of deliveries through the gossip substrate, not program loops.

#### `intervene` (Pearl L2 `do(X)`)

Counterfactual perturbation. A carrier from a foreign community is written into this Value with no `PrevID` stamp, severing causal history. `StageAssetFrom` delivers the foreign S+C+G+P into `asset[0,32]`, and the program XORs local context against the injected gradient (`asset[16,8]`) — the direction the do-operation is pushing the receiver relative to the attractor it was already converging on. Reduced signature lands in `properties[0,1]` as the intervention witness.

#### `classify_readout`

Tiny label readout. When a Value carries a dataset label in properties slot 0 (word 48), the `classify` rule fires this single OR line to broadcast the label into `signals[0,1]` so downstream observers can see the class without mutating the source Value.

#### `measure_field`

Permanent community resident. See **Field Crystallization** above.

#### `inject_labels` (not implemented)

Unsupervised learning does not inject labels back into source Values from Go. Settled learner results stay in-band on the learner itself. A future ALU extension for word-level writes would let label slots 1–3 be written by firmware; until then, labels flow only through emitted carrier Values.

### How Programs See Peer Data — the Gossip Substrate

The ALU has a strict single-Value contract: every program line operates on regions of the **currently executing Value** only. Peer data reaches a program by being **written into that Value** through an `io.ReadWriter` composition:

```text
peer Value   ──io.Copy──▶   gossip.Conn   ──Write──▶   receiver.asset[0,32]
```

`Value`, `gossip.Conn`, and **`mesh.Field`** all implement `io.ReadWriteCloser`, so `io.MultiWriter`, `io.MultiReader`, and `io.TeeReader` express fan-out, fan-in, and fast paths without any custom routing layer. `Conn.Write` invokes `StageAssetFrom` on every bundled receiver, copying the source's Signals+Context+Gradient+Properties (48 words) into `asset[0,48]`. The ALU then runs with peer state already in-band. No Go-side registry. No per-program staging path. Selection of who writes to whom is the field's job, expressed by which `io.ReadWriteCloser` ends up wired to which.

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

**Causal modelling is a system behaviour, not an inference call.** The rule engine in `cmd/cfg/config.yml` drives every Value through the causal cycle autonomously — no Go-side orchestration ever asks "should this Value now form a hypothesis?" The rules observe region state and fire firmware, in this order once the bootstrap `link → affinity → explore` cascade has settled:

1. **`hypothesize`** — affinity routed, `context[0,8]` and `gradient[0,8]` carry a live belief, `properties[0,1]` holds the reduced surprisal, and `properties[1,1]` (the refutation target) is still empty. The `hypothesis` program XORs context against gradient into `signals[0,8]`, reduces that signature into `properties[1,1]`, and folds the Value's own `id[0,1]` to guarantee a non-zero target. **This is the "what if" question being asked autonomously.**
2. **`falsify`** — a target is armed. The existing `falsification` program runs the Popperian test: XOR tokens against context, reduce back into `properties[1,1]`. The kernel's `ApplyRefutationProbe` runs post-ALU — when signals have a ≥48-bit one-run it stamps `FalsifiedBitNoiseWord` into `properties[4,1]`, clears the heartbeat, and clears `properties[1,1]`. No Go-side classifier reads signals; the refutation test lives in the kernel.
3. **`iterate_causal`** — `properties[4,1]` is stamped. `causal_hub` loops via `next self`, drifting gradient through stacked `asset[40,8]` residuals. Each heartbeat advances the belief along the counterfactual the refutation revealed — this is the "what would be different if that claim weren't true" question being answered in-band.
4. **`do_intervention`** — a foreign carrier with severed history (`prev[0,1]: false`) lands via `Conn.Write` with its gradient staged in `asset[16,8]`. The `intervene` program folds the foreign gradient into local gradient and reduces the scalar witness into `properties[0,1]`. The rule is placed before `peer_gap` so severed-history carriers take the do-operation path instead of the unsupervised similarity path — the semantics diverge even though both observe a staged peer.

Agency is the rule engine's first-match-wins traversal over the Value's own region state. Hypotheses are generated because the shape of the regions after `beam_swarm_step` is exactly the shape `hypothesize` matches. Refutations cascade into counterfactuals because `ApplyRefutationProbe` stamps a witness that `iterate_causal` reads. Nothing in the Go code decides when to ask "what if"; the substrate asks.

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

`primitive.Value`, **`mesh.Field`**, and `gossip.Conn` all implement `io.ReadWriteCloser`. This is not a serialization convenience — it is the **routing substrate**. Because every participant in the system speaks the same interface, the entire Go standard library's I/O combinators become first-class routing primitives with no additional glue.

### The Ring Buffer as Transport

The lock-free `data.Ring` (Vyukov MPMC) backs **`gossip.Conn`** and the **`pool.Queue`**: `Conn.Write` pushes frame bytes into the ring; `Conn.Read` pops them. That is the hot path for orchestrator → field fan-in. A **`primitive.Value`** itself is already a fixed `[128]uint64`; its **`Read`/`Write`** methods marshal wire bytes in place without a ring. **`mesh.Field.Read`** delegates to its embedded **`gossip.Conn`**, which uses the ring.

```text
gossip.Conn.Write(p) → Ring.Push(...)
gossip.Conn.Read(p)  → Ring.Pop(...) → copy into p
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

### Affinity as address

The five **affinity** words (see `value.region.affinity` in config) fingerprint each Value. **`mesh.Field`** routing compares incoming frames to **community seeds** with a Hamming-distance scan (`findCommunity`); that is the live addressing story today. A separate **affinity-filtered `io.Writer`** (drop frames whose decoded affinity is outside a Hamming budget of a target) is a natural stdlib-sized building block for multi-hop fan-out — compose it when you need it; it is not a dedicated type in `pkg/` yet.

### `gossip.Conn` as `io.ReadWriteCloser`

`gossip.Conn` bundles zero or more **`primitive.Value`** references with a **`pool.Queue`**, a **`data.Ring`** for streaming frames, and telemetry teeing. **`Write`** forwards bytes into the ring (and staging paths described in `pkg/gossip/conn.go`). **`Read`** round-robins **`Value.Read`** over the bundle so each full frame is one read. See the package docs for the exact staging / executable submission contract — the struct fields are not the simplified “intake + peer scores” sketch from earlier drafts.

### `mesh.Field` as `io.ReadWriteCloser`

```go
func (field *Field) Write(p []byte) (int, error)
// Deserialize a wire frame into a Value; on a leaf, append and refresh metrics;
// on a routing parent, Hamming-route into the winning sub-field.

func (field *Field) Read(p []byte) (int, error)
// Delegate to the embedded gossip.Conn — round-robin read of stored Values as frames.

func (field *Field) Close() error
```

A `mesh.Field` is constructed with a **`gossip.Conn`** (`field.conn`) and optional **`pool.Queue`** for backend publishing. Wire it with `io.Copy`, `io.MultiWriter`, and the same patterns as `Value` and `Conn` — there is no separate “field stream” type beyond this composition.

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

**`programs:`** blocks hold **DSL source** (the five-column line format above), loaded into `core.Cfg.Programs` and parsed by **`pkg/compute/firmware`**, so substrate behavior can be tuned without rebuilding the binary. Lowering from tokens to frames is still evolving alongside the kernels.

**`finalizers:`** blocks hold generic post-ALU orchestration rules. They do not define new Go-side algorithms. Instead they specify when the runtime should either **reprogram the current Value** with an existing named program or **emit an ephemeral clone** of the current Value, optionally copying already-written in-band regions (for example `value.signals` or `field.affinity`) into another region before the next ALU pass.

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
Prompt — forwards to orchestrator.Cycle (see vm/machine.go). Returned
Values are the field snapshot after that cycle; cancel the context to bound work.
*/
segments, _ := primitive.NewValue([]byte("the cat sat on the"))
// Production uses value.rules + queue dispatch; this is illustrative:
// backend.Dispatch(firmware.NewExecutable(seg, "beam_swarm_step")) per segment.
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
│   │   ├── firmware/       # Config-time DSL → tokens → frames (not the in-band programmer Value)
│   │   └── kernel/
│   │       ├── cpu/         # SIMD-optimized bitwise executor + ARM64/AMD64 asm
│   │       ├── cuda/        # NVIDIA GPU kernels
│   │       └── metal/       # Apple Metal GPU shaders
│   ├── core/                # Configuration, data structures
│   │   └── numeric/
│   │       └── geometry/    # GF(p) Field, PhaseDial, eigenmode, PGA, Procrustes
│   ├── mesh/                # Community/global Field (Values + routing + metrics)
│   ├── pool/                # Lock-free thread pool + tiered work queue
│   ├── vm/                  # Machine, Orchestrator, Tokenizer
│   ├── gossip/              # Gossip protocol types (Conn, Digest)
│   ├── network/             # QUIC, UDP, IPC transports
│   ├── transport/           # Pipeline, Stream framing
│   ├── telemetry/           # Event bus, binary wire protocol, bridge transport
│   └── errnie/              # Structured logging + Elasticsearch shipping
├── experiment/              # Experiment tasks, data loaders, paper generation
│   ├── data/                # HuggingFace + local data providers
│   ├── task/                # Classification, codegen, logic, phasedial, scaling, textgen
│   ├── projector/           # LaTeX/chart templates
│   └── trialmap/            # Trial visualization
├── visualizer/              # React debugger UI + local telemetry bridge
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
