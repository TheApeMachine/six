## Learned User Preferences

- Discuss concrete solutions briefly before making non-trivial edits, and wait for explicit approval unless the user clearly says to fix it now.
- Be concise and action-oriented: avoid restating obvious project context or explaining the user's own system back to them.
- Keep scope extremely tight to the exact request and avoid unrelated cleanup or exploratory changes.
- Do exactly what was asked, nothing more, nothing less; never add adapters, fallbacks, suppressions, or "helpful" refactors the user did not explicitly request.
- When the user says "go", "just do it", or "stop talking", skip preamble and clarifying questions and implement immediately.
- Treat the user as the designer of this system; do not push competing architectures or override their stated direction with your own opinion.
- Never edit generated artifacts under `paper/include/**` — they are produced by the experiment harness, not authored.
- In TypeScript, do not use `any`; use proper types.

## Learned Workspace Facts

- The visualizer's live telemetry path can involve tens of thousands of values arriving one by one, so hot paths must avoid full rebuilds, unnecessary allocations, and continuous animation.
- In the mesh pipeline, visualizer "communities" correspond to child `Field` ids stamped into the `COMMUNITY` property during routing.
- The intended live visualizer surface is graph-first and field/community-centric, with inspector/detail views as secondary UI.
- Run experiments via `make paper`, which executes `go test -ldflags='-checklinkname=0' -tags=exp_pipeline -v ./experiment/task/`; `-ldflags='-checklinkname=0'` is required on every `go test` invocation in this repo.
- Telemetry must originate only from `pkg/gossip/conn.go` and only carry `primitive.Value` payloads; never emit telemetry from fields, queues, orchestrator, or anywhere else.
- The telemetry websocket bridge is a Vite plugin in the visualizer; the Go side pushes onto it (it is not a client).
- The I/O layer is built on `io.MultiReader`/`io.MultiWriter`/`io.TeeReader`/`io.Copy`; do not introduce manual Read/Write loops or cursor bookkeeping in `pkg/gossip/conn.go` or `pkg/transport/*`.
- Community fields saturate at the Shannon limit defined in config (currently 47%); when full, route the incoming Value to a new community. Avoid hard-coded magic numbers — derive metrics through `pkg/core/numeric/`.
- Region-aggregation pattern: `primitive.Value.Write` copies Signals/Context/Gradient/Properties into Assets; community fields aggregate from their Values' regions the same way, and the global field aggregates from community fields the same way.
- Execution model is branchless and loop-less: `UniversalBitwise` (CPU/CUDA/Metal) takes a single Value and operates only on that Value's own data, never on pairs, with no thread divergence.
- `gossip.Conn` is the "cycle": Values enter via Write, exit via Read, are mutated in transit (e.g. `pkg/primitive/value.go` Write), and only Values with STATUS=READY are submitted to the queue/backend.
- `errnie` logs to Elasticsearch (index `six-logs`) when enabled and produces large `*.Read []` traces during `make paper` runs.
