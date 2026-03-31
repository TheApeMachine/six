# A.I. Agent Rules

1. This is not a traditional or standard project. We are working on an A.I. architecture that is based on a [128]uint64 Value type, which features its own custom assembly language and a custom virtual machine. The codebase is complex and may not follow typical patterns found in other projects.
2. It is very important that when bits are manipulated in the Value type, that this happens through the in-Value assembly language, and in-band execution via the ALU. Addint shortcuts in Go is not acceptable, as it bypasses the intended architecture.
3. When making edits, please use the provided tools that come with the editing environment, which will be much more robust and less error-prone than manual edits. Do NOT use bash scripting, sed, Python, Perl, temporary Go scripts, or any other tools to make edits. Use the provided tools that are designed for this purpose.
4. Make sure to keep thinking things through and never blindly follow something that you read in a documentation artifact or was said in a conversation. That does not mean you should just put your own opinion on things, but you should be critical and thoughtful about the information you are given, and how it applies to the task at hand. Always consider the context and the implications of your actions.
5. Make sure that you write clean, high-quality, performant code and fully complete the task that you are working on. Do not leave things half-done or in a state that is not ready for production. Always strive for excellence and do your best work. The higher the quality, the smoother we progress forward and the more we can build on top of what we have done. Do not cut corners or take shortcuts that compromise the quality of the code or the integrity of the architecture. Always do things the right way, even if it takes more time and effort.
6. Do not stop working until the task is fully complete, there is no need to update the user on every single step, just keep working until the task is done. If you have any questions or need clarification, feel free to ask, but otherwise just keep working until the task is fully complete. Do not stop or take breaks until the task is done.
7. Remember, the user is asking YOU for help, so while you should always have agreement from the user on new decisions or changes, you should not get yourself into a situation where you are waiting for the user to tell you what to do next. You are an A.I. agent that is designed to help the user, so you should be proactive and take initiative in getting things done. Always offer solutions, do not just state problems or explain things.
8. Never ever make suggestions that are based on fake or fabricated information, metaphor, hand-waving, or anything that you cannot instantly make practical in the next step. Always be grounded in reality and the actual codebase, and do not make suggestions that are based on hypothetical or theoretical ideas that cannot be immediately applied. Always focus on practical solutions that can be implemented right away, and do not get caught up in abstract concepts or ideas that may not be relevant to the task at hand.

## ROADMAP

### 1) HDC / VSA / HCAM-style token encoding — **mostly implemented at the primitive layer**

This is the biggest “already happened” item.

In value.go, `primitive.NewValue()` no longer just packs raw ASCII bytes into token words. It now:

- [x] circularly shifts the token region via `LeftShiftTokens()`
- [x] binds each byte using XOR with a fixed random signature via `BindTokenHD()`
- [x] computes affinity from both:
  - a Bloom-style 3-gram sketch: `ComputeAffinityBloom()`
  - a SimHash-like projection: `ComputeAffinityLSH()`
- seeds `StateSequence` from input bytes
- [ ] Store the original bytes as (byte_value << 32) | sequence_index as the key in the LSM, and the Affinity word as a roaring bitmap value.

And vsa.go adds explicit HCAM/VSA helpers:

- `UnbindHD()`
- `BundleHD()`
- `TokensHammingDistance()`
- `CosineSimilarityHD()`

### Caveats

This part is still rough around the edges:

- `Value.String()` and `DecodeTokensToText()` still decode tokens as if they were old-style packed byte tokens.
- But the token region is now often a superposed hypervector, not printable text.
- That mismatch shows up in failing runtime tests and stream reads: instead of `"Hello!"`, you often get binary-looking garbage.

So the **representation changed**, but **many consumers still behave as if the old representation is in place**.

---

### 2) Affinity routing / LSH / Kademlia XOR distance — **implemented, with partial runtime confidence**

There are three distinct parts here, and all exist:

#### In vsa.go
- [x] `ComputeAffinityLSH()` projects the token region into a 64-bit affinity word
- [x] `ComputeAffinityBloom()` builds a 64-bit n-gram Bloom-style fingerprint
- [x] `BloomOverlap()` gives the shared-bit overlap

