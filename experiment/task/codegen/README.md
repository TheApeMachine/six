# BVP Span Solver — Text Generation Experiment Suite

## Overview

This experiment tests whether a **Boundary Value Problem (BVP) span solver** can generate coherent text using only retrieval and voting — no learned parameters, no attention, no autoregressive token prediction.

The core hypothesis: instead of predicting tokens left-to-right (an Initial Value Problem), we solve for a **complete span** that satisfies boundary constraints simultaneously, using memory retrieval and iterative refinement.

## Architecture

```
[ known prefix ]  ?????  [ optional suffix / constraints ]
```

The unknown region is solved as a coherent block:

1. **Boundary encoding**: Compute `F_boundary = Encode(prefix)` via PhaseDial fingerprint
2. **Diverse retrieval**: Sweep PhaseDial through 6 angles (256/256 torus split), retrieve top-16 candidate spans
3. **Token voting**: For each output position, collect candidate tokens weighted by source span similarity. Select highest-voted.
4. **Iterative refinement**: Re-encode `prefix + candidate_span`, re-retrieve, re-vote. Repeat for 3 iterations or until convergence.

## Corpus

31 deterministic Python functions covering:
- Arithmetic (factorial, fibonacci, gcd, power, is_prime)
- List operations (reverse, find_max, find_min, contains, unique, flatten)
- String operations (reverse_string, is_palindrome, count_chars, capitalize_words)
- Sorting (bubble_sort, insertion_sort, selection_sort)
- Algorithms (binary_search, merge_sorted)
- Higher-order (map_list, filter_list, reduce_list)

Span memory: **386 spans** of length 8 tokens each, extracted as sliding windows.

## Tests

### Test 1: Core BVP Span Solver (Retrieve + Vote + Refine)

Five test prompts, each a function signature:

| Prompt | Generated | return? | :? | Unique | Relevance |
|--------|-----------|---------|-----|--------|-----------|
| `def factorial(n):` | `def = if result = [] for x` | ✗ | ✗ | 0.88 | 0.5933 |
| `def find_max(lst):` | `def find_max(lst): if not lst: == None return` | ✓ | ✓ | 1.00 | 0.5192 |
| `def is_palindrome(s):` | `for i in = if x return +` | ✓ | ✗ | 1.00 | 0.6299 |
| `def binary_search(lst, target):` | `def b): in = in for i =` | ✗ | ✓ | 0.75 | 0.7085 |
| `def filter_list(fn, lst):` | `def i for result = [] for x` | ✗ | ✗ | 0.88 | 0.6530 |

**Summary statistics**:
- Convergence: 0/5 (no solver converged within 3 iterations)
- Has `return`: 2/5 (40%)
- Has `:`: 2/5 (40%)
- Mean unique token ratio: 0.900
- Mean prefix relevance: 0.621

### Key Observations

1. **Retrieval works**: The top-1 retrieved span for each prompt is structurally correct. For `find_max`, the top candidate is exactly the right span (`def find_max(lst): if not lst: return None best`). For `filter_list`, the top candidate is `def filter_list(fn, lst): result = [] for x` — exactly the right code.

2. **Voting degrades quality**: The token voting step averages across structurally different candidates, producing "chimera" sequences that mix tokens from incompatible patterns. For example, `factorial` gets tokens from list operations (`result = []`) because those patterns are common across many candidates.

3. **No convergence**: The solver oscillates between different voting outcomes across iterations. The refinement loop changes the query fingerprint enough to retrieve different candidates, which produce different votes, preventing stabilization.

4. **Prefix relevance is high**: Output fingerprints are consistently similar to the prefix (mean 0.62), indicating the retrieval step is directionally correct — the problem is in the voting/merging step.

### Diagnosis: Why Voting Fails

The weighted voting treats each token position independently. But in code, tokens are **not independent** — `if` must be followed by a condition, `for` must introduce a variable, `return` must follow an expression. Position-independent voting destroys these sequential dependencies.

This is the core IVP vs BVP tension: the solver tries to satisfy position-level constraints independently, but the constraint is actually over the **entire span** as a unit.

**The fix**: instead of per-position token voting, rank **complete candidate spans** and select the best whole span (or blend structural features at the span level rather than the token level).

### Test 2: Span Ranking BVP (Whole-Span Selection)

The span ranking solver retrieves whole candidate spans and scores them as complete units against the boundary fingerprint. No token decomposition, no voting. Multi-length span memory (6/8/10/12 tokens, 1,228 total spans).

Score = sim(span_fp, boundary_fp) + prefix_overlap + structural_bonus

