# Behavior architecture

This document combines the substrate’s **algorithm families** (how Values and Fields behave) with the **meta-layer** (how communities observe themselves and choose which behavior to deploy). It complements `README.md`, which defines regions, ALU contracts, and named programs. **Firmware** (what each Value executes) is treated as **in-value and in-band** like every other payload—see **Firmware and programming** below (including **`pkg/core/numeric`: where shared math is allowed**). Implementation plans are ordered so later phases can assume earlier ones exist. **Clarifications: streams, cycles, and readouts** (under IO-native composition) records completion and stream semantics that are easy to misread—especially for host/async vs in-band gossip.

### How to use this document (implementers and automated assistants)

1. **Authority.** For **behavior, firmware install semantics, meta-layer, and autonomy**, treat **this file** as the contract. Do not replace its intent with shortcuts inferred from `README.md` or from existing Go names—use those other sources for detail, not for redefining what “programming” or “install” *means* here.

2. **Programming is not “the host memcpy’d a full `Value`.”** The primary story is **§Firmware and programming → Installing firmware on a Value**: a **programmer Value** (or equivalent in-band carrier) brings **program bits** through **frames and gossip**; **resident firmware** is **program words** (and related kernel words) in the substrate sense. **`Value.Write`** stages **S+C+G+P into Asset** for peer propagation—it is **not** the definition of “programming” the program region. Do not equate a **harness** that materializes bytes with the **architectural** description of who performs install (in-value, ALU on program words, programmer role).

3. **Refactor order: remove before you add.** Closing purity gaps means **deleting** host-side `Set` loops, duplicate schedulers, and post-install mutations—not **leading** with new helpers, wrappers, or `pkg/core/numeric` on runtime paths. Prefer **narrowing** call sites and **one** non-duplicative path over a growing “toolkit” API. **`pkg/core/numeric`** stays at boundaries (lowering, wire **encoding while building buffers**, oracles, field observation, telemetry)—see **`pkg/core/numeric`: where shared math is allowed**.

4. **Purity gaps vs §Firmware.** **Purity gaps** name **forbidden** patterns (e.g. per-word `Set` install) and **closure** requirements (native bytes, no second-pass scheduling in Go). They are **not** an invitation to rename “programming” as “full-frame decode in `Dispatch`.” Read **Purity gap §2** together with **Installing firmware on a Value** so implementation stays one mechanism, not two competing stories.

---

## Region roles (commitment vs hypothesis)

Keeping these roles explicit avoids mixing “candidate search state” with “settled structure.”

| Region         | Typical role across behaviors                                                                                                                                            |
|----------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| **Tokens**     | Ground text or ingested span; stable prefix for prompts; Morton slab for discrete content.                                                                               |
| **Context**    | Local **hypothesis** or beam candidate; safe to wipe from above when pruning search.                                                                                     |
| **Signals**    | **Evidence** from pairing and ALU (bitwise or PGA); drives emission and readouts.                                                                                        |
| **Gradient**   | Auxiliary execution lane (often paired with Context in geometric programs).                                                                                              |
| **Properties** | **Committed** labels, TTL, community metadata, reduced scalars (e.g. surprisal); room for **caller / return** bookkeeping (see below). Slower to overwrite than Context. |
| **Affinity**   | Routing fingerprint; community membership and nearest-neighbor readout for supervised classification.                                                                    |

---

## Firmware and programming: in-value and in-band like everything else

**Contract.** Changing what a Value **runs** is not a special host privilege. It is the **same class of event** as the rest of the substrate: **wire frames**, **`Value.Read` / `Value.Write`**, **`gossip.Conn`**, and the **ALU** on resident program words. There is **no** second pipeline where Go reaches into program regions ad hoc at runtime. Programming is **in-value** (the executable lives in the **program** region and related kernel words) and **in-band** (how new firmware reaches a Value is **frames and gossip**, not a side channel).

**DSL vs machine.** The **authoring DSL** in config exists **only** so humans can write firmware comfortably. It is **not** the on-wire language and **not** what executing Values parse at runtime. At **configuration load** (or equivalent ingest), the DSL is **parsed and lowered once** into **opaque, pre-compiled program bytes**—a **`Firmware`** object (or the same information under another name) that holds **ready-to-run** material keyed by program name. After load, the process deals only in **native layout**: the same representation the ALU and **`Value.Bytes`** already use. Runtime **`compute` dispatch** consumes **those artifacts**, not source text.