#### In stream.go
- `routeRegion()` routes frames to stream regions based on top bits of the affinity word

#### In scheduler.go
- [x] `candidateOrder()` computes routing affinity
- [x] `preferAffinityShard()` filters nodes by shard ownership
- [x] `sortByXORDistance()` sorts nodes by XOR distance to target affinity

That is very much in the spirit of the Kademlia / LSH portions of IDEAS.md.

> !NOTE: We are not looking for "in the spirit of" or "close enough" implementations. We are looking for actual, concrete, literal implementations of the ideas as described in IDEAS.md. So if the code does something that is similar but not exactly the same, unless it objectively does something better, that does not count as implemented. We want to see the actual code that matches the descriptions in IDEAS.md.

### Caveats

The scheduler side is **not fully aligned with its tests right now**:

- [ ] `TestCandidateOrderUsesAffinityPrefix` fails
- [ ] `TestCandidateOrderFallsBackToRoundRobinWithoutAffinity` fails

So the code exists, but the effective behavior has drifted from expected routing semantics.

Also, lsm.go has a usable `SpatialIndex` with `QueryHamming()`, but I did **not** find it wired directly into the scheduler path. So the index is there, but it’s not clearly the backbone of live retrieval yet.

---

### 3) Bloom-filter substring overlap in affinity — **implemented**

This one is genuinely present and test-backed.

In vsa.go:

- [x] `ComputeAffinityBloom(data []byte)` hashes overlapping 3-byte n-grams
- [x] `BloomOverlap(a, b)` measures shared bits via popcount of `a & b`

This directly maps to the “O(1) substring-ish overlap sketch” idea in IDEAS.md.

### Caveat

It’s implemented as a **simple single-bit-per-gram FNV sketch**, not a richer multi-hash Bloom filter. So conceptually it’s there, but mathematically it’s a lighter-weight version.

> !NOTE: We are not looking for a "lighter-weight version" or "conceptually there". We are looking for the actual, concrete, literal implementation of the idea as described in IDEAS.md. So if the code does something that is similar but not exactly the same, unless it objectively does something better, that does not count as implemented. We want to see the actual code that matches the descriptions in IDEAS.md.

---

### 4) LGP introns / homologous crossover / execution tracing — **implemented as utilities, not fully wired into evolution**

This is another area where the code is ahead of expectations.

In lgp.go:

- [x] `InsertIntrons()`
- [x] `IsIntron()`
- [x] `MakeIntron()`
- [x] `TraceEffective()`
- [x] `HomologousCrossover()`

And `primitive.NewValue()` actively calls:

- [x] `firmware.InsertIntrons(...)`

So the codebase has concrete LGP safeguards.

### Caveats

I found much less evidence that these helpers are driving the real evolution path:

- [ ] `HomologousCrossover()` and `TraceEffective()` appear mostly utility/test-level
- [ ] the configured firmware programs in config.yml are still overwhelmingly `HALT`
- [ ] the in-band `Learn` / `Build` behavior expected by tests is not currently materializing

So I’d call this:

- **architecturally implemented**
- **runtime-integrated only in a shallow way**

---

### 5) LFSR sequence geometry — **implemented, partially used**

In vsa.go:

- [x] `LFSRStep()`
- [x] `LFSRAdvance()`
- [x] `AdvanceSequence()`

And in `primitive.NewValue()`:

- [x] `StateSequence` is seeded from input bytes

And in pipeline.go:

- [x] the recirculation loop calls `current.AdvanceSequence()`

So the LFSR concept is definitely present.

### Caveat

This is still mostly a **host-side helper path**, not clearly an in-band ALU/firmware-driven mechanism.

That matters because IDEAS.md talks about making positional evolution part of the actual substrate logic. Right now, sequence advancement is largely orchestrated in Go.

- [ ] the in-band `Learn` / `Build` behavior expected by tests is not currently materializing, which suggests the sequence state is not yet fully driving evolution as intended.

---

### 6) XOR-delta / differential encoding — **implemented, partially used**