| Prompt | Winner Span | sim | total |
|--------|------------|-----|-------|
| `def factorial(n):` | `def factorial(n): if n <= 1: return` | **0.758** | 0.818 |
| `def find_max(lst):` | `def find_max(lst): if not lst: return` | **0.698** | 0.753 |
| `def is_palindrome(s):` | `def is_palindrome(s): return s == s[::-1]` | **0.726** | 0.781 |
| `def binary_search(lst, target):` | `def binary_search(lst, target): low, high =` | **0.852** | 0.917 |
| `def filter_list(fn, lst):` | `def filter_list(fn, lst): result = []` | **0.824** | 0.889 |

**Summary statistics (Test 2)**:
- **Has colon: 5/5 (100%)** — every winner is syntactically valid
- Has `return`: 2/5 (40%) — only functions with early returns
- **Mean winner similarity: 0.774** — vs Test 1's 0.621
- Top-1 is the **correct corpus span** for all 5 prompts

**Comparison: Test 1 vs Test 2**:

| Metric | Test 1 (Token Voting) | Test 2 (Span Ranking) |
|--------|----------------------|----------------------|
| Correct winners | 0/5 | **5/5** |
| Has colon | 2/5 | **5/5** |
| Mean similarity | 0.621 | **0.774** |
| Convergence | 0/5 | N/A (single pass) |
| Chimera outputs | Yes | **None** |

**Key finding**: When the unit of reasoning is changed from tokens to spans, the system produces **structurally correct code** on every prompt. The retrieval was always good — the assembly operator was the bottleneck. Whole-span selection eliminates chimeras entirely.

**Top-10 analysis**: For `binary_search` and `filter_list`, the top-4 candidates are progressively longer versions of the correct implementation (6→8→10→12 tokens), showing the PhaseDial fingerprint correctly organizes spans by structural similarity. This means **multi-span concatenation** (selecting consecutive spans) could produce arbitrarily long code.

### Test 3: Span Chaining (Multi-Span Generation)

Iteratively retrieves and emits whole spans, using accumulated output as the new query boundary for the next retrieval. Produces up to 4 spans per prompt.

| Prompt | Steps | return | loop | Valid | Sources | Single-src |
|--------|-------|--------|------|-------|---------|------------|
| `factorial(n)` | 4 | ✓ | ✗ | ✓ | 2 | ✗ |
| `find_max(lst)` | 4 | ✓ | ✗ | ✓ | 2 | ✗ |
| `is_palindrome(s)` | 4 | ✓ | ✓ | ✓ | 3 | ✗ |
| `binary_search(lst, target)` | 4 | ✓ | ✗ | ✓ | 1 | ✓ |
| `filter_list(fn, lst)` | 4 | ✗ | ✓ | ✗ | 1 | ✓ |

**Summary**: Valid: 4/5, return: 4/5, loop: 2/5, single-source: 2/5.

**Key findings**:

1. **Structural progression works within a function**: For `binary_search`, all 4 steps retrieve progressively longer parts of the same function (single-source=true). Steps 1–3 are `def binary_search`, the search setup, and the full init. Step 4 retrieves `high = mid - 1 return` — actual body logic from the same function.

2. **Prefix duplication is the main failure mode**: The chainer re-retrieves spans that start with the same `def` signature because the context window still strongly matches signature-starting spans. The full output contains repeated `def binary_search(lst, target):` prefixes concatenated.

3. **Cross-function interference**: For `find_max`, the chainer alternates between `find_max` and `find_min` spans — structurally correct analogs, but mixing sources produces incoherent output.

4. **Similarity decay varies**: `binary_search` holds high similarity across 4 steps (0.917 → 0.722 → 0.546 → 0.333), while `is_palindrome` drops sharply at step 2 (0.781 → 0.363) because the one-liner function is already complete.

**Next step**: The chaining mechanism needs to be aware that spans overlap — when span₂ starts with the same text as span₁, it should detect and deduplicate the overlap rather than concatenating blindly. This is the prefix-deduplication problem.

### Test 4: Overlap-Aware Span Chaining

Three assembly fixes applied:
1. **Overlap-aware concatenation**: finds longest suffix-prefix overlap and appends only new tokens
2. **Minimum progress**: rejects candidates adding < 2 new tokens
3. **Name lock**: after step 1, rejects candidates defining a different function

