# NOTES

Yes, the Morton key packs 2D (byte_value × position) into 1D. In a pure 2D grid, you have 4 moves: ±X (change byte value), ±Y (change position).

But the real question is about the **257-bit value** you get for free at each key. Let's budget it:

**An "arrow" to the next byte costs 5 bits.** The next byte value's 5-bit hash, rotated to distinguish it from other data. That's 2% of the value.

So in 257 bits, you can store:

| What | Cost | Rotation slot | Remaining |
|---|---|---|---|
| Arrow → next byte | 5 bits | RollLeft(0) | 252 bits |
| Arrow ← prev byte | 5 bits | RollLeft(1) | 247 bits |
| Arrow → next chunk start | ~20 bits | RollLeft(2) | 227 bits |
| Arrow ← prev chunk start | ~20 bits | RollLeft(3) | 207 bits |
| Phase/boundary type | ~10 bits | RollLeft(4) | 197 bits |
| Flow-in context | ~20 bits | RollLeft(5) | 177 bits |
| Flow-out context | ~20 bits | RollLeft(6) | 157 bits |
| **Still free** | | | **157 bits** |

You're not limited to 4 directions. Each rotational slot is an independent "channel" in the value. At rotation 0, read the next-byte arrow. At rotation 1, read the prev-byte arrow. At rotation 2, read the next-chunk pointer. They don't interfere because the GF(257) rotations map to non-overlapping bit positions.

So each Morton key's value is a **tiny routing table**: it tells you where to go next, where you came from, what chunk this belongs to, what phase boundary it sits on — all packed into 257 bits with room to spare. The Morton keys give you the data. The value gives you the **graph** connecting the data.

And the graph edges are what you need for all five capabilities: follow arrows for generation, reverse arrows for reconstruction, XOR the flow channels for reasoning, match the phase channels for classification.

Does that match what you're imagining?

---

Yeah, but try this out for size: The cat sat on the mat

