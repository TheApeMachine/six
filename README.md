# Six

> This is a research project under active development. 
> Certain code architectural decisions are built for speed, not for comfort. 
> Proper systems engineering is considered deferred until the architecture stabilizes. 
> Feedback is highly appreciated, ideally focused on the architecture as an alternative research model for machine intelligence.

This research project started from a simple question: "Can we reject gradient descent and back-propagation long enough to convince ourselves that we may not need them?"

---

## Motivations

1. I can't afford a $20k+ GPU, so the only realistic option is to attempt to side-step that obstacle.
2. If you reject the 1847 origin of gradient-descent, at best the algorithm is over 70 years old, let's try something else.
3. I like to explore interesting problems, and this is by far the most interesting problem of our time.

---

## Architecture

Six has four layers. Each is useful on its own, but the interesting behavior emerges from their interaction.

```text
┌────────────────────────────────┐
│           The Field            │
│  Emergent eigenmodes project   │
│  top-down pressure onto tries  │
└──────────────┬─────────────────┘
               │ bias
┌──────────────▼─────────────────┐
│         Kadabra DHT            │
│  Affinity-routed mesh of tries │
│  Gossip propagates field state │
└──────────────┬─────────────────┘
               │ store / retrieve
┌──────────────▼────────────────┐
│          MarkovTrie           │
│  Adaptive probabilistic trie  │
│  Classification + generation  │
└──────────────┬────────────────┘
               │ data
┌──────────────▼────────────────┐
│           Values              │
│  1KB programmable tokens      │
│                               │
└───────────────────────────────┘
```

## Values: Programmable Data

The `Value` type comes from the idea that machine intelligence currently lacks its own distinct "language" and that, to me at least, it seems like a missed opportunity when we force machines to reason using human language. I believe that severely constrains a system, locking it in human-level semantics.

A Value is a `[128]uint64` — exactly 1KB — that serves simultaneously as data, program, and identity. It is the atom of computation in Six.

```text
┌───────────┬────────────┬────────────┬────────────┬────────────┬─────────────┬──────┬──────┬─────┬────────────┐
│  Tokens   │  Program   │  Signals   │  Context   │    Meta    │  Reserved   │ Prev │ Next │ ID  │  Affinity  │
│ 512 bits  │  512 bits  │  512 bits  │  512 bits  │  512 bits  │  6464 bits  │  64  │  64  │ 64  │  257 bits  │
│ words 0-7 │ words16-23 │ words24-31 │ words24-31 │ words24-31 │ words32-124 │  125 │  126 │ 127 │ words 8-15 │
└───────────┴────────────┴────────────┴────────────┴────────────┴─────────────┴──────┴──────┴─────┴────────────┘
```

- **Token region**: Raw input data, encoded as bits across 8 words.
- **Affinity region**: A 512-bit locality-sensitive hash (8 independent SimHash projections) that fingerprints the content. This determines where the Value lives in the network.
- **Program region**: 32-bit instruction slots that execute on the Value's own bits. When Values encounter each other, their programs run — no external interpreter needed.
- **Prev/Next**: Linked-list pointers for chaining Values into sequences and graphs.
- **ID**: 64-bit unique identifier.

**RULES**

- `Value` operates on itself. It uses its own `Token` region as data.
- `Values` that encounter each other potentially modify the way they compute.

### The ALU

The `UniversalBitwise` method is a linear sweep across the `Program` region, where the data in the `Token` region is split up, and then used to perform bitwise operations between two spans. Important to understand is that `Values` are not mutated during this process, the operations are done on copies of the data, and purely emit a `Signal` as the result of each operation, which is written to the `Signals` region.

Because alignment is important in this process the `Token` region and `Program` region are the same size, so the `A` part of the data can be held steady while the `B` part of the data is rotated. The "line number" of the `Program` acts as the "program counter" in this case. Rotations need to happen in steps of 8 positions at a time, given our data exists at byte-level granularity.

Once the `Value` comes out of the `ALU` those `Signals` are used to emit new `Values` which are linked via the `PrevID`, `NextID`, and `ValueID` regions.

The linear sweep is a deliberate limitation in favor of having a system that includes loops and branching, as it is highly sympathetic to the hardware, eliminating thread-divergion on the GPU, and enabling parallelism via SIMD on the CPU.

To recover the ability for loops and branching, a final instruction can be written (need to take some of the reserved region) to mark a `Value` for a loop (re)cycle, or branch traversal. When the `Value` comes out of the `ALU` and is marked as such, it is then (re)placed onto a priority `Queue` in the orchestrator and fed back into the `ALU` for another run.

## MarkovTrie: Learning Without Gradients

This component quite naturally and very quickly spun out of control. It started at a way to store `Values`.

MarkovTrie is a suffix trie that learns from every observation. No training epochs, no loss functions, no weight matrices. It sees data, it updates.

**Core loop:**