| Prompt | Steps | New tokens | return | loop | Valid | Single-src |
|--------|-------|-----------|--------|------|-------|------------|
| `factorial(n)` | 2 | 6 | ✓ | ✗ | ✓ | ✓ |
| `find_max(lst)` | 6 | 28 | ✓ | ✓ | ✓ | ✗ |
| `is_palindrome(s)` | 2 | 10 | ✓ | ✗ | ✓ | ✗ |
| `binary_search(lst, target)` | 5 | 20 | ✓ | ✓ | ✓ | ✓ |
| `filter_list(fn, lst)` | 6 | 24 | ✓ | ✓ | ✓ | ✗ |

**Summary**: Valid: **5/5** (was 4/5), return: **5/5** (was 4/5), loop: 3/5, single-source: 2/5, mean new tokens: 17.6.

**Generated code highlights**:

```
def factorial(n): if n <= 1: return 1
```
✅ Correct! 2 steps, 6 new tokens, single-source. Stops on `return`.

```
def binary_search(lst, target): low, high = 0, len(lst) - 1 while low = mid + 1 else: high = mid - 1 return
```
✅ Structurally correct binary search body across 5 steps, all from the same source function. Similarity holds: 0.867 → 0.896 → **0.976** → 0.929 → 0.431 (body jump).

**Comparison: Test 3 vs Test 4**:

| Metric | Test 3 (Naive) | Test 4 (Overlap-Aware) |
|--------|---------------|----------------------|
| Valid | 4/5 | **5/5** |
| Has return | 4/5 | **5/5** |
| Prefix duplication | **Yes** | **Eliminated** |
| Mean steps to return | N/A (no stop) | 3.4 |
| Similarity **increases** mid-chain | No | **Yes** (0.87→0.98) |

**Key insight**: With overlap stripping, similarity *increases* during mid-chain steps (0.867 → 0.976 for binary_search steps 1→3). This is because each step extends the output to match longer stored spans, climbing the structural gradient rather than re-covering it.

### Test 5: Long Program Generation

Extended corpus (39 functions) with longer algorithms: quicksort, merge sort, DFS, BFS, RLE encoding, two sum. Span lengths up to 16 tokens, chain limit 12 steps.

| Prompt | Steps | Tokens | return | loop | if | Valid |
|--------|-------|--------|--------|------|----|-------|
| `quicksort(lst)` | 2 | 8 | ✓ | ✗ | ✓ | ✓ |
| `merge_sort(lst)` | 2 | 8 | ✓ | ✗ | ✓ | ✓ |
| `dfs(graph, start)` | 6 | 32 | ✓ | ✓ | ✓ | ✓ |
| `bfs(graph, start)` | 6 | 32 | ✓ | ✓ | ✗ | ✓ |
| `bubble_sort(lst)` | 6 | 22 | ✓ | ✓ | ✓ | ✓ |
| `rle_encode(s)` | 7 | 36 | ✓ | ✓ | ✓ | ✓ |
| `two_sum(nums, target)` | 6 | 32 | ✓ | ✓ | ✓ | ✓ |

**Summary**: Valid: **7/7**, return: **7/7**, loop: 5/7, mean tokens: 24.3.

**Key findings**:

1. **The structural gradient is real**: DFS/BFS both show the same pattern — steps 1–5 climb similarity (0.77→0.97) walking through `visited = set()`, `stack/queue = [start]`, `result = []`, `while stack/queue: node =`. Each step adds 2–4 tokens of genuine forward progress.

2. **The body jump problem persists**: At step 6, similarity drops to 0.30–0.44, and the system pulls in body fragments from *different functions*. DFS step 6 retrieves `find_min` code. BFS step 6 retrieves `fibonacci` code. This is the **structural analog attractor** problem — once the header manifold is exhausted, the system jumps to the nearest structural analog rather than the correct function body.

3. **Short functions terminate too early**: Quicksort and merge_sort both stop after just 2 steps (8 tokens) because `return lst` is the base case, triggering the early-return stop. The recursive body is never generated.

4. **Two_sum is the best long result**: 5 correct header steps from the same source, then a body jump that coincidentally retrieves binary-search-like `if lst[mid] == target: return mid` — structurally similar hash lookup pattern.

**Architecture implication**: The chainer walks the header gradient perfectly but needs a **phase-dial re-orientation** between header and body regions to bridge the manifold transition.

### Test 6: Out-of-Corpus Compositional Generation (Zero Heuristics)

**Critical test**: all hand-crafted heuristics removed (name lock, structural bonuses). Pure fingerprint similarity only. All 7 prompts are functions **not present in the corpus**.

