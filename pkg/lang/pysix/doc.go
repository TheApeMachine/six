/*
Package pysix ingests a restricted subset of Python 3 (via a host python3 process
that exports a JSON AST) and lowers it to stepwise descriptors executed with
compute/stepwise.RunScalar against one 128-word frame.

This is an experimental bridge: Python carries meaning, six carries execution.
Full Python semantics are intentionally out of scope; unsupported constructs
produce clear compile errors.

Requirements: python3 on PATH, and the shipped scripts/astexport.py (also
embedded for locating next to tests).

Supported (v1): int literals (0..2^64-1 with multi-step load when imm16 is not
enough), Name, + − * (small constant multiplier unroll), Unary minus, Compare
Eq/NotEq on two uint64 paths, Assign, AugAssign (+ − * with constant right for
*), ExprStmt (discard), Pass, If when each branch is exactly one Assign to the
same target name, ForRange with constant range(stop). Module-level only; nested
functions and calls are rejected.
*/
package pysix