**Installing firmware on a Value.** Delivery follows the **same rules** as any payload: a carrier may be a **programmer Value**—full or partial wire frame with program bits set, **Properties** marking role (e.g. programmer / install), TTL and trust fields as documented—routed through **Fields** and **Conns**. **Programming**, in the substrate sense, is **populating the program region** (and related kernel words such as scheduling metadata) **in-band**—i.e. **not** a separate Go-only loop that writes program words with **`Value.Set`**. The **inbound path** where wire bytes become **resident program words** must be **one** mechanism consistent with **Firmware and programming** (see **Purity gaps** for what to remove); **`Value.Write`** remains the **Asset** staging path for peer state (S+C+G+P), not the general definition of install. **Addressability** (caller ID, target ID) applies when an install is delegated across the graph.

**Autonomy and future capability.** A system that **rewrites its own behavior** does not emit DSL strings. It emits **native program words** (or full frames) into Values—the same representation produced at config load. “Self-programming” is **more Values and frames**, not a text compiler on the hot path.

**Packages.** Parse, compile, and lowering from the authoring DSL belong at the **config / tooling** boundary. The **runtime** stack is **`primitive`**, **`mesh`**, **`gossip`**, **`compute` execution** on already-materialized bytes—**not** a runtime dependency on DSL source. Refactors that **eliminate host `Set`-based install and post-install scheduling** (see **Purity gaps**: native wire bytes, **`ApplyContinuation` deleted**) complete this picture in code—without expanding ad hoc Go surface area (see **How to use this document** above).

### `pkg/core/numeric`: where shared math is allowed

**Non-negotiable.** Anything that **can** be done **in-value and in-band** (firmware, full wire frames, `Value.Write` staging, ALU) **must** be. `pkg/core/numeric` is **never** an approved parallel implementation of substrate **behavior**—only boundaries and observation around it.

| Area | Role of `pkg/core/numeric` |
|------|----------------------------|
| **Semantics / contract** | **`geometry/`** holds **reference definitions** that compiler lowering and kernels must match: e.g. **`ScanZeroRun` / `ScanOneRun` / `RunLabel`** (longest-run Signals), **PGA / Clifford / rotation** helpers consistent with `primitive.Value` word layout, **prime-basis phases** (`primes`, **`PhaseDial`**). Backend tests treat these as **oracles**; Metal/CUDA/ALU paths must not drift. |
| **Observation (readout)** | **`geometry`** (e.g. **eigenmode** partitioning, **PhaseDial** encoding from member snapshots) supports **`mesh.Field.Cycle`**-style **aggregation over existing Values**—pure observation of population state, not a second path that **mutates** program words or bypasses **`Value.Write`** for peer staging. |
| **Config and wire construction** | **`hash`**, **`bitwise`**, **`cosine`**, **`fibonacci` / primes** infrastructure: lowering, fingerprints, and **encoding scalars into `[]byte`** (e.g. pressure carriers—**only** while building the buffer; see **Purity gaps**). |
| **Telemetry and policy glue** | **`adaptive/`**, **`probability/`**, **`learned/`**, and root helpers (**`softmax`**, **`argmax`**, **`Dynamic`**, **`SurprisalVelocityCouple`**, etc.) support **dashboards**, **thresholds**, and **logged router experiments** over **already-exposed** metrics. **Decisions that change substrate behavior** (which program runs, carrier parameters stamped into Properties) should still be expressed **in-band** when expressible; host numeric here is for **harness, visualization, and transitional hooks**, not a permanent duplicate of logic that belongs in firmware. |

**Summary.** Treat **`pkg/core/numeric`** as **shared math for lowering, wire encoding, test oracles, field observation, and off-substrate tooling**—not as optional runtime firmware written in Go.

---

## Addressability: caller identity and “return” without CALL/RETURN

### Intent

The execution model does **not** expose host-style **CALL** and **RETURN**. There is no implicit stack: when a `Value` or `Field` **emits** other Values to do work (sub-steps, learners, probes), there is no opcode that jumps back and hands a result to the invoker. **Addressability** is therefore mandatory: the system must know **who to report to**, and **how** result frames find that recipient across a **nested network** of `gossip.Conn` and `Field` links.

**Caller record.** When spawning task Values, record the **caller** in the child’s **Properties** region (now **1024 bits** in the default layout—enough for more than labels alone). At minimum this should include the **callee’s notion of “who invoked me”**: e.g. the **caller’s `ValueID`** (and whatever **type** or **role** tag you need so programs can branch—same substrate, different intended use). That is the stand-in for a **dynamic link** that a RETURN instruction would have resolved.

