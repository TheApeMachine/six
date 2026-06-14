# Canonical firmware IR

📋 The **stable product** is **`ProgramIR`**, defined in `pkg/compute/program/ir.go`; it is not feed text nor a provisional human-facing DSL surface.
**`ProgramIR`** is modeled as a 16-slot **`MachineOp` sweep**: frontends resolve names, regions, properties, and constants before this package packs the sixteen resident program words (**`FormatProgramSweep16`** prints the result).

**Non-negotiable:** anything that cannot be shown as a 16-slot sweep (with explicit `empty` lines) is not firmware.

**Primary entry (IR → words):** `EncodeProgramIR` in `encode_program.go`. It does not use the Tree-sitter feed parser.

**Legacy entry (feed → words):** `Compile` in `compiler.go` remains for existing `programs:` blocks in YAML.

⚙️ **Strict sweep:** kernels execute `pc = 0..15` exactly once. Iteration,
recursion, and branch selection are expressed by in-frame status/continuation
words and rescheduling, not by hidden ALU loops. `pop(B)`, `stage(B)`, and
`popEnd` rewinds are outside the canonical firmware contract.

⚙️ **Operand alignment:** truth-table instructions may use the
predicate-condition bits as SrcB byte-rotation metadata when `predicate = 0`.
This is exposed in source as `rot8(B.region[start,span], n)` for `n in [0,7]`;
predicate instructions keep the same bits for comparison and scalar selection.

⚙️ **Allowed scalar/predicate slots:** `popcnt` compare/store remains the
resident scalar witness primitive. Predicate condition `PredScalar` carries the
generic scalar sublane, with opcode naming `shiftl`, `shiftr`, `rotl`, or
`rotr`. These are word operations, not named task reducers.

⚙️ **Hypercube gossip:** `gossip(B)` is first-class topology. `target=B`
broadcasts the truth-table result to each peer; `target=A` folds bitwise
truth-table output back into the owner. `TopoHypercubePerPeer` may use in-band
peer masks written by preceding predicate slots.

⚙️ **Rejected helper surface:** the feed compiler rejects `pop(B)`, `stage(B)`,
`argmin_nonzero`, `mode_eq`, `zipf_select`, `geo_centroid`, `geo_nearest`,
`run_zero`, `run_one`, `align_zero`, and `any_zero`. CSA and carry-style code may exist as
internal implementations of popcount/carry mechanics, but they are not public
semantic reducers.

⚙️ **Geometric slots:** PGA instructions occupy a raw resident slot, not a
packed low-nibble truth-table word. The canonical opcodes are `0x10`
(`geometric compose`), `0x20` (`geometric sandwich`), and `0x30`
(`geometric reverse`). They carry no Boolean operands or predicates; the fixed
frame contract is `Context` / `Gradient` → `Signals`, and unused geometric
operands remain implicit in the frame. `ProgramIR` uses `MachineOp{Opcode:
OpGeometric...}` for the same raw slot shape.

Feed examples live in the root `README.md` and in `cmd/cfg/config.yml`; self-programming paths should prefer `ProgramIR` rather than emitting source text.