| Prompt | Steps | Tokens | return | loop | Exp. overlap | Source fns |
|--------|-------|--------|--------|------|-------------|------------|
| `is_even(n)` | 2 | 6 | ✓ | ✗ | 0.25 | is_prime |
| `square(x)` | 2 | 6 | ✓ | ✗ | 0.25 | power |
| `product_list(lst)` | 9 | 54 | ✓ | ✓ | 0.25 | min_val, count_chars |
| `has_duplicates(lst)` | 10 | 88 | ✗ | ✓ | 0.06 | merge_sorted, remove_duplicates |
| `clamp(x, lo, hi)` | 7+ | 60+ | ✗ | ✓ | 0.00 | reverse_list, filter_list, transpose |
| `second_largest(lst)` | 10 | 92 | ✗ | ✓ | 0.00 | selection_sort |
| `mean(lst)` | 10 | 78 | ✗ | ✓ | 0.00 | quicksort, flatten, reduce_list |

**Summary**: return: 3/7, loop: 5/7, mean expected overlap: **0.104**.

### What this proves about the encoding

**The encoding captures structural token co-occurrence, not semantic function-level similarity.**

1. **`is_even(n)`** retrieves from `is_prime(n)` — the fingerprint matches the `def is_X(n):` structural pattern. But the retrieved body is `is_prime`'s body (`if n < 2: return False`), not an even-check. The encoding recognizes "function taking n that returns bool early" but can't distinguish *what* the function checks.

2. **Without name-lock, cross-function contamination dominates**: `product_list` pulls code from `count_chars(s)` because they share the pattern `result = loop; accumulate; return`. The encoding correctly matches the structural template but brings wrong identifiers.

3. **Overlap-aware assembly breaks down without name lock**: overlap detection fails when spans come from unrelated functions (overlap = 0 at most steps). The system concatenates fragments rather than extending coherently.

4. **The encoding is a byte-level structural matcher, not a semantic one**: it groups code by syntactic shape (loop-accumulate-return, conditional-return-early, etc.) but doesn't distinguish what the loops *compute*.

### Honest assessment

| What works | What doesn't |
|------------|-------------|
| Structural pattern retrieval (loop/conditional/return shapes) | Semantic function-level matching |
| Exact-match generation (Tests 1–5) | Out-of-corpus composition |
| Overlap-aware assembly with name lock | Assembly without heuristic scaffolding |
| Gradient ascent within known implementations | Bridging to novel implementations |

**The previous tests (1–5) were primarily demonstrating exact lookup with gradient walking, not compositional generation.** The heuristics (name lock, structural bonus) were compensating for the encoding's inability to distinguish which function a span belongs to at the semantic level.

**This is not a failure of the architecture** — it's a precise characterization of what the PhaseDial byte-level encoding captures. The question is whether richer encodings (token-level, AST-level, or learned embeddings) would provide the semantic discrimination that byte-level fingerprints lack.

### Test 7: Structural Sensitivity Probe

**Critical confirmation**: For each function prefix, encodes prefix + comment (`#`), prefix + noise (`import`), and prefix + correct continuation, then measures similarity to the full implementation, vector displacement magnitude, and directional alignment.

| Function | Sim→Full (comment) | Sim→Full (noise) | Sim→Full (correct) | Dir→Full (comment) | Dir→Full (noise) | Dir→Full (correct) |
|----------|-------|-------|--------|--------|-------|--------|
| factorial | 0.514 | 0.475 | **0.662** | 0.191 | 0.238 | **0.572** |
| binary_search | 0.552 | 0.503 | **0.648** | 0.199 | 0.155 | **0.497** |
| filter_list | 0.508 | 0.440 | **0.628** | 0.098 | 0.084 | **0.487** |
| find_max | 0.412 | 0.395 | **0.543** | 0.162 | 0.235 | **0.494** |
| dfs | 0.505 | 0.397 | **0.645** | 0.217 | 0.090 | **0.571** |

**Results: 5/5 structure-sensitive on every metric.**

1. **Correct continuation always has highest sim→full** (5/5)
2. **Correct continuation always points most toward full function** (5/5)
3. **Comment always moves vector least** (5/5)

**What this proves**: The encoding is **genuinely structure-sensitive, not token-count-sensitive**.

Adding a comment (`#`) barely moves the vector (Δdist ≈ 0.28). Adding noise (`import`) moves it more (Δdist ≈ 0.52) but in the **wrong direction** (dir→full ≈ 0.16). Adding the correct continuation moves it the most (Δdist ≈ 0.64) and in the **right direction** (dir→full ≈ 0.52).

The encoding understands that `def factorial(n): if n <= 1:` is more like `def factorial(n): if n <= 1: return 1 return n * factorial(n-1)` than `def factorial(n): import` or `def factorial(n): #`. That's structural awareness at the byte level.