**Return path.** “Reporting back” is not a special CPU instruction; it is **delivery** of a wire frame (or staged S+C+G+P via `Value.Write`) such that the **Value whose ID matches the recorded caller** can eventually **observe** the outcome—in **Asset**, **Properties**, or merged Signals, depending on program rules. **Gossip** (`io` through nested Conns and Fields) is the transport: frames propagate until the **addressee** (the Value with the right ID in the population or at the end of a route) can absorb the payload as the **return** for that delegation. This is the same composable streaming story as in **IO-native composition**, with **identity** in Properties acting as the routing hint for “this frame completes task X for caller Y.”

**Relation to Prev / Next.** `Prev` / `Next` words still describe **segment / chain** linkage in the Morton and emission narratives (`README.md`). **Caller ID + type in Properties** describe **delegation** (“who spawned this task Value”), which is a different axis: many emitted workers can point at the same logical parent without being linear segments of one stream.

### Implementation plan

1. **Layout.** Reserve a fixed **Properties** sub-span (words or bit fields) for **caller ID**, optional **caller type / kind**, and optionally **correlation** (opaque handle) so multiple concurrent children do not collide. Document it in one place next to kernel label slots and TTL words.
2. **Mint path.** When minting delegated Values (programs, finalizers, or Field-emitted carriers), stamp caller fields before the frame enters gossip.
3. **Delivery.** Define when a “return” is a **full frame** aimed at a Field that holds the parent vs a **Write**-staged update; either way the consumer program must recognize **destination ID** (or accept by staged Asset rules already in use).
4. **Liveness.** Ensure nested Conns actually **forward** frames toward communities or members that host the target ID—otherwise addressability is only data with no path. This may overlap with affinity routing and trie lookup outside this doc’s scope; the contract here is **what** to stamp, not **every** routing policy.
5. **Visibility.** Extend telemetry / visualizer panels to show caller fields so debugging delegated flows does not require guessing from IDs alone.

---

## IO-native composition (nested gossip, streaming, completion)

### Intent

Peer coordination (especially **unsupervised** learners) can be expressed as **nested `io.ReadWriteCloser` graphs**: a local sub-swarm uses `gossip.NewConn(ctx, queue, v0, v1, v2, …)` so every participant shares one stream abstraction. The stack is **composable and recursively nestable**—`mesh.Field` and `gossip.Conn` already implement `Read`/`Write`; higher levels (e.g. `vm.Orchestrator`) can treat work as **streaming bytes** through nested endpoints instead of a bespoke control API.

**Inbound frames and Asset.** `pkg/primitive` keeps the surface small: **`Value.Write(p []byte)`** is the inbound path. It decodes a full `Value.Bytes` wire frame into a **temporary** Value, then copies that frame’s contiguous **Signals + Context + Gradient + Properties** into **this** Value’s **Asset** region. That one behavior is the io-native way a downstream participant gets the prior hop’s outbound regions in-band—no extra helpers on `Value`, no separate staging API.

A **chain** of learners is then: bundle order + repeated `Write` so each Value receives the frame from “before” it; the same field-level quantities you care about for metrics are exactly what land in Asset for the next program. Wiring (`gossip.Conn`, pool dispatch, `Field`) must preserve **order** and full frames; see `README.md` for how programs read staged peer state from Asset.

**Cycle vs stream.** Today `mesh.Field.Cycle` also recomputes crystallisation, eigenmodes, and `PhaseDial`—pure **observation** of the population (implemented with **`pkg/core/numeric/geometry`** for dial and eigenmode snapshots—see **`pkg/core/numeric`** under **Firmware and programming**). A fully **streaming** model would make the hot path `io.Copy` (or equivalent) through nested `Read`/`Write` endpoints; **aggregation** (field metrics, mode detection) can remain an explicit **side pass** or a **tee** on the same frames so “no dedicated Cycle” refers to **ingress/egress**, not necessarily dropping observability.

**Population storage.** Replacing `values []*primitive.Value` with `[]io.ReadWriteCloser` (or a small wrapper that carries both an `io.ReadWriteCloser` and optional `*Value` for metrics) makes the Field a pure **plumbing** node. Trade-off: anything that needs **direct** member access (`measureCrystallization`, routing fingerprints) must **type-assert**, hold a parallel slice, or receive snapshots from the stream—document the chosen pattern.

