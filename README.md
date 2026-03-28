<p align="center">
  <img src="docs/infographic.jpg" width="680" alt="Six Architecture Infographic" />
</p>

<h1 align="center">six</h1>

<p align="center">
  <strong>Physics-sympathetic, stream-composed computing substrate</strong>
</p>

---

> [!NOTE]
> This is a research project under active development. This README is written to match **what the repository actually does today** (packages, CLI, and key types). Earlier design notes that are not reflected in code—such as GF(65537) affine routing on `Region`—are not described here as current behavior.

---

## Core thesis

> **Can we reject gradient descent and backpropagation long enough to convince ourselves that we may not need them?**

Six explores a different shape of system: fixed-size **values** that carry data and **in-band programs**, flowing through **I/O-native** pipelines (`io.Reader` / `io.Writer`) into **multi-backend** execution (CPU, optional CUDA / Metal). The goal is to minimize heavyweight orchestration and treat the byte layout as the source of truth, so file handles, sockets, and kernels all speak the same physical frame.

---

## Repository layout

| Path | Role |
|------|------|
| `pkg/primitive` | `Value` fixed layout; `Region` concurrent mixer; global `Backend` hook used by the VM |
| `pkg/core` | Viper-driven **value layout config** (`config.yml` / embedded default), opcode names, `CompileFunc` (text → packed 32-bit words) |
| `pkg/compute` | `Backend`: registers CUDA, Metal, and CPU substrates; routes `UniversalBitwise` |
| `pkg/compute/kernel` | `Substrate` interface; CPU / CUDA / Metal implementations |
| `pkg/vm` | `Machine` stream pipeline, worker pool, `Region` fan-out |
| `pkg/network` | UDP, QUIC, IPC transports (tests and adapters) |
| `pkg/telemetry`, `pkg/transport` | Events, streaming, S3 adapter |
| `experiment/` | Task suites, reporters, datasets (e.g. Hugging Face), artifact/projector pipeline |
| `visualizer/` | HTTP / WebSocket UI for substrate telemetry |
| `cmd/` | Cobra CLI (`six`, `viz`, `paper`, `worker` stub) |

If you only want the core types:

```bash
go get -u github.com/theapemachine/six/pkg/primitive
```

The rest of the tree stays for **reproducible experiments**, visualization, and paper tooling.

---

## `Value`: fixed 1024-byte frame

In code, a `Value` is `type Value [128]uint64` — **128 little-endian words, 1024 bytes total** (`primitive.Words`, `primitive.ByteSize`). The exact field map is **driven by `pkg/core` config** (loaded from `$HOME/.six/config.yml` when present, else embedded `cmd/cfg/config.yml`). The file `pkg/primitive/value.go` documents the default intent:

- **Data / identity**: packed token region, `ValueID`, `PrevValueID`, `NextValueID`
- **State** (write engine): slot index, sequence index, XOR accumulator
- **Affinity**: bitmask for clustering (used by kernels for routing hints)
- **Link**: temporary grouping pointer
- **Gossip**: 256-bit routing signature (reserved for mesh-style use)
- **TTL**: hop budget (low 8 bits of the configured word)
- **Registers** and **PC**: VM-visible state
- **Program**: tail words hold **packed 32-bit instructions**; the instruction set is built from the **16 two-input boolean truth tables**, with skip / control flow patterns described in-package

`Value` implements **`io.Reader`** and **`io.Writer`**: reads emit one full frame (then `io.EOF`); writes participate in collision / sequence-aware path semantics documented in `value.go`. Serialization cost for streaming is avoided when both sides agree on the 1024-byte boundary—the layout **is** the on-wire form.

Assembly-like programs are compiled with `core.CompileFunc(src string) ([]uint32, error)`: lines are `src dst op` with binary opcode digits, registers like `r0`, `pc`, `fw`, and `*` for pointer/span forms (see `pkg/core/compile.go`).

---

## `Region`: concurrent channel mixer (not algebraic routing)

`primitive.Region` is an **`io.ReadWriteCloser`** that **buffers whole Values** on two Go channels:

- **Primary** buffer: capacity **64** frames (`NewRegion`)
- **Spill** buffer: up to **256** extra frames when the primary is full (`spillMaxFrames`)

**Writes** copy each incoming 1024-byte chunk into a private buffer, try the primary channel without blocking, then enqueue on **spill** if the primary is full—blocking until space exists so **no frames are dropped**.