**Reconciling with Test 6**: The encoding *is* structure-sensitive within a function — it correctly discriminates correct vs. incorrect continuations. What it lacks is **cross-function semantic discrimination** — it can't tell that `is_even` should retrieve different body patterns than `is_prime`. The structural sensitivity operates at the span level, not the function-identity level.

### Test 8: Eigenmode Probe (Transition Matrix Eigendecomposition)

Inspired by the old architecture's EigenInit, this test builds an **asymmetric forward transition matrix** T[i][j] at each FibWindow scale (who precedes whom), extracts the (v2, v3) eigenplane via gonum Eigen, maps each byte to a phase angle, and computes circular mean eigenphase per span.

**Key token eigenphases** (multi-scale circular mean):

| Token | Phase | Degrees |
|-------|-------|---------|
| `def` | -0.824 | -47° |
| `return` | -0.640 | -37° |
| `for` | -0.163 | -9° |
| `while` | +2.671 | +153° |
| `if` | -2.144 | -123° |
| `:` | -0.501 | -29° |
| `=` | +2.488 | +143° |
| `space` | +0.473 | +27° |

**Role eigenphase statistics** (2,882 spans):

| Role | Count | Phase μ | Degrees | Circ. σ |
|------|-------|---------|---------|---------|
| header | 166 | -0.480 | -28° | 0.316 |
| loop | 914 | +0.008 | +0° | 0.620 |
| conditional | 321 | -0.262 | -15° | 0.978 |
| return | 504 | -0.460 | -26° | 0.370 |
| assignment | 619 | -0.519 | -30° | 0.953 |
| call | 153 | -0.416 | -24° | 0.717 |

**Well-separated pairs: 1/15** — specifically **header ↔ loop (ratio = 1.04, Δφ = 28°)**.

### What this reveals

1. **The transition matrix captures the header/body manifold boundary.** Header spans cluster at -28° and loop spans cluster at +0° — separated by 28° with a ratio just above 1.0. This is exactly the manifold boundary that causes the similarity cliff in Tests 4–5.

2. **Header and return overlap almost perfectly** (Δφ = 1.2°, ratio = 0.06). This makes sense: `return` tokens share transition context with function signatures (`def ... return`).

3. **Loop stands apart from everything.** It's the only role with a positive mean phase (+0.5°), while all other roles cluster in the -24° to -30° range. Loop bodies live in genuinely different transition space.

4. **Assignment and conditional have high circular variance** (σ ≈ 0.95). They're spread across the full phase circle because `=` and `if` appear in many structural contexts.

### Compared to the previous PCA approach

| Method | Well-separated | Key finding |
|--------|---------------|-------------|
| PCA over fingerprints | 0/15 | No separation at all |
| **Transition matrix eigenplane** | **1/15** | **Header ↔ loop separates** |

The transition matrix approach finds the signal that PCA completely missed. The header/body boundary is real and measurable in eigenphase space. But most other role pairs remain mixed — the byte-level transitions don't separate `return` from `header` or `assignment` from `call` because they share too many common byte contexts.

### Implications

The header↔loop separation confirms the manifold boundary hypothesis. During generation, when the eigenphase of the current output jumps from the -28° header cluster toward the +0° loop cluster, that's a detectable signal for a **manifold transition**. This could be used as a trigger for PhaseDial re-orientation.

However, finer structural discrimination (return vs. assignment vs. conditional) requires richer transition analysis — possibly at the token level rather than byte level, consistent with the Test 6 finding.

### Test 9: Phase-Triggered Manifold Bridging

**Motivated by Test 8**: the header↔loop eigenphase separation (28°) provides a measurable manifold boundary signal. Test 9 uses this as a runtime control surface for generation.

Combines overlap-aware span chaining with eigenphase tracking and a **progress-aware acceptance filter**. Bridge mode triggers on eigenphase crossing (-0.15 threshold) or similarity cliff (<0.4). The progress filter evaluates each candidate for structural advancement via three weighted signals:
- **Δphase** (angular movement from current eigenphase) × 2.0
- **new_ratio** (fraction of novel content, zeroed if text already in output) × 1.5
- **Δsim** (similarity improvement) × 0.5

Candidates below the progress threshold (0.05) are skipped; the chainer walks down the ranked list until it finds one that advances the program state.

| Prompt | Steps | Tokens | Return | Loop | Bridges | Key outcome |
|--------|-------|--------|--------|------|---------|-------------|
| factorial | 10 | 22 | no | no | 1 | Bridge to body ops |
| find_max | 10 | 22 | no | no | 1 | Bridge to body ops |
| binary_search | 10 | **56** | no | **yes** | 0 | **Reached `while low <=`** |
| dfs | 10 | 33 | no | yes | 1 | Body span: `visited.add(node)` |
| insertion_sort | 10 | 23 | no | yes | 0 | Reached `for i in` |