**Orchestrator completion.** The VM treats **task completion** as explicit **readout** after frames have drained through the nested IO graph for the current logical pass: inspect **ingress** Values, a designated sentinel, or the field population for **Properties** status (e.g. a **status** word or bit pattern, `RESOLVED` / belief gap), aligned with `Machine.Prompt`’s notion of “resolved” Values. **`io` does not implicitly finish a task**; the observer reads state once the pass is done—typically a **single-shot read** at quiescence, not a busy polling loop. Work that spans more than one pass stays **in-band** through successive cycles and gossip (see **Clarifications: streams, cycles, and readouts** below).

### Clarifications: streams, cycles, and readouts

These points exist so readers (including automated assistants) do not import **host/async** mental models that do not match this substrate.

1. **Single-shot read vs “polling.”** “Inspect after drain” means: after the chosen I/O barrier (e.g. drain after `Write`/`Read` for that pass, or `io.Copy`-equivalent when EOF semantics allow), **read** the relevant `Value` regions once. That is **not** “polling” in the sense of repeatedly checking until something external completes. Older shorthand “polling or single-shot read” was only meant to say: **completion is not a magic property of `io`**—something must **observe** Properties / population state. Prefer **single-shot read** unless a design document explicitly requires waiting across unrelated events.

2. **No EOF-vs-background split.** Avoid assuming **stream end** while “real” substrate work continues **out of band**, as if a separate completion channel or polling loop were required. Here, **nested gossip is the lifeline**: if a delegation is not finished, the path has not yet delivered the **return** frames so the terminal reader can see the right status. Long-lived **outer loops** (successive **cycles** or an effectively unbounded stream) are normal.

3. **Multi-cycle completion.** A later **cycle** may be the one where frames reach the **end** of the nested `gossip.Conn` / `Field` story with the correct **Properties** (or staged Asset) for the task. **Gossip** is what **eventually** brings that `Value` data to where the orchestrator or HUD reads it. Status is carried **in Values on the wire path**, not in a parallel hidden completion mechanism.

4. **For reviewers.** Do not infer that “orchestrator completion” implies **extra** Go control flow beyond: drive I/O to the contract’s barrier, then **read** Values; and do not treat **polling** as the default pattern for this doc.

### Implementation plan

1. **Prototype nested Conn.** Build a test-only `gossip.NewConn` over a **fixed** learner bundle and one upstream ring; assert round-robin `Read` and that each bundled Value sees correct **`Write`** staging into Asset when frames are delivered in order.
2. **Align ordering.** Audit the pipeline so inbound frames hit each bundle member in the intended sequence; fix any drift between `Conn`/`Field` comments and where bytes actually meet `Value.Write`.
3. **Field adapter.** Introduce an interface (e.g. `io.ReadWriteCloser` plus whatever you need for metrics) so `Orchestrator` can stream through `Field` without baking in more than necessary, while `Cycle`/observation stays available until you replace it with a tee or side pass.
4. **Status contract.** Reserve or document a **Properties** word/bit for **task status** (resolved / active / failed); wire `Orchestrator` to stop on the same condition documented for `Prompt`.
5. **Migration.** Keep `Cycle` as the compatibility path until metrics and eigenmode detection have a streaming or tee-based replacement; only then thin `Orchestrator` to pure IO loops.

### Purity gaps (three fixups): one mechanism each

These are **not** ingress/egress—they are places where Go still authors substrate bits **without** passing through the frame decoder, a peer **`Value.Write`** (where that is the correct staging contract), or the ALU. They violate **Firmware and programming** above until closed. Each item below has **exactly one** target contract: **no** parallel installers, **no** ranked alternatives, **no** leftover stubs.

#### 1. Pressure carriers (`mesh.Field.BuildPressureCarrier`)

**Today.** The helper mints a `*primitive.Value`, stamps a new ID, then uses **`Value.Set`** to write **fixed-point scalars** into **`Asset[0..2]`** (Coverage, Consensus, Crystallization) and sets **TTL** in Properties. The caller routes the carrier through **`gossip.Conn`** so peers observe the payload via **`Value.Write` → Asset staging**—transport is in-band; **encoding** is host-authored.

**Target.** The carrier **`Value` is populated only by decoding a complete wire frame:** after `FieldMetrics` is computed, build **`[]byte`** of length **`core.Cfg.Value.Bytes`** using the **same layout and encoder as `Value.Read` / `Value.Bytes`**, then **`primitive.ValueFromWireFrame`** constructs the carrier. Float-to-fixed-point conversion (and any auxiliary scalar math) is allowed **only** while building that buffer—**`pkg/core/numeric`** may be used **here** as encoding support, not as a post-decode mutation path; **`Value.Set` must not appear** on the carrier for metrics or Asset. Downstream peers are unchanged: **`Write`** remains the sole ingress to their state.

