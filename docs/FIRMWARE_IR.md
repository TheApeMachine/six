# Canonical firmware IR (plan)

The **stable product** is `ProgramIR` in `pkg/compute/program/ir.go`, not feed text or a future human DSL surface. All frontends converge on the same IR, then lower to `MachineOp`, then pack into **exactly sixteen** resident program words (`FormatProgramSweep16` must be able to print the result).

**Non-negotiable:** anything that cannot be shown as a 16-slot sweep (with explicit `empty` lines) is not firmware.

**Primary entry (IR → words):** `EncodeProgramIR` in `encode_program.go`. It does not use the Tree-sitter feed parser.

**Legacy entry (feed → words):** `Compile` in `compiler.go` remains for existing `programs:` blocks in YAML.

**Milestone program:** `RecruitMinimalProgramIR` + `lowerRecruitMinimalToMachineOps` implement §24 of the firmware plan (`recruit_minimal`).

See `SYNTAX.md` for the feed language; see the root implementation plan for phases 8–11 (JSON/YAML, scheduler contract, self-programming API).