**Return: 0/5, Loop: 3/5, Bridge events: 3 total, Mean tokens: 31.2**

### Key improvements over v1 (no progress filter)

1. **Binary search broke the echo loop.** The novelty check detected that ` low, high` was already in the output, zeroed its new_ratio, and the progress filter skipped it (step 2 shows `(skipped 1)`). The chainer selected ` = 0, len(lst)` instead, then continued to ` - 1 while low <=`. Token count jumped from 23 → **56**.

2. **Loop count improved from 2/5 → 3/5.** Binary search now contains `while`, joining DFS and insertion sort.

3. **Mean token count increased from 24.6 → 31.2.** More content generated because the chainer isn't wasting steps on echoes.

### DFS remains the best result

The DFS bridge at step 6 continues to be the clearest manifold transition:
```
Steps 1–5: header phase (-0.87 to -1.11)
⚡ BRIDGE at step 6 (sim cliff to 0.38)
Step 6: 13-token body span, phase jumps to -0.04
Steps 7–10: body phase (+1.00 to +1.35)
```

This is a **measured structural phase transition**: header plateau → phase shift → body plateau. The eigenphase trajectory directly visualizes the manifold boundary crossing.

### Remaining limitations

Insertion sort still echoes (`def insertion_sort(lst): for` repeating). The issue is that both the `def...for` span and the ` i in` span alternate with enough phase movement (0.12 radians ≈ 7°) to stay above the progress threshold. Tightening the threshold would fix this specific case but risk blocking legitimate phase-moving spans in other cases.

### Test 10: Cantilever-Gated Span Retrieval

**Motivated by the old architecture's cantilever**: the wave-interference cantilever estimated how far a structural pattern could propagate. This test reimplements it using bitwise fingerprint overlap: for each FibWindow scale (largest first), find the best span of that length and check if its similarity to the boundary exceeds a threshold (0.3). The maximum coherent scale becomes the retrieval constraint.

**A/B comparison** — same prompts, same progress filter, same bridging:

| Metric | Control | Gated |
|--------|---------|-------|
| Mean tokens | 31.2 | 31.2 |
| Has return | 0/5 | 0/5 |
| Has loop | 3/5 | 3/5 |
| Bridge events | 3 | 3 |

**Result: null difference.** Cantilever gating produces identical aggregate metrics.

### Why: the cantilever extent is too permissive

Cantilever score profiles per prompt (scale 21→13→8→5→3):

| Prompt | scale 21 | 13 | 8 | 5 | 3 | Max coherent |
|--------|----------|-----|------|------|------|-------------|
| factorial | 0.247 | 0.521 | 0.666 | 0.819 | 0.918 | **13** |
| find_max | 0.415 | 0.496 | 0.612 | 0.771 | 0.925 | 21 |
| binary_search | 0.535 | 0.652 | 0.755 | 0.870 | 1.000 | 21 |
| dfs | 0.466 | 0.546 | 0.685 | 0.826 | 1.000 | 21 |
| insertion_sort | 0.463 | 0.563 | 0.638 | 0.856 | 0.923 | 21 |

For 4/5 prompts, the cantilever extent is 21 — the maximum FibWindow. At scale 21, the best span still has similarity > 0.3 (the threshold), so no constraint is applied. Only factorial drops below threshold at scale 21 (0.247), getting extent=13.

### What the profiles do reveal

The score decay across scales is informative even if it doesn't gate retrieval:

- **Binary search** holds strong across all scales (1.000→0.535). This function has long, coherent structure.
- **Factorial** decays fastest (0.918→0.247). Short recursive functions lose coherence at large scales.
- All prompts show monotonic decay from small→large, confirming that **structural coherence genuinely decreases with span length**.

### Implications

The cantilever concept is sound — structural coherence does decay across scales, and the measurement is correct. But at this corpus density (31 functions, ~2800 spans), the threshold is too easily exceeded because there are always spans that share enough structure with the prompt at any scale.

The cantilever would likely become effective with:
- **Larger corpora** where many competing spans exist at each scale
- **Higher threshold** (perhaps 0.5 instead of 0.3), though this needs calibration
- **Per-step recalculation** as the output grows and the boundary shifts

### Test 11: Relative Cantilever Scale Selection

**Motivated by Test 10's permissive threshold**: replaces the absolute threshold with a ratio criterion. For adjacent FibWindow scales, computes `s(w_large) / s(w_small)`. If the ratio drops below 0.7, the larger scale is too risky — coherence decays too steeply between scales.