**Implementation.** One serializer, one layout table (metrics + TTL + ID rules) documented next to kernel TTL words; **`BuildPressureCarrier` uses only that path** and gossip injection; remove direct `Set` on Asset.

#### 2. Compiler install (remove per-word `Frame.WriteIntoProgramRegion`)

**Today.** The compiler emits **`[]Frame`**; **`Frame.WriteIntoProgramRegion`** copies **`frame.Program[]`** with **`Value.Set` per word**. **`compute.Backend`** loops frames, installs, then executes.

**Target (aligned with §Installing firmware on a Value).** **Forbidden:** host **`Set` per program word** and any **second** host mutation for scheduling after install. **Required:** lowered **native** bytes (full **`core.Cfg.Value.Bytes`** frames **or** an equivalently strict wire representation—**no** ad hoc partial mutation) are the **only** source for program words and for word **117** in the emitted install sequence; **`ApplyContinuation`**-style Go must disappear. **Clarification for implementers:** the **architecture** is still **programmer / program-region** semantics (see **Firmware and programming**), not “programming **means** `Dispatch` decodes a whole `Value`.” A **transitional** closure may use **one** in-place wire materialization of the resident `Value` to avoid **`ValueFromWireFrame`’s allocator** on the install hot path—that is a **mechanical** replacement for **`Set` loops**, not a new definition of substrate programming.

**Implementation.** Remove **`WriteIntoProgramRegion`** and **`ApplyContinuation`**; **`Dispatch`** drives **`Execute`** only from materialized native bytes that satisfy the **single** inbound contract; tests prove program words and scheduling metadata **do not** come from stray **`Set`** calls.

#### 3. Continuation / scheduling word — delete `ApplyContinuation`

**Today.** After each install, **`ApplyContinuation`** does **`Value.Set(kernel.SchedulingNextProgramWord, …)`** for **`next <id>`** / **`next self`** (`programmer.Parser`). **Post-exec** clears word **117** on TTL (`kernel` postexec). Scheduling is a **second** host mutation after install.

**Target.** **`next` and `next self` are lowered only in the compiler.** Each install buffer includes the correct **`SchedulingNextProgramWord` (117):** for **`next <id>`**, that **ValueID**; for **`next self`**, the **resident Value’s `ID()` at install time**. For multi-line programs, word **117** appears in the **last** frame of the ordered lowering sequence (same rule as today’s “continuation applies after the frame loop”). That word is **only** present in the wire bytes—written **only** when the frame is decoded into the `Value`, **never** by a follow-up Go call.

**Implementation.** **`ApplyContinuation` is deleted**—not renamed, not left as an empty function, not kept for tests. Remove the symbol and **all** call sites in **`compute.Backend`**. Extend the compiler output so word **117** is part of the emitted wire buffer; **`Dispatch`** does not set word **117** outside decode. Postexec (TTL clearing word **117**) is unchanged.

---

## Meta-layer: field metrics, autonomy, and algorithm selection

**Observation.** Each leaf `mesh.Field` already aggregates its population per `Cycle` (`FieldMetrics`: crystallisation, eigenmodes, pressure). Extending this with **scalar summaries** over **Signals, Context, Gradient, and Properties** (e.g. mean bit activity, optional XOR-fold dispersion per region) gives a compact **community state vector** without inspecting every Value on the hot path outside `Cycle`.

**Action.** The substrate exposes many **programs** and **finalizers** (`config.yml`, `README.md`). **Autonomy** here means a closed loop: field state (and optionally task outcomes) informs **which algorithm family** runs next (beam vs classify vs logic vs causal exploration), or parameters thereof (prune aggressiveness, how many ephemeral learners to mint).

**Learning.** Those same summaries can feed a **learnable policy** (e.g. contextual bandit or small parametric router) updated from **intrinsic** signals (crystallisation delta, coverage, dominance) and/or **extrinsic** task reward. **`pkg/core/numeric/adaptive`**, **`learned`**, and related helpers are appropriate for **smoothing, weights, and scoring** in that harness; **committed routing or program choice** should still be reflected **in-band** (Properties, carriers, firmware) when the substrate can carry it. Parameters can live per-community, in Properties on carriers, or in a small host-side table keyed by community ID, as long as the **choice** is logged for credit assignment.

