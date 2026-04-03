![image](infographic.png)

# six

> This is a research project under active development. Certain code architectural decisions are built for speed, not for comfort. Proper systems engineering is considered deferred until the architecture stabilizes.
> Feedback is highly appreciated, ideally focused on the architecture as an alternative research model for machine intelligence.

This research project started from a simple question: *"Can we reject gradient descent and backpropagation long enough to convince ourselves that we may not need them?"*

## Motivations

1. I got tired of waiting on training runs.
2. I feel that the barrier to entry for testing large scale models is too high, and the hardware requirements prohibitly expensive.
3. I disagree with the cost (financial, environmental, societal) versus benefit of current state of the art models.

## Assumptions

There is no denying that this work is experimental, and relies on highly personal assumptions to drive the work forwards, so it seems prudent to explicitely state what these assumptions are.

1. **Intelligence is compression** Of course it is recognized that most questions on intelligence have not been answered yet, however one must take a stance on this to effectively research along a conherent direction. So this project operates under the clear assumption that intelligence is the ability to compress information.
2. **Entropy is overstated** I find enough truth in work done by people such as [George Kingsley Zipf](https://en.wikipedia.org/wiki/George_Kingsley_Zipf) to believe that structure exists everywhere, so I want to investigate an idea where I move the battlefield by not fighting the natural structures within data, no matter how chaotic it might seem to my own eyes. In other words, I question wether or not the fault lies with my own ability to perceive the structure, rather than assume the data is not structured.

## F.R.C. (Frequently Raised Critiques)

1. **The entropy of natural language** I would suggest this discussion is better with [George Kingsley Zipf](https://en.wikipedia.org/wiki/George_Kingsley_Zipf). I propose we agree that on either side of this argument we are operating purely on assumtion. I am just testing the other side, there are no guarantees, until there are.
2. **Rigid bitwise operations are too brittle** Agreed, if you are considering the `Value` type in isolation. But in the substrate the `Value` type is not by itself, the gradient you may be looking for is still there, it is in the field.
3. **Unstructured data** Disagree, it is unstructured only if you feel the need to force the data into a structure that works for your architecture. This architecture accepts the natural structure of the data as already perfectly conditioned.
4. **The Gemma reliance** That is a misreading of why the experiment exists in the first place. This is the only experiment that uses a large language model, and its origin is a direct answer to the first thing most people say when first taking a look at this architecture: "Is it a transformer killer? No." This has never been relevant to the motivation behind this architecture, however, I figured I would show some early value to the traditional machine learning field and investigate what is possible (and what is not possible) by integrating multiple architectures. I think the early results, while limited in scope and scale, reveal interesting signals.

## Types

This section provides a high-level overview of the main types that play a structural role in the architecture.

### Value

The Value is a 1024-byte (128 × uint64) fixed-size frame that serves as the fundamental unit of computation, memory, and structure. Every piece of data in the system is a Value. Every instruction that runs is stored inside a Value. The graph that emerges from computation is made of Values pointing to other Values.

A Value's 128 words are divided into regions:

| Region    | Words  | Purpose                                                                                                                                                                                                                             |
|-----------|--------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Tokens    | 0–56   | Data payload. Raw content is encoded here using a hyperdimensional shift-and-XOR scheme, where each byte is bound with a position-dependent signature. This produces a distributed representation that supports bitwise comparison. |
| Identity  | 57–59  | ValueID (unique), PrevID, and NextID. These form the linked-list pointers that let Values chain into sequences and graphs.                                                                                                          |
| State     | 60–62  | StateIndex, StateSequence, and StateAccumulator. These track the Value's computational progress across execution cycles.                                                                                                            |
| Affinity  | 63     | A combined Bloom filter and SimHash locality-sensitive hash over the token region. Used for fast approximate nearest-neighbor lookup without scanning the full token payload.                                                       |
| Registers | 64–75  | General-purpose registers r0–r9 plus `fw` and `pc`. These remain in-band state words; the backend does not mutate them out-of-band after execution.                                                                                 |
| Program   | 76–127 | 52 words of instruction memory holding 32-bit slot instructions, packed two per word. This region is where the bootstrap program and evolved payload programs live.                                                                  |

Values implement `io.Reader` and `io.Writer`, which means they can be piped through standard Go I/O infrastructure (files, network connections, streams) without any serialization overhead. The in-memory layout *is* the wire format.

Values are pooled and reference-counted. A Value must be tombstoned before it can be released back to the pool. The long-term substrate model is still an in-band tombstone program that zeros the token, affinity, and program regions while propagating through the stream to clean up dangling PrevID/NextID references. The current host-side `Value.InstallTombstone` path wipes the frame eagerly before release so prompt-evaluation and test cleanup remain deterministic on the self-only backend.

### Backend

The Backend executes self-only bitwise programs over Value frames. It is the system's ALU. The current live instruction format is a 32-bit slot packed into the program region:

- bits `[3:0]`: 4-bit truth-table opcode (`FALSE`, `AND`, `A ∧ ¬B`, `A`, `¬A ∧ B`, `B`, `XOR`, `OR`, `NOR`, `XNOR`, `¬B`, `A ∨ ¬B`, `¬A`, `¬A ∨ B`, `NAND`, `TRUE`)
- bits `[17:4]`: source word index
- bits `[31:18]`: destination word index

Execution is a straight sweep across the configured program region, two slots per `uint64` word. A zero slot is a NOP, not a halt. There is no partner Value, no `c1` context, and no host-side post-run rewrite of `fw`, `pc`, `accumulator`, or the program band. If those bits move, they move because the in-band program wrote them.

The Backend operates on raw `unsafe.Pointer` frames, processing batches in a tight loop. The CPU path groups homogeneous programs for a gather/scatter word kernel, while CUDA and Metal execute the same self-only slot sweep on packed slabs so all three backends share one runtime contract.

**Extended VSA slots (bit 31 set).** When `[31]=1`, the slot is *not* a truth table. Fields are: `[6:0]` extended opcode, `[13:7]` argA, `[20:14]` argB, `[27:21]` argC (all 7-bit word indices or small immediates). Opcodes: `1` token-region XOR strip with frame words starting at argA (bind/unbind), `2` three-way majority per word (argA/argB strips), `3` rotate each token `uint64` left by `argC & 63`, `4` cyclic roll of token words by `argC mod 57`, `5` mark a **memory load** pending in `State.Index` / `State.Accumulator` for host resolution (`compute.WithMemoryLoad` + `primitive.ProcessMemoryLoadRequests`), `6` **resonator unbind**: XOR a rotated **macro-graph** strip (built by `primitive.MacroGraphAccumulateFromChild` on emissions) into tokens; `argC` = rotation, `argB != 0` selects **PrevID** as pivot key instead of ValueID. Opcode `6` is a **no-op on GPU** kernels (macro lives on host). Helpers: `cpu.PackExtendedInstruction`, `cpu.DecodeExtendedInstruction`.

**Substrate tuning (`system` in `config.yml`).** There is a single runtime shape: emission tries longest-run signals first, then holistic chunked Hamming; exploit scoring takes the stronger of the two. `NewValue` always applies temporal rotation, per-byte bind, and trigram superposition. Post-batch work always includes MAP-Elites archive + injection (`mapElitesInjectionRate`, `mapElitesGridShift`), XOR-bind `HolographicCrossoverXORBind`, thermodynamic decay/gain (`thermodynamicEnergyWord` — `-1` ⇒ `r9` — and the birth/decay/gain fields), semantic affinity refresh / control-plane reinsert, macro-graph accumulation, token settling (`tokenSettleMaxPasses` / `tokenSettleEpsilonBits`; non-positive passes are replaced with `core.DefaultTokenSettleMaxPasses` at load), and sleep consolidation on a **fixed** ticker (`core.SubstrateSleepIdle`, `sleepMaxPairs`). Holistic gating still uses `holisticChunkBits` and `holisticHammingMax`. Ingress coalescing uses `max(batchWindow, evolutionBatchWindow)`.

## Concepts

This section provides a description of some high-level concepts that underpin the direction this architecture takes to approach specific problems.

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

Repeating this across all pairs builds a graph:

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

**The longest sequential run is the decisive signal in `runs` mode.** Both operations produce multiple runs of varying lengths. `ScanSignals` detects all of them, sorts by length, and the longest of each kind becomes the local action. Shorter signals are published for inter-cluster exchange. In `holistic` / `both` modes, chunked Hamming similarity can also trigger emission (full-token AND consolidation) without requiring a long contiguous run; see `pkg/primitive/holistic_signal.go`.

**Implementation (stream ingest).** `pkg/vm.Machine` now pairs each dataset line with the **previous** line’s frame (the first line still pairs with itself). After learn, it registers the full XOR workspace via `StructureFromWorkspace` as before, then calls `EmitFromPairwiseSignals`: `SplitSignals` picks the decisive cancel and merge spans, and each cancel span expands into up to three `Structure` frames (shared bit-run from the agreement region, left residue, right residue), each Prev-linked to the parent canonical Value, HIE-blended, and inserted into the spatial index. Merge spans emit a consolidation frame from the AND row. Token-empty cuts still register under a synthetic spine key so every emission has an LSM row.

**Query hook (tests).** `vm.ResolvePromptIntersection` takes prompt bytes and intersects LSM postings for each `Tokenize(byte, index)` key (same as `NewValue` indexing). After a `Machine` ingest, a shared prefix such as `X: ` should resolve all matching canonical rows (`Machine.IngestedCanonicalValueIDs` lists them). `vm.PrevChainBackward` follows `Prev` through stored frames so a signal cut can be traced to its canonical parent. Run `go test ./pkg/vm -run 'TestResolvePromptIntersection|TestPrevChainBackward' -count=1`. Full in-Value query firmware and `experiment/task.Pipeline` integration are still future work.

### Firmware

Values do not carry general-purpose programs from birth. Instead, they reference firmware: short, pre-compiled instruction sequences stored in a shared pool. A Value's `fw` register selects which firmware to install, and the bootloader copies it into the program region on the next execution cycle.

There are four firmware types:

| Firmware   | Register        | Purpose                                                                                                                                                      |
|------------|-----------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Bootloader | (pre-installed) | Sets `fw=1` (Learn) and initializes registers. Every new Value starts here.                                                                                  |
| Learn      | 1               | XORs self token words with partner token words using a DJNZ loop across all 57 data words. After execution, the token region contains the cancel signal map. |
| Build      | 4               | ANDs self token words with partner token words across all 57 data words. After execution, the token region contains the merge signal map.                    |
| Tombstone  | 2               | Zeros out the token region and affinity word, preparing the Value for pool recycling.                                                                        |

Firmware is compiled from a small assembly language defined in `config.yml` and compiled by `CompileFunc`. The ISA supports `MEM` (load/store/immediate/indirect), `ALU` (any truth-table opcode), `CTL` (conditional skip, shift, loop), and `HALT`.

### Linear Genetic Programming

The program region of a Value is not limited to firmware. After the firmware prefix (the first 4 words reserved for bootstrap logic), the remaining program slots are available for evolved code.

The LGP subsystem provides three mechanisms to make this safe:

**Introns.** Protective no-op instructions are inserted at regular intervals throughout the program region. These act as firebreaks during crossover: if a crossover boundary falls on an intron, no working code is damaged. An instruction is classified as an intron if it copies a register to itself (identity with src == dst) or is zero.

**Execution tracing.** `TraceEffective` performs a simulated dependency analysis of the program, tracking which instruction slots ultimately influence the output register (r6). This produces a bitmask of "effective" instructions. Everything else is dead code that can be safely overwritten.

**Homologous crossover.** When two Values are crossed, `HomologousCrossover` only copies effective instructions from the donor, and only into non-effective slots in the recipient. If both have effective code at the same slot, the donor wins with probability proportional to the instruction's bit complexity. This prevents catastrophic destruction of working logic while still allowing new behavior to be introduced.

**Holographic instruction encoding (HIE).** `HolographicCrossover` (`pkg/compute/firmware/hie.go`) multiplexes each 32-bit LGP slot into eight disjoint 8-bit bands, majority-blends two donors with an affine-generated third parent, and decodes each band to the nearest valid nibble. The third parent is generated in instruction space, then HIE-encoded, so crossover keeps a structured opcode/src/dst orbit instead of raw random byte noise. The firmware bootstrap prefix is never overwritten.

**Substrate exploit score.** `SubstrateExploitScore` (`pkg/primitive/substrate_fitness.go`) is experiment-agnostic: it uses `ScanSignals` / `SplitSignals` on the parent vs workspace token regions and returns the longest local cancel or merge run normalized to `[0, 1]`. That value biases the affine third parent toward donor A (the canonical parent frame), so sharp token-level structure increases exploitation of the canonical program; weak structure keeps the third parent on a broader affine orbit instead of collapsing to white noise. `HolographicCrossoverTwoParent` takes the same `parentBias` argument for call sites that only have one donor.

### Token Encoding

Raw bytes are not stored verbatim in the token region. Each byte is encoded using a hyperdimensional computing scheme:

1. The token region is circular-left-shifted by one bit.
2. The byte is XOR-bound with a pre-generated 3648-bit random signature unique to that byte value.

This produces a distributed, position-sensitive representation. Two Values encoding the same string will have highly similar token regions (low Hamming distance). Two Values encoding different strings will have roughly 50% bit disagreement. This property is what makes XOR-based cancel detection work: shared substrings produce long zero-runs in the XOR because their encoded representations are nearly identical.

Affinity is derived from the token region in two ways: a Bloom filter over 3-byte n-grams for exact substring matching, and a SimHash LSH for approximate similarity. Both are OR'd into the single affinity word, giving a compact fingerprint for spatial indexing.

### Tombstoning

Deletion in this system is not passive. When a Value needs to be discarded, it is not simply dereferenced. Instead, the tombstone firmware is installed, which zeros out the token, affinity, and program regions. The Value's ID is written to a register, and the tombstone spreads virally through the stream: as it encounters other Values, it XORs the dead ValueID against their PrevID and NextID fields. Matches are cleared, effectively unlinking the dead node from the graph.

This means garbage collection is eventually consistent and proportional to connectivity. A leaf node's tombstone clears quickly. A highly connected hub takes longer, but that is appropriate because there are more references to clean up.

A Value cannot be returned to the pool until tombstoning is verified. Attempting to close a Value that still has non-zero data in its token, affinity, or program regions returns a `ValueErrorNotTombstoned` error.

### Distributed Execution

Values are 1024 bytes. This is not an accident. It is the size of a single UDP payload that fits comfortably within typical MTU limits, allowing Values to be transmitted between nodes without fragmentation.

The distributed layer uses UDP multicast for peer discovery with heartbeats and TTL-based expiry. Each node advertises an affinity shard mask, allowing the cluster to partition the Value space by affinity region. Work is distributed through a scheduler that assigns Value pairs to workers based on available capacity and shard ownership.

QUIC connections handle reliable point-to-point transfer when Values need to move between nodes. An IPC transport exists for same-machine communication between processes. An S3 adapter provides durable storage for Values that need to survive node restarts.

### Experiment pipeline

`experiment/task.Pipeline` is the paper-facing execution path. It is expected to run the real system, not a stripped-down surrogate, because its generated `Artifacts` are compiled directly into the research paper. Dataset ingress, machine execution, prompt observation, and cleanup therefore belong to the same end-to-end path that the rest of the substrate uses.

`Holdout` remains supervision metadata on the scoring side. It must not be copied into `Observed`, and it must not be allowed to leak into prompt execution through hidden shortcut wiring. If an experiment stages resident corpus data ahead of prompt evaluation, that staging is still part of the real system path. Any reduced execution mode is acceptable only when it is explicitly treated as an ablation, debugging aid, or microbenchmark, never as the default paper pipeline.

## Project Structure

```
six/
├── cmd/                    CLI entry points (init, mesh, worker, viz, paper)
├── experiment/             Benchmark suite and paper generation
│   ├── data/               Dataset loaders (HuggingFace, local)
│   ├── projector/          LaTeX chart and figure rendering
│   └── task/               Benchmark tasks (bAbI, classification, text generation, scaling)
├── pkg/
│   ├── primitive/          Value type, signal detection, VSA encoding
│   ├── compute/
│   │   ├── firmware/       Firmware loader, LGP crossover, intron management
│   │   └── kernel/         CPU (AVX2 + scalar), CUDA, and Metal backends
│   ├── core/               Config, ISA compiler, firmware definitions
│   ├── vm/                 Stream processing machine
│   ├── store/              LSM-tree spatial index for affinity lookup
│   ├── network/            UDP multicast, QUIC, IPC transports
│   ├── distributed/        Peer discovery, scheduling, worker management
│   ├── transport/          Stream adapters (S3, emitter, gate)
│   └── telemetry/          UDP event shipping, Elasticsearch integration
└── visualizer/             Three.js real-time substrate visualization
```

## Running

```bash
# Start a local worker node
go run main.go worker

# Start the visualization server
go run main.go viz

# Run the experiment/benchmark suite
go run main.go paper

# Start a mesh node for distributed execution
go run main.go mesh
```

The full distributed stack (Elasticsearch for telemetry, MinIO for object storage) can be brought up with:

```bash
docker-compose up
```

## License

This is a research project. License terms are pending.
