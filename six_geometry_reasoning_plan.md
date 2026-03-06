# Getting the cubes to actually reason

This note is a concrete proposal for making the cube / chord / geodesic architecture do useful retrieval and multi-hop completion **without depending on raw bytes as the runtime oracle**.

## 1. Core diagnosis

The current design has three distinct jobs tangled together:

1. **Semantic payload**: the actual meaning carried by chords.
2. **Geometric path**: rotations, A5 state, winding, branch history.
3. **Readout oracle**: byte lookup / reverse lookup for text emission.

The geometry is good at job 2.
It is not yet carrying enough of job 1.
And job 3 still leaks into storage and generation.

If geometry is only a path label, then retrieval becomes fancy choreography.
If geometry is allowed to structure semantic payload, then it can become a real reasoning substrate.

## 2. The representation that makes sense here

### Chord
A `Chord` should remain the **atom**: a sparse, high-dimensional bundle of features.
It should *not* be treated as a byte alias at runtime.

### MacroCube
A `MacroCube` should be a **typed frame** with fixed semantics for its 27 cells.
The cells should not be arbitrary hash buckets.
Give the 3 axes explicit meaning:

- **X axis = role**: source / relation / target
- **Y axis = temporal-causal offset**: previous / current / next
- **Z axis = abstraction scale**: token / span / schema

That gives every block a stable meaning.
Examples:

- `(source,current,token)` = local anchor/entity evidence
- `(relation,current,token)` = predicate/action evidence
- `(target,current,token)` = local object/value evidence
- `(source,next,span)` = predicted next anchor
- `(relation,current,schema)` = abstracted relation pattern

Now rotations are not arbitrary; they become controlled changes of viewpoint.

### 5 intersecting cubes
Use the 5 cubes as **epistemic channels**, not symbol hashes:

- `C0` focus / active query
- `C1` retrieved support
- `C2` abstract schema
- `C3` alternative branch / counterfactual
- `C4` consequence / prediction

This is where the A5 permutations become useful.
A 3-cycle can move focus -> support -> prediction.
A double transposition can swap active and alternative hypotheses.
A 5-cycle can sweep repetitive low-information material out of focus.

## 3. What is broken right now

### A. Runtime still depends on byte identity
Current insertion routes by `byteVal % 5`.
That means cube choice is a byte oracle, not a geometric consequence.

Replace this with slot selection derived from:

- phase dynamics
- event type
- role heuristics from local context
- occupancy / saturation state

### B. `PrimeField` is a hot accumulator, not a memory bank
Keeping a single active manifold is fine for **working memory**, but not for corpus retrieval.
You need a separate **episodic manifold bank** of frozen completed spans.

Keep:

- one hot manifold for the live stream
- many frozen manifolds for retrieval
- a smaller schema bank from pooled abstractions

### C. Query generation is mostly empty
If generation builds a zero-ish `queryCtx` and only rotates it, scoring will prefer emptier candidates with compatible header state.
So the current geometry can drift into “least-populated matching shell” behavior.

The fix is simple and load-bearing:

**build the query from real prompt content before applying the next geometric step.**

## 4. How data should actually enter the cubes

The cleanest rule is:

> A completed boundary-delimited segment becomes one frozen manifold.

Where segments come from your sequencer events and phase boundaries.

### Injection rules
When a new chord arrives:

1. Determine whether it belongs to source / relation / target / qualifier / delimiter behavior from local phase and density deltas.
2. Write it into the corresponding role slice in `C0` at `(role, current, token)`.
3. Update span and schema layers through pooling.
4. Rotate only after the payload has entered a meaningful role-addressed cell.

This means geometry organizes already-typed evidence, instead of being forced to infer semantics from arbitrary diffusion.

### Initial role heuristic
You do not need perfect parsing to start.
Use a brutally practical heuristic:

- first stable density cluster after a reset -> source
- strongest phase inversion -> relation
- following trough / settling cluster -> target
- persistent low-variance material -> qualifier / context
- hard reset -> delimiter / new frame

That is enough to start producing typed manifolds from chords alone.

## 5. Retrieval should be hole-filling, not nearest-neighbor cosplay

You already have the right primitive: `ChordHole(target, existing)`.
That is the seed of reasoning.

A query manifold is a **partial frame** with some known cells and some missing cells.
Retrieval asks:

> Which stored manifold best fills the missing cells without contradicting the known cells?

### Score a candidate by four terms
For query `Q` and candidate `M`:

- **overlap**: evidence shared with known cells
- **fill**: how well M fills the missing cells of Q
- **contradiction**: bits M asserts in cells that conflict with Q
- **geodesic cost**: header / path mismatch penalty

A practical score is:

`score = a*overlap + b*fill - c*contradiction - d*geodesicCost`

Where:

- `overlap` uses `popcount(M & Q)` on observed cells
- `fill` uses `popcount(candidateFill & hole(Q))`
- `contradiction` uses support in forbidden or mutually exclusive channels
- `geodesicCost` comes from the LUT

### Two-stage retrieval
1. **Coarse pass** on pooled shadows (`core / semantic / detail`) for pruning
2. **Dense pass** on exact cells for fill scoring

That keeps GPU friendliness while making retrieval genuinely semantic.

## 6. Reasoning = repeated manifold completion

Once retrieval returns a good candidate manifold, reasoning is just controlled propagation.

### Single hop
Example query:

- source = `Paris`
- relation = `capital_of`
- target = missing

A stored manifold containing `(Paris, capital_of, France)` fills the target slot.

### Multi-hop
Reasoning hop = rotate the newly filled target into the next source slot and query again.

Example:

1. `(person, born_in, ?)` -> retrieve country
2. rotate target -> source
3. `(country, capital_of, ?)` -> retrieve capital