---

## Global field: lifting community metrics to system-wide state

### Intent

**Community Fields** already occupy a middle layer: they **host** member `Values` and, on each `Cycle`, produce a **`FieldMetrics` snapshot** (crystallisation, eigenmodes, pressure, and—once extended—regional aggregates) that summarizes **that** community’s collective state. That is the field-level analogue of a Value’s own regional state: not the same data layout, but the same **role**—“what is this unit’s observable fingerprint right now?”

The **final lift** is to repeat the **same relationship one level up**:

| Level                                      | Members                               | Aggregate “observable”                                                |
|--------------------------------------------|---------------------------------------|-----------------------------------------------------------------------|
| Leaf **Field**                             | `Values`                              | `FieldMetrics` from the member population                             |
| **Global** (root / orchestrator) **Field** | **Community Fields** (child `Field`s) | **Global** or **rolled-up** metrics that summarize the **whole tree** |

So: **community Field : its Values** should mirror **global Field : its community Fields** in *behavior*: children contribute; the parent exposes a **single** view of the substrate suitable for **telemetry, autonomy, and algorithm selection at system scope**. That is where you see **global state**—not by scanning every Value in the process, but by **composing** the same metric family already defined at leaves, bottom-up through the hierarchy.

**Alignment with routing.** `mesh.Field` already maintains an **affinity XOR fold** that propagates upward (parent aggregate folds every inbound frame; children’s fingerprints participate in the parent’s routing story). Metrics rollup should be **conceptually consistent**: either use **associative** folds where it makes sense (sums, XOR of hashes, min/max), or **documented** reductions (weighted averages by member count, dominance of the largest community) where it does not. The important part is **one contract** for “what the root field reports,” not ad hoc one-off numbers per tier.

**Today vs target.** Routing parents currently short-circuit leaf-style `measureCrystallization` on themselves because they do not hold direct members—the meaningful numbers live on **children**. The **target** is that after child `Cycle`s complete, the **parent** computes a **rollup** `FieldMetrics` (or a parallel `GlobalFieldMetrics` struct) from child snapshots and stores it where `Metrics()` on the root returns the **entire system** view.

### Implementation plan

1. **Contract.** Define how each scalar in `FieldMetrics` (and future regional fields) **combines** across children: e.g. `MemberCount` as sum of child member counts; `Crystallization` as weighted blend or worst-case community; `DominantRatio` as max or population-weighted mean. Document the choice so dashboards are interpretable.
2. **Cycle ordering.** Rely on **recursive child `Cycle` first** (already the pattern), then run **parent rollup** so the root’s snapshot is always one consistent pass behind the same tick’s leaves.
3. **API.** Expose **root `Field.Metrics()`** (or `GlobalMetrics()`) as the **single** read for “whole substrate”; wire `pkg/vm` orchestrator and telemetry to that endpoint for the global HUD.
4. **Visualizer / ops.** One screen (or envelope) that shows **tree summary** (communities × their metrics) plus **rolled-up** headline numbers at the top—same data model, two views.
5. **Policy.** Allow **system-level** autonomy rules (e.g. spawn new community, throttle carriers) to key off **global** aggregates, while **community-level** rules keep using **local** `FieldMetrics`—same signals, different scope.

---

## 1. Beam search / swarm (generative search)

### Intent

Generative tasks (next token or span, image reconstruction, out-of-corpus generation) are cast as **hierarchical beam search**. A prompt defines the **stable beam** so far. Each `Value` proposes extensions using its **Token** region and stores its best candidate in **Context**. Child hypotheses roll up to **community Fields**, which implement a **beam-of-beams**: larger composites built from projected child beams. Values whose beams participate in the winning composite receive **more attention**; others receive a **top-down reset** (e.g. Context cleared) to escape local minima.

### Implementation plan

