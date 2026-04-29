# Canonical firmware IR

The **stable product** is **`ProgramIR`**, defined in `pkg/compute/program/ir.go`; it is not feed text nor a provisional human-facing DSL surface.
**`ProgramIR`** is modeled as a 16-slot **`MachineOp` sweep**: frontends resolve names, regions, properties, and constants before this package packs the sixteen resident program words (**`FormatProgramSweep16`** prints the result).

**Non-negotiable:** anything that cannot be shown as a 16-slot sweep (with explicit `empty` lines) is not firmware.

**Primary entry (IR → words):** `EncodeProgramIR` in `encode_program.go`. It does not use the Tree-sitter feed parser.

**Legacy entry (feed → words):** `Compile` in `compiler.go` remains for existing `programs:` blocks in YAML.

⚙️ **Operand alignment:** truth-table instructions may use the predicate-condition bits as SrcB byte-rotation metadata when `predicate = 0`. This is exposed in source as `rot8(B.region[start,span], n)` for `n in [0,7]`; predicate instructions keep the same bits for comparison and reduction selection.

⚙️ **Lane reducers:** predicate-flagged reducer opcodes operate over explicit
region references, not task names. Current reducers are
`argmin_nonzero(B.value, B.key)`, `mode_eq(B.value, B.key, A.match)`, and
`zipf_select(B.value, B.utility, A.temperature)`. `zipf_select` writes one
non-zero B-side value after ranking by utility; `A.temperature = 0` is greedy,
positive temperature selects an integer Zipf power bucket that flattens toward
uniform tail pressure. The sample is deterministic from owner witness words
rather than host RNG.

See `SYNTAX.md` for the feed language; see the root implementation plan for phases 8–11 (JSON/YAML, scheduler contract, self-programming API).