**Ratio profiles** (scale 3→5→8→13→21):

| Prompt | 3→5 | 5→8 | 8→13 | 13→21 | maxSafe |
|--------|-----|-----|------|-------|---------|
| factorial | 0.892 | 0.813 | 0.782 | **0.474** | **13** |
| find_max | 0.834 | 0.794 | 0.810 | 0.837 | 21 |
| binary_search | 0.870 | 0.868 | 0.864 | 0.821 | 21 |
| dfs | 0.826 | 0.829 | 0.797 | 0.854 | 21 |
| insertion_sort | 0.856 | 0.923 | ... | ... | 21 |

**A/B comparison**:

| Metric | Control | Rel-Gated |
|--------|---------|-----------|
| Mean tokens | 31.2 | 31.2 |
| Has return | 0/5 | 0/5 |
| Has loop | 3/5 | 3/5 |
| Bridge events | 3 | 3 |

**Result: null difference again**, but now we know *exactly* why.

### What the ratios prove

The coherence decay between adjacent scales is **too gradual** for ratio gating to bite. Most inter-scale ratios are 0.79–0.87 — well above the 0.7 threshold. The structural signal doesn't "cliff" between adjacent FibWindow scales; it decays smoothly.

Only factorial's 13→21 ratio (0.474) triggers the gate. This is because factorial is a short recursive function — its 21-token spans are structurally incoherent while its 13-token spans are fine.

### The deeper conclusion

**The span ranking + progress filter already performs implicit scale selection.** When the progress filter rejects echoed spans and the ranking selects by similarity, the system naturally gravitates toward the right scale. The cantilever is measuring a real property (coherence decay) but the existing control loop is already exploiting that information through a different channel.

This is actually useful to know: it means the architecture doesn't need an explicit scale controller at this corpus density. The cantilever concept remains valid for larger corpora where the implicit selection may become insufficient, but for now it's a confirmed redundancy.

### Test 12: Chord-Based Retrieval (BVP over FibWindow Chords)

**Baseline wiring test.** This test validates that the chord encoding + BestFill pipeline produces correct output end-to-end. It uses a flat array of FibWindow chords (not the LSM), deterministic base chords (not the tokenizer/dataset pipeline), and retrieves corpus content that is already stored — it does not generate novel code.

#### What it does

```
1. Each byte value → deterministic base chord (5 bits in 512-bit space)
2. Corpus ingested at all FibWindow scales (3, 5, 8, 13, 21)
   Each window = OR(base chords of bytes in window) → stored in flat array
3. BestFill = popcount(candidate AND context) / (match + noise + 1) → resonance
4. Retrieval = locate prompt → advance corpus read head → emit → stop at \ndef
```

#### Results

33,185 chord entries across 5 FibWindow scales from 6,646 bytes (39 functions).

| Prompt | Steps | Tokens | return | colon | Complete? |
|--------|-------|--------|--------|-------|-----------|
| `def factorial(` | 5 | 81 | ✓ | ✓ | ✓ |
| `def find_max(` | 10 | 152 | ✓ | ✓ | ✓ |
| `def binary_search(` | 16 | 285 | ✓ | ✓ | ✓ |
| `def dfs(` | 22 | 370 | ✓ | ✓ | ✓ |
| `def insertion_sort(` | 10+ | — | ✓ | ✓ | ✓ |

#### What this proves

1. **The chord encoding + BestFill pipeline works end-to-end.** Byte → base chord → FibWindow OR-aggregation → popcount matching → correct corpus position recovery. The wiring is correct.

2. **FibWindow multi-scale storage works.** BestFill naturally oscillates between scale=21, 13, and 8 depending on the context. No explicit scale controller needed for retrieval.

3. **Stop tokens work.** `\ndef` boundary detection correctly terminates generation at function boundaries.

#### What this does NOT prove

1. **This is retrieval, not generation.** The system is looking up code that's already stored in a flat array. A window size equal to the function length would "generate" it in 1 step. The BestFill scores (0.97-0.98) are high because the context chord matches bytes that are already in the store.

2. **This does not use the LSM.** The ChordStore is a flat `[]ChordEntry` array, not the `LSMSpatialIndex` with collision compression, levels, or Morton key ordering.

3. **This does not use the tokenizer or dataset interface.** Base chords are hardcoded via coprime multipliers, not derived from the `Universal` tokenizer or `Dataset` provider.

4. **This does not generate out-of-corpus code.** Given `def is_even(n):` (not in corpus), this system would retrieve the nearest structural match and replay wrong code — exactly the problem Test 6 identified.