1. **Conventions.** Document and enforce in programs: Token = prefix + local extension proposal; Context = scored hypothesis; field-level scoring reads only agreed slots (avoid ad hoc word use).
2. **Projection.** Define how child field summaries surface to parents (existing `Field` tree + `Write` routing): either explicit carrier Values carrying compressed beam metadata in **Asset**, or metrics-only rollup first (lighter).
3. **Field-level beam graph.** Extend `mesh.Field` (or a dedicated helper) to track **which member Values** contributed to the current best composite (reference indices or IDs). This is the hook for reward and for “break beam” signals.
4. **Reset path.** Implement “break beam” as either: (a) a **finalizer** or small program that clears Context per target Value, or (b) orchestrator-assisted batch clear where ALU cannot yet do word-precise writes (same pattern as README’s label-injection constraint).
5. **Metrics.** Add regional aggregates to `FieldMetrics` (see meta-layer) so beam pruning pressure can be learned or scheduled (e.g. high Context dispersion triggers prune).
6. **Tasks.** Wire one **end-to-end** benchmark per modality (text span, reconstruction, OOC) sharing the same beam orchestration to avoid three divergent code paths.

---

## 2. Classification (supervised readout vs unsupervised structure)

### 2a Supervised classification

**Intent.** A prompt carries the query in **Tokens**; **Affinity** routes it to the right community. The system finds the nearest stored **Affinity** among members and reads the class from the **labels word** in **Properties** (see `kernel` classification slots and `measureCrystallization` in `pkg/mesh`).

### Implementation plan

1. **Routing.** Rely on existing community routing (`mesh.Field` + affinity seeds + Hamming budget); add tests that prompt Values land in the expected child field for known affinity fixtures.
2. **Readout.** Single path: unpack label slots from Properties word 0 (already used by crystallisation metrics); document the exact word/slot contract in one place consumed by VM and visualizer.
3. **Telemetry.** Expose `FieldMetrics` (coverage, consensus) for debugging “is this community label-ready?”

### 2b Unsupervised classification (ephemeral learners)

**Intent.** When no labels exist in the population, **Fields** deploy **ephemeral** Unsupervised Learning Values. They coordinate via **gossip.Conn** (peer frames staged into **Asset**, same substrate as `README.md` peer-similarity and `measure_field`), read/write allowed regions, and converge on discovered labels or structure in **Signals** / **Properties** (respecting ALU constraints for mutating others’ words).

### Implementation plan

1. **Trigger.** Define a clear predicate on field state (e.g. `LabeledCount == 0` and `MemberCount > threshold`, or low crystallisation with unlabeled members) that schedules learner deployment; avoid hard-coding only in Go: prefer a **carrier** or **program** trigger aligned with pressure carriers (`BuildPressureCarrier` pattern).
2. **Ephemeral lifecycle.** Mint Values with TTL in Properties; ensure they are **not** permanently appended to `field.values` if that would pollute crystallisation (same principle as pressure carriers in `field.go`).
3. **Gossip coordination.** Reuse `gossip.Conn` bundling for learner–learner and learner–member coordination; prefer a **nested local Conn** (see **IO-native composition**) for sub-swarms. Staging into Asset is **`Value.Write`** on each hop; document ordering and the Properties status contract for completion.
4. **Discovery write path.** Where the README allows, keep label discovery in **signals/properties on the learner**; where host intervention is required, isolate it behind one `Orchestrator` or `Backend` hook with explicit tests.
5. **Promotion.** Optional second phase: write **discovered** labels into durable members’ Properties once consensus stabilises (may require ALU/orchestrator support for clean slot writes).

---

## 3. Signals algorithm (pairwise structure, logic / bAbi)

### Intent

Pairwise comparison of **Token** regions produces **Signals** (longest zero-runs vs one-runs, merge vs cancel narratives in `README.md`). That is the substrate’s **relational** layer for logic-style tasks (e.g. bAbi): structure from agreement patterns, not a single softmax. **Pre-rotated frames** and compiler vs kernel responsibilities must stay documented so Boolean sweeps and PGA opcodes do not alias.

### Implementation plan

1. **Contract lock.** Keep **`pkg/core/numeric/geometry`** `ScanZeroRun` / `ScanOneRun` / `RunLabel` as the single decisive specification for “longest run wins”; add regression tests when changing ALU or compiler lowering so **executing Values** match this contract, not a divergent Go-only path.
2. **Emission chains.** Verify `Prev` / `Next` / `ValueID` linking for cancel vs merge paths matches the README examples; extend `pkg/vm` tests if orchestrator behavior changes.
3. **Firmware vs host.** Maintain the explicit split: programs in `programs:` for expressible sweeps; orchestrator only where the README’s ALU constraint applies. Re-audit when adding word-level writes.
4. **Benchmarks.** Keep bAbi (or equivalent logic) as the **contract test** for this family so rotation and signal semantics do not drift.

---

## 4. Causal modeling (“what if?”)

### Intent