That is a reasoning path, not text regurgitation.

### Why the geometry helps
Because the hop itself is a path transform:

- move filled target -> next source
- move support frame -> evidence cube
- move predicted consequence -> consequence cube
- branch uncertain paths into alternative cube
- merge via convergence / pooling

Now the A5/O-group transitions are doing computational work.

## 7. You need explicit cleanup memory

Noisy distributed representations need cleanup.
Otherwise every superposition eventually becomes mushy semantic soup.

You need prototype banks for:

- entity-like chords
- relation-like chords
- qualifier-like chords
- schema manifolds

At hop time:

1. retrieve candidate fills
2. cleanup to nearest prototypes / stable manifolds
3. write cleaned result back into the active manifold

Without cleanup, multi-hop chains will drift.

## 8. OR-only accumulation is not enough for reasoning

`ChordOR` is monotonic. It can only add support.
Reasoning needs contradiction, exclusion, and negation.

So add one of these:

### Option A: dual-channel bits
For each slot, maintain:

- support bits
- veto bits

Then score with support minus veto overlap.

### Option B: reserve a cube for counterevidence
Use one cube as an inhibitory or contradiction channel.
This is cheaper architecturally if you want to preserve bitwise kernels.

### Option C: tiny signed counters in pooled shadows
Keep atomic storage binary, but allow pooled shadows to carry signed confidence.

Without one of these, the system can complete patterns but cannot really arbitrate them.

## 9. Fix `ChordBin` or EigenMode becomes a noisy oracle

If `ChordBin` is just XOR-folding to 256 bins, it is not reliably locality preserving.
That means your toroidal phase estimates are based on a collision-prone hash rather than a geometric neighborhood.

Replace with a locality-sensitive binning scheme:

- 8-bit SimHash over the 512-bit chord
- or 8 learned random hyperplanes
- or multi-table LSH if you can afford it

The goal is:

**similar chords should usually land in nearby bins in Hamming sense**

Then EigenMode phases become meaningful structural coordinates instead of hash turbulence.

## 10. Concrete implementation order

### Step 1 — stop the biggest bug
Build `queryCtx` from the last `K` prompt chords inserted into role-addressed slots before applying extrapolated events.

This alone will drastically improve retrieval quality.

### Step 2 — separate hot memory from frozen memory
Keep the current active manifold as working memory, but freeze completed segments into a `[]IcosahedralManifold` corpus bank.
Search that bank, not just the live scratchpad.

### Step 3 — replace byte-based routing
Delete byte-driven cube choice from insertion.
Route by role/time/scale semantics.

### Step 4 — add hole-based scoring
Use known vs missing slot masks.
Rank candidates by overlap + fill - contradiction - geodesic cost.

### Step 5 — add cleanup memory
Prototype bank for slot-level chords and pooled schema manifolds.

### Step 6 — add iterative hop loop
After each fill, rotate filled target into next source and retrieve again until:

- hole is closed
- entropy rises too much
- margin collapses
- cycle detected in winding / support IDs

## 11. Pseudocode skeleton

```go
type SlotMask struct {
    Observed [5][27]bool
    Missing  [5][27]bool
}

func BuildQuery(prompt []data.Chord, events []int) geometry.IcosahedralManifold {
    var q geometry.IcosahedralManifold

    recent := prompt
    if len(recent) > 12 {
        recent = recent[len(recent)-12:]
    }

    for i, ch := range recent {
        role := inferRoleFromLocalDynamics(i, ch, events)
        x, y, z := slotCoords(role, i)
        idx := x + 3*y + 9*z
        q.Cubes[0][idx] = data.ChordOR(&q.Cubes[0][idx], &ch)
    }

    applyEvents(&q, events)
    return q
}

func ScoreCandidate(q, m *geometry.IcosahedralManifold, mask SlotMask) int {
    overlap := 0
    fill := 0
    contradiction := 0

    for c := 0; c < 5; c++ {
        for b := 0; b < 27; b++ {
            if mask.Observed[c][b] {
                overlap += data.ChordSimilarity(&q.Cubes[c][b], &m.Cubes[c][b])
                contradiction += contradictionBits(&q.Cubes[c][b], &m.Cubes[c][b])
            }
            if mask.Missing[c][b] {
                fill += expectedFill(q, m, c, b)
            }
        }
    }

    geod := int(geometry.UnifiedGeodesicMatrix[q.Header.RotState()*60+m.Header.RotState()])
    return 3*overlap + 5*fill - 4*contradiction - geod
}

func Reason(query geometry.IcosahedralManifold, bank []geometry.IcosahedralManifold, maxHops int) []int {
    var path []int
    for hop := 0; hop < maxHops; hop++ {
        mask := deriveMask(query)
        bestIdx, bestScore := -1, -1<<30
        for i := range bank {
            s := ScoreCandidate(&query, &bank[i], mask)
            if s > bestScore {
                bestIdx, bestScore = i, s
            }
        }
        if bestIdx < 0 || !improvesHole(query, bank[bestIdx]) {
            break
        }
        integrateFill(&query, &bank[bestIdx], mask)
        query = rotateFilledTargetIntoNextSource(query)
        path = append(path, bestIdx)
    }
    return path
}
```

## 12. The shortest honest summary

To make the architecture work:

- stop using bytes to decide where meaning lives
- give every cell a stable semantic job
- store frozen manifold episodes, not just one hot manifold
- query with real semantic payload, not empty rotated shells
- retrieve by **hole filling**
- reason by **repeated fill -> rotate -> fill**
- add cleanup memory and contradiction channels

That is when the cubes stop being decorative mathematics and start acting like a weird little geometric cortex.