1. **Tokenize** input (character-level, word-level, or BPE).
2. **Walk** the trie, recording which paths exist and which don't.
3. **Compute surprisal** — how unexpected this input is given what the trie already knows.
4. **Learn** by inserting the sequence into the trie with a learning rate modulated by surprisal. Novel inputs are learned aggressively; familiar inputs reinforce gently.
5. **Decay** all counts by a factor per step, so stale knowledge fades and the trie tracks the current distribution.
6. **Prune** dead branches when their counts fall below threshold.

**Classification** uses Naive Bayes over interpolated n-gram probabilities. The trie maintains per-label counts at every node, and classification is a walk through the trie accumulating log-evidence per label.

**Generation** uses beam search with temperature-controlled sampling. The beam width, temperature, and generation length are not hyperparameters — they are derived from the trie's own adaptive state.

**Adaptive self-tuning** replaces every fixed constant with an online estimator:

| Parameter              | Signal                    | Mechanism                                              |
|------------------------|---------------------------|--------------------------------------------------------|
| Decay factor           | Surprisal EMA             | High surprisal = volatile domain = faster decay        |
| Learning rate          | Per-token surprisal       | Novel tokens learned more aggressively                 |
| Prune threshold        | Node growth rate          | Fast growth = prune harder to control memory           |
| Classification context | Classification entropy    | Confused classifier = widen context window             |
| Temperature            | Surprisal EMA             | Familiar domain = explore more; novel domain = exploit |
| Beam width             | Classification confidence | Low confidence = wider search                          |
| Interpolation weights  | Depth hit rate            | Tracks which n-gram depths are most predictive         |
| Episodic blend         | Episodic match quality    | Useful episodic memory = higher blend weight           |
| Unsupervised threshold | Classification accuracy   | Maturing label space = require more confidence         |

The unified entry point is `Predict(data) -> Prediction` — the caller passes data, the system returns a classification and continuations. Everything else is internal.

### Kadabra: Distributed Knowledge Routing

Kadabra is a Kademlia-style distributed hash table where the nodes are MarkovTries. It serves two purposes: **distributing knowledge** across tries based on content similarity, and **forming the substrate** from which the field emerges.

**Affinity-based routing**: When a Value is published, its 512-bit affinity fingerprint determines which trie stores it. Values with similar content cluster on the same node. This is not a design choice imposed from outside — it follows from the LSH property: similar inputs produce similar hashes, so they route to the same place. Each trie naturally specializes in a region of content space.

**Replication**: Each Value is stored on the `k` closest nodes by affinity distance (Hamming distance over the 512-bit affinity vectors). This provides both redundancy and the ability for multiple tries to learn from the same data.

**Adaptive peer selection**: Each routing bucket tracks peer quality over epochs. Every `EpochQueries` queries, the bucket scores its peers by latency, explores alternatives, and swaps in better candidates. This is the Kadabra algorithm — a multi-armed bandit at the routing layer.

**Zero-affinity rejection**: Values without a computed affinity fingerprint are invalid and rejected. Every Value must know what it is before entering the network.

### The Field: Emergent Attention

The field is the mechanism that binds isolated tries into a coherent system. It is not a data structure that nodes query — it is a force that acts on them.

**Gossip protocol**: At each epoch boundary, every node broadcasts a compact `FieldDigest` — its current surprisal, classification entropy, growth rate, and affinity vector — to all routing peers. The digest is small (a few floats and 8 uint64s) and propagation is automatic.

**Eigenmode detection**: When a node absorbs a digest, the field recomputes emergent eigenmodes — clusters of structurally aligned, phase-coherent tries. Structural alignment is measured by **affine coupling**: instead of raw Hamming distance, the field evaluates overlap across multiple affine rotations of the affinity vectors, capturing alignment at different scales. Phase coherence is measured by **surprisal velocity** — whether nodes are rising or falling in surprisal together.

**Top-down projection**: The dominant eigenmode — the cluster with the most collective energy — is what the system is "attending to" right now. The field projects asymmetric pressure onto each trie:

|              | Aligned (in dominant mode)               | Misaligned (outside dominant mode)         |
|--------------|------------------------------------------|--------------------------------------------|
| **Decay**    | Suppressed — knowledge retained longer   | Amplified — stale knowledge pruned faster  |
| **Learning** | Amplified — absorbs related input faster | Suppressed — doesn't waste effort on noise |

This is attention without an attention mechanism. No query-key-value matrices, no softmax — the field *is* the attention. Tries don't decide to check the field. The field decides which tries matter and modulates their behavior accordingly, exactly as a physical field acts on particles within it.

**Phase dynamics**: In-phase nodes (both absorbing novelty or both consolidating) amplify each other's field pressure. Anti-phase nodes (one learning while the other is stable) dampen each other — they're already complementing each other naturally, so the field doesn't interfere.

---

## Compute Substrate

Values execute their programs on a multi-substrate backend that automatically selects the best available hardware:

1. **CPU**: Universal bitwise executor with SIMD fast-paths. Processes Values in tiles of 64, detecting homogeneous instruction runs for vectorized execution. Supports all 16 boolean truth-table operations plus popcount, shifts, and addition.

2. **Apple Metal**: GPU compute shaders for macOS. Compiled from Metal Shading Language at build time.

3. **NVIDIA CUDA**: GPU kernels for NVIDIA hardware. Generated via cgo bindings.

The `compute.Backend` load-balancer probes available substrates at startup and routes work to whichever has the least in-flight depth and lowest exponential moving average service time. When all accelerators are saturated, work overflows to CPU.

---

## Network Transport

Six provides pluggable transport for distributing Values across machines:

- **QUIC**: Reliable, encrypted WAN transport with congestion control. Single bidirectional stream per connection carrying Value frames. Built on `golang.org/x/net/quic`.
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
    tokens:   { start: 0,   bits: 512 }
    affinity: { start: 8,   bits: 512 }
    program:  { start: 16,  bits: 512 }
    reserved: { start: 24,  bits: 6464 }
    prev:     { start: 125, bits: 64 }
    next:     { start: 126, bits: 64 }
    id:       { start: 127, bits: 64 }

kadabra:
  bits: 64
  bucketSize: 20
  replicationFactor: 3
  alpha: 3
  epochQueries: 100
```

Firmware programs (LGP assembly for Value program regions) are also defined in config, enabling runtime-configurable computation without recompilation.

---

## Infrastructure

The project includes a Docker Compose stack for observability and data management:

| Service                          | Purpose                               | Port       |
|----------------------------------|---------------------------------------|------------|
| Elasticsearch (3-node cluster)   | Log aggregation, experiment telemetry | 9200       |
| Elasticsearch ML nodes (3 nodes) | Machine-learning-capable ES nodes     | —          |
| Kibana                           | Log visualization and dashboards      | 5601       |
| MinIO                            | S3-compatible object storage          | 9000, 9001 |
| LakeFS                           | Data versioning over MinIO            | 8000       |

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

# CPU profile an experiment
make pprof EXP=Text_Classification
```

**Requirements**: Go 1.26+. Metal shader compilation requires macOS with Xcode. CUDA requires NVIDIA toolkit. Both are optional — the CPU backend is always available.

---

## Usage

```go
// Create a Kadabra node backed by a MarkovTrie
node := kadabra.NewKadabraNode(
    kadabra.NodeIDFromString("node-alpha"),
    kadabra.WithReplicationFactor(3),
)

// Create a Value (affinity + ID filled by NewValue), publish to the DHT
value, _ := primitive.NewValue([]byte("the cat sat on the mat"))
node.Publish(*value, "Sentence")
value.Close()

// The trie learns automatically. Query it:
prediction := node.Store.Predict("the cat sat")
fmt.Println(prediction.Label)         // "Sentence"
fmt.Println(prediction.Continuations) // [{Sequence: "on the mat" Score: 0.87} ...]
```

For distributed operation, connect nodes and let the field emerge:

```go
nodes := make([]*kadabra.KadabraNode, 10)
for i := range nodes {
    nodes[i] = kadabra.NewKadabraNode(kadabra.NodeID(i))
}

// Connect the mesh
for i := range nodes {
    for j := i + 1; j < len(nodes); j++ {
        kadabra.Connect(nodes[i], nodes[j], 1.0)
    }
}

// Publish data — Values route to affinity-similar tries automatically.
// The field emerges from gossip and biases learning across the network.
// No central coordinator. No global optimizer. Just local learning
// shaped by emergent collective dynamics.
```

---

## Project Structure

```text
six/
├── cmd/                    # CLI commands (root, init, paper)
│   └── cfg/config.yml      # Default configuration
├── pkg/
│   ├── primitive/           # Value type, VSA operations, affinity LSH
│   ├── compute/             # Multi-substrate load balancer
│   │   └── kernel/
│   │       ├── cpu/         # SIMD-optimized bitwise executor
│   │       ├── cuda/        # NVIDIA GPU kernels
│   │       └── metal/       # Apple Metal GPU shaders
│   ├── store/
│   │   ├── markovtrie/     # Adaptive probabilistic trie
│   │   └── kadabra/         # Kademlia DHT + field dynamics
│   ├── network/             # QUIC, UDP, IPC transports
│   ├── vm/                  # Machine orchestrator
│   ├── core/                # Configuration management
│   └── errnie/              # Structured logging + Elasticsearch
├── docker-compose.yml       # Elasticsearch, Kibana, MinIO, LakeFS
├── Makefile                 # Build targets
└── main.go                  # Entry point
```

---

## Status

Six is research software under active development. The core mechanisms work and are tested, but the system is not yet production-ready. Current focus areas:

- Field dynamics and eigenmode-driven attention
- Multi-node distributed learning across network boundaries
- GPU acceleration of Value program execution at scale
- Episodic memory integration with field pressure

---

## License

See repository for license details.