**Causal** behavior is a **horizontal** capability: alternate interventions and counterfactual worlds over the **same** Value frame. It should integrate with beam search (counterfactual **continuations**), classification (labeling regimes or confounders), and Signals (consistency under intervention). Explicit structure and routing reduce the data burden that pure sequence models need for comparable reasoning.

### Implementation plan

1. **Representation.** Specify how an intervention is encoded (duplicate Value with altered Context/Tokens, or staged counterfactual peer in Asset) without forking the wire format.
2. **Orchestration.** Add a thin **causal** mode in the VM or field loop: fork beam, run same programs, compare Properties or merge signals (depends on task).
3. **Integration.** Link to beam metrics and field metrics so “explore intervention” is one policy action alongside prune and classify.
4. **Evaluation.** Beyond benchmarks, one **integrated** scenario (e.g. beam + causal fork) to prove behaviors compose.

---

## 5. Cross-cutting: metrics, policy, and documentation

### Implementation plan

1. **Implement regional aggregates** in `pkg/mesh` (`FieldMetrics` + `measureCrystallization` or fused pass over `snapshotValues()`), using `core.Cfg.Value.Region` word extents for Signals, Context, Gradient, Properties; optionally mask kernel-reserved Properties words. Use **`pkg/core/numeric`** (e.g. **bitwise** dispersion, **cosine**/**hash** where documented) **only** to compute **summaries from snapshots**—same rule as **`pkg/core/numeric`** under **Firmware and programming**.
2. **Expose** summaries via existing `Field.Metrics()` and telemetry/visualizer envelopes so policies and humans see the same numbers.
3. **Router (incremental).** Start with **rule-based** mode selection from thresholds; upgrade to **logged** decisions + bandit or small learned router once metrics are stable. Router math may use **`adaptive/`** and **`probability/`**; **actions** on the substrate remain frames and programs per this document.
4. **Keep `README.md` and this file aligned** when region layouts or program names change; prefer one table of “behavior → primary regions → entry program” to reduce drift.

---

## Summary

| Family                        | Core idea                                                                           | Depends on                                         |
|-------------------------------|-------------------------------------------------------------------------------------|----------------------------------------------------|
| Beam / swarm                  | Hierarchical search; Context = hypothesis; field composes and prunes                | Regional metrics, reset path, optional policy      |
| Classification (supervised)   | Affinity route + Properties readout                                                 | Routing, label slot contract                       |
| Classification (unsupervised) | Ephemeral learners + gossip coordination                                            | Carriers/TTL, staging, ALU constraints             |
| Signals / logic               | Pairwise token → Signals → structured emission                                      | Scan/run contract, emission tests                  |
| Causal                        | Counterfactuals over same substrate                                                 | Beam + representation of interventions             |
| Meta                          | Field summaries → algorithm selection → (optional) learning                         | `FieldMetrics`, logging, rewards                   |
| Addressability                | Caller ID/type in Properties; gossip delivers “return” frames to that ID            | Properties layout, nested `Conn` forwarding        |
| Global field                  | Root Field rolls up child community `FieldMetrics` → system-wide state              | Recursive `Cycle`, documented fold rules, root API |
| Firmware and programming      | Pre-compiled bytes at config load; install via frames / `Write` only; native not DSL at runtime | **Firmware and programming** section; **Purity gaps** |
| Purity gaps (three fixups)    | Wire-only carriers; **no `Set`-based** compiler install (native bytes + single inbound path); scheduling only in lowered bytes — **`ApplyContinuation` removed** | See **Purity gaps** and **How to use this document**    |
| `pkg/core/numeric`            | Shared math for **lowering**, **wire encoding**, **test oracles**, **field observation**, **telemetry**—**not** a substitute for in-value behavior | **`pkg/core/numeric`** under **Firmware and programming** |

Together, these define **autonomy** in this stack: the substrate **observes** aggregated regional and crystallisation state, **acts** by deploying the right algorithm family, and **learns** which actions improve intrinsic or extrinsic objectives, while the **Value** remains the native programmable reasoning token.

> !NOTE
> **Firmware and programming** is the anchor: execution and re-programming stay **in-value** and **in-band**; the authoring DSL is human-only and disappears at config load. Prefer removing Go from runtime paths over adding it—see **Purity gaps** for concrete removals. **`pkg/core/numeric`** is limited to **`pkg/core/numeric`: where shared math is allowed**—do not use it to host behavior that belongs in firmware; anything expressible in-band **must** be expressed **in-value** (programs, frames, ALU), not as a second Go implementation.