Now we load CIFAR10 training data:

        \     /\
          ) (  ')
        (  /   )
        \(__)|  [CAT]

Now we could reorder, or use some other technique to see that CAT the label, points to CAT the word and since CAT the label points to CAT the image (or really multiple image chunks) so we have just achieved cross-modality

---

TEXT path:    "The " → [cat] → " sat"
                         ↑
LABEL path:  [image_chunk_47] → [cat]  ← same trie node
                         ↓
IMAGE path:  [cat] → [image_chunk_1] → [image_chunk_2] → ...

The trie node [cat] — Morton keys 

(c,0)(a,1)(t,2)
 — is the SAME NODE whether you arrive from a sentence or from a CIFAR label. The meta value's rotational slots record:

Rotation 0: flow-in from text ("The "), flow-out to text (" sat")
Rotation 1: flow-in from image label, flow-out to image chunks (pixel data)
XOR the two flow-in contexts → the text context cancels the image context → what remains is the cross-modal residue: the relationship between what a cat looks like and how the word "cat" is used in language.

And it works in both directions:

Text → Image: "The cat sat on the mat" → [cat] trie node → follow rotation 1 flow-out → image chunks → reconstruct pixels
Image → Text: Feed image bytes → sequencer chunks them → trie paths match CIFAR data → flow arrows reach [cat] → follow rotation 0 flow-out → " sat on the mat"
No vision encoder. No text encoder. No alignment loss. No CLIP. Just one trie, one meta value format, one XOR cancellation mechanism. The label is the bridge between modalities and it happens automatically because the bytes c-a-t are the same bytes regardless of where they came from.

You want me to start building this? The Alice in Wonderland test would be the text-only proof. CIFAR-10 would be the cross-modal proof.

---

Yeah, but forget about the LSM, that is data storage, and later, translation.

We're now in the "graph" or the "substrate". This is where the values live, and this is why we will still need to find the right storage format, the prompt comes in: Picture of a cat. Now, let's hope that our sequencer is able to chunk intelligently enough to do some really rudimentary form of edge detection, and we match enough cat-like bytes, we pull those branches into the substrate, we then have a whole bunch of values, pointing to each other. And we likely have some values pointing at nothing. We need to find a way to have those arrows point to the storage location where what they want to point to lives, and pull that into the substrate (LOAD opcode). Now we build the AST, XOR until you can XOR no more, recursive fold. And now we are praying that after that, we somehow have enough structure to "naturally" walk to the label CAT

---

Now I see the layers clearly:

LSM = cold storage. Bytes. Morton keys. Trie branches. Disk.

Substrate = hot working memory. Values. Graph edges. XOR cancellation. The Fermat cube.

The pipeline:

Image bytes → Sequencer → Chunks → LSM lookup → LOAD matched branches → 
Substrate now has values → Some arrows dangle (point to unloaded data) →
LOAD those too → Build AST → XOR recursive fold → Walk residue → "CAT"
So the value format in the substrate needs to serve exactly ONE purpose: be an arrow that also carries enough identity to match or be matched.

Here's what I think works:

Each value in the substrate is a node in the graph. Its 257 bits encode:

Identity: who am I? (The hash of the chunk I represent — maybe 20-30 bits for first few bytes of the chunk, enough to match against the LSM)
Forward arrow: who comes after me? (15-20 bits — hash of next chunk's identity)
Backward arrow: who comes before me? (15-20 bits)
Dangling flag: if the arrow points to something NOT in the substrate, it's unresolved → triggers LOAD
Each at a different GF(257) rotation. Non-interfering.

The dangling arrows are the key insight: after the initial LOAD, you have a partial graph. You XOR-fold what you have. The residue contains arrows that point to things not yet loaded. Those arrows ARE the LOAD addresses — you use them to fetch more data from the LSM. Repeat until no more dangling arrows, or until the fold converges.

The recursive fold is: XOR cancels shared structure. What survives is the UNIQUE path — the one that connects your input (cat image chunks) to the output (the label "CAT"). If the sequencer did even crude edge detection on the image, some chunk byte patterns will match stored CIFAR chunks, and those stored chunks have arrows that eventually reach the label.

The prayer is that the arrows form a connected path from input to label. The more data you train on, the denser the graph, the more paths exist, the more likely cancellation leaves the right one standing.

Want me to write the value format and the LOAD/FOLD cycle as code? Or do you want to keep refining the design first?

---

The value needs to encode:

The arrows to what it wants to point to (could be multiple/many, and it may actually be better to always make it point to the LSM, so then it would be a 2D coordinate, or it is the morton key, then we may not even need the LSM and collision remains compression)

Potentially a weight (online learning?)

Phase Dial information (trust me, we can play Clue that way)

Maybe eigenmode

---

## Value Format: 4 Channels via GF(257) Rotation

The 257-bit value is divided into 4 rotational channels. Each channel occupies a band of rotational offsets, isolated by the prime field's permutation properties.

### Channel 0 — ARROWS (rotations 0–127)

Each arrow is a Morton key hashed to 5 bits via `(k*7, k*13, k*31, k*61, k*127) mod 257`. Multiple arrows OR together.

Sub-bands:
- **Roll(0..42)**: Forward successors — "what comes next"
- **Roll(43..85)**: Backward predecessors — "what came before"
- **Roll(86..127)**: Chunk-boundary links — "what chunk am I part of / adjacent to"

Room for ~8 forward + ~8 backward + ~8 cross-chunk arrows before any band hits 40%.

**Key property**: the arrow IS the Morton key. Two different contexts pointing to the same (byte, position) hash to the same 5 bits. Bits already set. No new storage. **Collision = compression.**

### Channel 1 — WEIGHT (rotations 128–169)

Online learning. Every time this (byte, position) is seen in a new context, set one more bit in this band. Count of active bits = traversal frequency. Max ~42 contexts before saturation.

Built-in popularity score. No external counter.

### Channel 2 — PHASE DIAL (rotations 170–211)

From the sequencer:
- Boundary type: chunk-start, chunk-end, mid-chunk
- Shannon density at this position
- Topological event (LAW tokens)

This is the "Clue" channel — same byte value, different phase dial = fundamentally different structural context.

### Channel 3 — EIGENMODE (rotations 212–256)

Resonance fingerprint. Which community/cluster this node belongs to.

Built over multiple passes. Could be XOR residue of all paths through this node. Nodes in the same eigenmode "resonate" — same topic/concept.

### Density Budget

```
Channel 0 (arrows):    ~40 bits max → 16% of value
Channel 1 (weight):    ~20 bits typical → 8%
Channel 2 (phase):     ~15 bits → 6%
Channel 3 (eigenmode): ~10 bits → 4%
──────────────────────────────────────────
Total typical:         ~85 bits → 33% density ✓ (under 40% ceiling)
Total saturated:       ~160 bits → 62% (only after heavy accumulation)
```

### Pipeline

```
Prompt: [image bytes of a cat]
         ↓
Sequencer chunks → Morton keys → Lookup → LOAD into substrate
         ↓
Substrate has values with arrows:
  image_chunk_17: arrows→[image_chunk_18, image_chunk_23, ...]
  image_chunk_23: arrows→[cat_label_chunk]
  cat_label_chunk: arrows→[text chunks "cat", more image chunks]
         ↓
Some arrows dangle (targets not loaded) → LOAD from storage
         ↓
Build AST: walk the arrow graph
XOR recursive fold: shared structure cancels
Residue: the UNIQUE path from input to answer
         ↓
Follow residue: image → ... → "CAT"
```

### Reconstruction test (Alice in Wonderland)

1. Load Alice → Sequencer → chunks → Morton keys
2. At each key, build value: hash next byte's Morton key as forward arrow, prev as backward, chunk boundaries, phase dial, weight++
3. On collision: same key exists → OR new arrows in → compression
4. Reconstruct: start at Morton(first_byte, 0) → read forward arrow → find matching Morton key → go there → repeat
5. On ambiguity (multiple forward arrows): use weight + phase dial to disambiguate
6. Measure: total unique keys × 41 bytes (8 key + 33 value) vs 151 KB raw text