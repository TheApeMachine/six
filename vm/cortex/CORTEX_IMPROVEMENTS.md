# Cortex improvements

This change set focuses on the volatile `vm/cortex` reasoning substrate.

## What changed

### 1. Prompt-cycle reset
The cortex now resets its volatile scratchpad state when a new prompt arrives. This clears accumulated cube mass, signal history, expanded mitotic nodes, and convergence state while rebuilding the base small-world topology.

Why this matters: the cortex is supposed to be a volatile reasoning workbench. Without a reset, prompt contamination leaks across tasks and makes later reasoning depend on unrelated prior cycles.

### 2. Conflict-aware multi-axis rotations
`DeriveOpcode` no longer collapses every true gate conflict onto `OpRotateX`. Conflicting gates now derive their rotation axis from the conflict residue itself using chord geometry, allowing all three physical rotation bands to participate in cortex dynamics.

Why this matters: a single conflict axis artificially flattens the graph's motion and suppresses legitimate topological diversity.

### 3. Signal memory and result condensation
Nodes now remember the strongest observation of each routed signal and suppress weaker echo loops. Sink extraction condenses routed signals into ranked reasoning residues instead of reading arbitrary thermodynamic gate mass.

Why this matters: this reduces signal ping-pong, keeps the sink cleaner, and makes cortex output a deliberate reasoning artifact rather than a side effect of background state.

### 4. Residue-seeded mitosis
Spawned nodes now inherit the parent's unresolved residue and the parent's rotational frame. Search expansion also refuses to attach to a nearest node if it is rotationally close but not resonant with the current search residue.

Why this matters: a new child node should represent an unresolved subproblem, not an empty bubble. This makes topology growth carry semantic pressure.

### 5. Dream-to-signal propagation
When a node detects a meaningful hole, it still writes the residue into face 256, but it also emits a signal token carrying that residue over the graph.

Why this matters: unresolved structure becomes active reasoning flow instead of inert control-plane mass.

### 6. Hot-path cleanup
The unconditional per-tick trace write was removed from the `Step()` hot path. The trace function is still available, but it is no longer invoked every tick during normal execution.

Why this matters: per-tick file I/O is hostile to the performance goals of the cortex.

## Added tests

New tests cover:
- multi-axis conflict routing
- prompt-cycle reset behavior
- residue-seeded child spawn behavior
- signal echo deduplication
- result ranking by signal support

## Validation

The `vm/cortex` package was validated in an isolated local build copy using stubbed external modules, because the full project test graph requires network-only dependencies that were unavailable in the execution environment.
