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

## Output Logging

- **Corpus Hash**: Deterministic hash of the full corpus for regression tracking
- **Generated spans**: Raw token sequences from each prompt
- **Quality metrics**: return/colon presence, unique ratio, prefix relevance

## Generated Artifacts

| File | Description |
|------|-------------|
| `textgen.tex` | Complete LaTeX section with results table and generated examples |
| `results.json` | Structured JSON with all experimental data |

## Executing the Test Suite

```bash
go test -v -timeout 300s ./experiment/task/textgen/ -run TestSpanSolverSuite
```