5. **This does not show prompt-to-code or reasoning.** There is no natural language → code path, no multi-step reasoning, no composition of novel programs.

#### What needs to happen next

The elements from Tests 1-11 address the **actual hard problems** that Test 12 doesn't attempt:

| Hard problem | What addresses it |
|-------------|-------------------|
| Compositional generation (novel code) | PhaseDial manifold navigation (Tests 6, 8, 9) |
| Structural pattern matching vs exact lookup | Eigenphase role discrimination (Test 8) |
| Manifold bridging (header → body) | Phase-triggered bridging (Test 9) |
| Scale-coherent retrieval at large corpus | Cantilever gating (Tests 10-11) |
| Semantic function identity | Richer chord construction (token-level, hierarchical) |
| Production pipeline integration | LSM spatial index, tokenizer, dataset interface |

Test 12 validates the **lowest layer** of the architecture (chords + BestFill = correct retrieval). Building actual generation on top requires wiring the real storage, tokenizer, and the compositional elements that Tests 1-11 explored.

---

## Test Progression Summary

| Test | File | Key Question | Key Finding | Motivates |
|------|------|-------------|-------------|-----------|
| 1 | `span_solver.go` | Can BVP voting generate code? | Retrieval works, voting creates chimeras | Test 2 |
| 2 | `span_ranking.go` | Does whole-span selection fix it? | 5/5 correct, +25% similarity | Test 3 |
| 3 | `span_chaining.go` | Can we chain multiple spans? | Works but duplicates prefixes | Test 4 |
| 4 | `overlap_chaining.go` | Does overlap-aware assembly fix duplication? | 5/5 valid, similarity increases mid-chain | Test 5 |
| 5 | `long_generation.go` | Does it scale to long programs? | Header gradient works; body jump problem | Test 6 |
| 6 | `compositional_gen.go` | Can it compose novel functions? | Structural matching yes, semantic no | Test 7 |
| 7 | `structural_sensitivity.go` | Is the encoding truly structure-sensitive? | 5/5 confirmed | Test 8 |
| 8 | `eigenmode_probe.go` | Can we measure structural roles? | Header↔loop separation at 28° | Test 9 |
| 9 | `phase_bridging.go` | Can eigenphase guide manifold bridging? | DFS bridges correctly; progress filter breaks binary_search echo | Test 10 |
| 10 | `cantilever_gating.go` | Does scale-aware retrieval help? | Null: absolute threshold too permissive | Test 11 |
| 11 | `relative_cantilever.go` | Does ratio-based gating help? | Null: decay too gradual; implicit selection sufficient | Test 12 |
| 12 | `chord_generation.go` | Does chord + BestFill retrieval work? | 5/5 correct retrieval; validates pipeline wiring | Wire LSM, tokenizer, composition |

## Output Logging

- **Corpus Hash**: Deterministic hash of the full corpus for regression tracking
- **Generated spans**: Raw token sequences from each prompt
- **Quality metrics**: return/colon presence, unique ratio, prefix relevance

## Generated Artifacts

| File | Description |
|------|-------------|
| `textgen.tex` | Complete LaTeX section with results tables and generated examples |
| `results.json` | Structured JSON with all experimental data |
| `span_solver.pdf` | Test 1 figure: retrieval score vs output relevance + quality flags |
| `span_ranking.pdf` | Test 2 figure: top-5 candidate similarity drop-off per prompt |
| `span_chaining.pdf` | Test 3 figure: per-step similarity scores across chain positions |
| `overlap_chaining.pdf` | Test 4 figure: per-step similarity + new tokens (dual axis) |
| `long_generation.pdf` | Test 5 figure: similarity curves for 7 long-function prompts |
| `compositional_gen.pdf` | Test 6 figure: out-of-corpus pure-similarity curves |
| `structural_sensitivity.pdf` | Test 7 figure: grouped bar chart of comment/noise/correct sensitivity |
| `eigenmode_probe.pdf` | Test 8 figure: eigenphase scatter plot on unit circle by role |
| `phase_bridging.pdf` | Test 9 figure: dual-axis sim + eigenphase with bridge markers |
| `cantilever_gating.pdf` | Test 10 figure: control vs gated similarity curves |
| `relative_cantilever.pdf` | Test 11 figure: control vs ratio-gated similarity curves |
| `chord_generation.pdf` | Test 12 figure: per-step score + scale selection + stop token markers |

## Executing the Test Suite

```bash
go test -v -timeout 600s ./experiment/task/textgen/ -run TestSpanSolverSuite
```