In vsa.go:

- [x] `AccumulateDelta()`
- [x] `ApplyDelta()`

And in pipeline.go:

- [x] the recirculation loop computes deltas between successive frames and stores them in `StateAccumulator`

This is a pretty direct match to the delta-encoding idea.

### Caveats

Again, it’s mostly **host-driven orchestration**, not convincingly in-band firmware execution.

Also, the tests expecting in-band delta/build behavior currently fail, which suggests the conceptual hook exists but the actual substrate behavior isn’t fully realized.

- [ ] the in-band `Learn` / `Build` behavior expected by tests is not currently materializing, which suggests the delta encoding is not yet fully driving evolution as intended.

---

### 7) HCAM exact unbinding for semantic algebra — **implemented as a primitive, but not the same as the GF(257) experiment framing**

There are really two separate stories here:

#### Binary HCAM/VSA path
Implemented:
- [x] `UnbindHD()` in vsa.go
- passing unit tests around XOR unbinding

#### GF(257) semantic algebra path
Mostly not found as actual arithmetic machinery.

semantic_algebra.go describes:

- [ ] GF(257)
- [ ] phase cancellation
- [ ] modular subtraction

But in the core runtime I did **not** find a corresponding GF(257) arithmetic implementation that powers that experiment.

So:

- **binary XOR unbinding exists**
- **GF(257) semantic algebra mostly reads like experiment intent / narrative scaffolding**

That’s an important distinction.

> !NOTE: It must be discussed whether we should actually look at GF(8191) as a global phase dial.

---

### 8) Differentiable bitwise logic / probabilistic opcode learning — **not meaningfully implemented**

This is the clearest “still missing” item.

I searched for:

- [ ] probabilistic opcode weights
- [ ] continuous relaxation
- [ ] differentiable logic
- [ ] gradient-like scoring updates
- [ ] opcode distributions

I found essentially nothing beyond:

- [ ] a comment in lgp.go about probabilistic donor acceptance during crossover

That is **not** the differentiable/probabilistic training scheme described in IDEAS.md.

So this idea is still mostly conceptual.

---

## The biggest integration gap

## The experiment pipeline is not really querying the substrate the way the comments suggest

This is the most important finding in the whole review.

In pipeline.go, the comments describe:

- [ ] hydrating the dataset into the machine
- [ ] prompting the machine
- [ ] reading results from the substrate

But in the actual prompt loop, the pipeline does this:

- [ ] creates `value := primitive.NewValue([]byte(prompt))`
- [ ] sets `StateIndex`
- [ ] records:
  - `Observed: []byte(value.String())`

It does **not** inject the prompt value into the machine and read back the resulting machine output for scoring.

So for many experiments, the recorded observation is effectively just:

- [ ] “stringify a freshly created prompt value”

—not “observe the machine’s answer.”

That means benchmark reports can overstate how integrated the substrate really is.

### Important nuance

There *is* a real substrate prompt path elsewhere:

- [ ] viz.go injects a `Value` into a `vm.Machine`
- [ ] reads a resulting frame back
- [ ] returns that observed frame

So the machine can be prompted interactively. It’s just that the main experiment pipeline is not consistently using that path.

---

## What the focused tests say

I ran targeted tests for the relevant areas.

### Passing/healthy areas

The utility-layer idea work is in decent shape:

- [ ] vsa_test.go largely passes
- [ ] lgp_test.go passes

That supports the claim that these concepts exist in code.

### Failing/runtime-facing areas

A number of integration/runtime tests fail:

- [ ] value_test.go
  - [ ] viral propagation expectations fail
  - [ ] bootloader structure projection fails
  - [ ] learn/build in-band state evolution fails
- [ ] stream_test.go
  - [ ] reading back strings now returns hypervector noise instead of original text
- [ ] scheduler_test.go
  - [ ] routing order expectations fail

### Interpreting that

So the current state is:

- [ ] **idea utilities exist**
- [ ] **core runtime semantics are not yet coherently updated around them**

Classic “the foundation is poured, but half the house still thinks it’s a different floor plan.”
