# NEXTEST Truth Gate

This document ties `NEXTEST.md` claims to executable evidence in the current tree.

## Status Legend

- `proved by test`: directly exercised by current automated tests
- `proved by benchmark`: supported by benchmark output in the current tree
- `partially proved`: some mathematical or local-runtime part is verified, but the section overclaims beyond the current implementation
- `not provable in current implementation`: the repo does not contain the runtime needed to prove the section honestly

## Final Claim Status

| NEXTEST Section | Status | Current Evidence | Limit |
|---|---|---|---|
| `What A Value Is` | `proved by test` | `test/integration/primes_test.go`, `test/integration/lattice_test.go` | Proven over the byte projection and exact bit-field fixtures |
| `Why Primes, Not Plain Bit Positions` | `proved by test` | `test/integration/primes_test.go`, `test/integration/lattice_test.go`, `test/integration/claims_test.go` | Proven for GCD/LCM/XOR/AndNot, divisibility, and coprimality |
| `Operations On Values` | `proved by test` | `test/integration/primes_test.go`, `pkg/primitive/operation/bitwise_test.go`, `pkg/primitive/operation/fuzz_test.go` | Boolean truth-table rows are now exercised through the in-band path |
| `The Shell: A Value As A Transformation` | `proved by test` | `test/integration/motors_test.go`, `test/integration/lattice_test.go` | Proven for deterministic derivation, composition, inversion, and transport execution |
| `Structural Relevance In One Instruction` | `proved by test` | `test/integration/primes_test.go`, `test/integration/claims_test.go` | Proven for exact AND/equality routing semantics on current fixtures |
| `Novelty Extraction In One Operation` | `proved by test` | `test/integration/claims_test.go` | Proven for exact `A & ~B` novelty and reconstruction |
| `Self-Navigation Via Derived Motors` | `partially proved` | `test/integration/motors_test.go` | Motor application and exact round-trips are proven; corpus-region navigation is not implemented as a runtime |
| `Lattice Distance` | `proved by test` | `test/integration/primes_test.go`, `test/integration/lattice_test.go` | Proven for exact XOR structure and coprime/max-distance cases |
| `What This Replaces` | `not provable in current implementation` | local math only | No runtime exists that replaces ANN/search/classification systems end-to-end |
| `Sequence Accumulation: Motor And Composition Interleaved` | `proved by test` | `test/integration/claims_test.go`, `test/integration/motors_test.go` | Proven for non-commutativity, sequence sensitivity, and exact round-trip fixtures |
| `Multi-Branch Navigation, Not Next-Token Prediction` | `not provable in current implementation` | none | No runtime currently returns branch sets or ranked multi-continuation spans |
| `GPU Hardware Sympathy: Full-Span Resolution In One Cycle` | `partially proved` | `pkg/compute/kernel/cpu/backend_test.go`, `pkg/compute/kernel/cuda/backend_test.go`, `pkg/compute/kernel/cuda/backend_stub_test.go`, `pkg/compute/kernel/metal/backend_test.go` | Batch kernel correctness and local throughput are benchmarked; full-span one-dispatch resolution and `O(null)` ingestion are not proven |
| `Contextual Bias Without External State` | `partially proved` | `test/integration/claims_test.go` | Exact overlap/ranking math is proven, but branch selection in a live resolver is not implemented |
| `Bidirectional Resolution` | `proved by test` | `test/integration/claims_test.go`, `test/integration/motors_test.go` | Proven for middle-fragment reconstruction and full sentence round-trip recovery |
| `Composition Via Residual Tracking` | `partially proved` | `test/integration/primes_test.go`, `test/integration/claims_test.go` | Residual exactness and monotonic shrink are proven; multi-fragment continuation switching is not implemented |
| `The Knowledge Tree: Classification Via Parallel Cancellation` | `partially proved` | `test/integration/classification_test.go` | The cancellation math and max-overlap classification are proven with reference helpers only; no production knowledge-tree package exists |
| `8191 Independent Dimensions` | `not provable in current implementation` | none | Architectural interpretation only |
| `Möbius Inversion: Reconstructing Components From Composites` | `partially proved` | `test/integration/lattice_test.go`, `pkg/primitive/algebra/mobius.go` | Exact reconstruction fixtures are proven, but not a general inversion engine |
| `Motor Equivalence Classes` | `proved by test` | `test/integration/lattice_test.go` | Constructive collision and identical transform behavior are proven |
| `Motor Orbits And Natural Periods` | `partially proved` | `test/integration/claims_test.go` | Orbit periodicity is proven on fixtures; richer period-interaction claims remain broader than current tests |
| `Chinese Remainder Theorem: Independent Decomposition` | `proved by test` | `test/integration/claims_test.go` | Coprime decomposition, independence, and exact recombination are proven on explicit fixtures |
| `Dirichlet Convolution: Functions That Compose On The Lattice` | `not provable in current implementation` | none | No implementation exists in the current tree |
| `The State-Space As Working Memory` | `not provable in current implementation` | none | No runtime exists that exposes this as an executable subsystem |
| `The Value As Wire Format` | `partially proved` | `test/integration/primes_test.go`, `pkg/primitive/value_test.go`, `pkg/transport/*_test.go` | Fixed-width framing and `io.ReadWriteCloser` composition are proven; actual network-frame behavior is not exercised in repo tests |
| `Operations Are Values` | `partially proved` | `test/integration/primes_test.go`, `test/integration/motors_test.go` | In-band operation frames are proven locally; the distributed interpretation is still unimplemented |
| `Self-Routing Via AND` | `proved by test` | `test/integration/claims_test.go` | Exact summary overlap/ranking behavior is proven on explicit routing fixtures |
| `Transport Hierarchy` | `not provable in current implementation` | local only in `pkg/transport` | No shared-memory, multicast, or QUIC transport hierarchy exists in the current tree |
| `Idempotency And The Absence Of TCP` | `not provable in current implementation` | none | No network substrate exists to prove this |
| `Discovery` | `not provable in current implementation` | none | No multicast/bootstrap/gossip implementation exists |
| `Deployment Story` | `not provable in current implementation` | none | Narrative only |

## Local Benchmark Snapshot

Latest benchmark run on `darwin/arm64` (`Apple M4 Max`):

- `pkg/compute/kernel/cpu`
  CPU `BitwiseAnd`: `31.91 ns/op` at batch `1`, `38.299 us/op` at batch `1024`, `0 allocs/op`
- `pkg/compute/kernel/cpu`
  CPU `MotorApply`: `357.4 ns/op` at batch `1`, `526.446 us/op` at batch `1024`, `0 allocs/op`
- `pkg/compute/kernel/cuda`
  Stub `Available`: `66.277 us/op`, `609 B/op`, `5 allocs/op`
- `pkg/compute/kernel/metal`
  Metal `BitwiseAnd`: `152.744 us/op` at batch `1`, `1.434975 ms/op` at batch `1024`, `0 allocs/op`

These benchmarks prove that the current backend surfaces execute fixed-width batch kernels and expose measurable throughput. They do not prove the stronger prose claims about `O(null)` ingestion, one-dispatch full-span retrieval, or a complete distributed GPU routing pipeline.