**Reads** are **non-blocking** at the `io.Reader` level: if nothing is available, **`io.EOF`** is returned (not a blocking wait).

`SpillStats()` exposes queued spill depth plus lifetime spill enqueue count; the legacy “dropped” figure is always zero.

This is intentionally a **simple “stir the bucket”** mixing model, as described in `region.go`, so you can still compose pipelines with `io.Copy(regionB, regionA)` without inventing a separate messaging protocol—writers **block** when both queues are saturated instead of losing Values.

---

## `compute.Backend`: multi-substrate bitwise execution

`NewBackend` **probes hardware** and builds a slice of `kernel.Substrate` implementations in order:

1. One backend per **CUDA** device (`cuda.Available()`)
2. One per **Metal** device (`metal.Available()`)
3. A single **CPU** backend (always registered)

The primary entry point is **`UniversalBitwise(a, b unsafe.Pointer)`**, which dispatches to one substrate. **`pickSubstrate`** round-robins across devices, **except** when the host `Value` reports **`WANIngressScarred()`**—those frames are pinned to the **first** registered device to avoid bouncing across heterogeneous hardware.

Optional **`WithPool`** injects a `vm.Pool`; **`Schedule`** runs jobs on that pool or synchronously in tests when no pool is set.

On CLI startup (`cmd/root.go`), a **`compute.Backend`** is constructed and assigned to **`primitive.Backend`**, so library code that imports `primitive` can invoke the global substrate after init.

*Note:* comments in `backend.go` still mention overflowing into a local `Region`; the struct itself is a **substrate router**, not a container for `primitive.Region`. Region fan-out today lives primarily under **`vm.Machine`**.

---

## `vm.Machine` pipeline

`vm.NewMachine` builds a goroutine pool sized from `runtime.NumCPU()`, creates a **fixed set of `primitive.Region` instances** (currently 10), and can attach a **`transport.Stream`** and dataset (`io.ReadCloser`). It is the main orchestration path for the **visualizer** experiment loop: stream Values through regions and telemetry without ad hoc global state.

---

## Command-line tools

Configuration is loaded via **Viper**: try `$HOME/.six/config.yml`, then fall back to **embedded** `cmd/cfg/config.yml`. `core.LoadValueConfig()` must succeed for a consistent word layout.

| Command | Purpose |
|---------|---------|
| `six` | Root (no-op run). Persistent `--config` path. |
| `six viz` | HTTP / WebSocket **visualizer**; optional Hugging Face dataset driver; `--listen` to serve UI only and ingest UDP graph events |
| `six paper` | Stitch **LaTeX** fragments and figures from experiment artifacts into `paper/main.tex` (paths from config / `paper.dir`) |
| `six worker` | **Stub**: returns an error; distributed worker not yet ported to the current `Backend` |

---

## Building and module notes

- **Go**: see `go.mod` (currently **Go 1.26**).
- **Optional accelerators**: CUDA / Metal backends are included where the toolchain and tags allow; CPU is always available.
- **Local replace**: `go.mod` contains `replace github.com/tphakala/simd => ../simd`. Clone the sibling **`simd`** repo next to this one, or adjust/remove the replace for your layout.

---

## Research / experiments

The **`experiment/`** tree defines **tasks** (classification, text generation, scaling, phasedial, logic benchmarks, etc.), **artifact** writers, and **projectors** (charts, tables, LaTeX helpers). It is the main place for **measurable** behavior on top of the substrate. **`visualizer/`** complements this with a live view of graph/substrate activity.

---

## How the pieces compose (today)

1. **Ingress**: any `io.Reader` that yields 1024-byte aligned chunks can feed a **`Region`** or materialize **`primitive.NewValue(...)`** frames.
2. **Mixing / fan-out**: **`io.Copy`** between regions and **`vm.Machine`** region sets shuffle workloads without a separate message broker—at the cost of **bounded buffers and possible drops** under load.
3. **Execution**: **`primitive.Backend`** (when set) runs **kernel `UniversalBitwise`** on pairs of pointers into **`Value`** storage, using the substrate order described above.

This is **not** “zero serialization” in the abstract—any real network still needs framing—but for local pipelines the **Value** is a single opaque 1024-byte atom end-to-end.

---

## License / status

Treat behavior as **authoritative in tests and source** when it disagrees with older prose or diagrams. Contributions and questions are welcome while the design is still moving.
