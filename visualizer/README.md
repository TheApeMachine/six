# Six Visualizer

React/Vite inspection UI and local bridge for live Six telemetry. The Go runtime dials the bridge WebSocket as a client and sends raw 1024-byte Value frames on that connection; the bridge fans them out to every browser tab also connected to `/ws`. The bridge serves `/ws`, `/api/prompt`, and `/api/programs`.

## Run

```bash
npm install
npm run dev
npm run bridge
```

By default the Vite dev server exposes `ws://localhost:3000/ws` (see `telemetryWebSocketURL()` in `src/features/telemetry/endpoint.ts`). The Go process uses `telemetry.ws_url` (e.g. `ws://127.0.0.1:3000/ws`) to dial that same path and push raw frames.

```bash
VITE_VIZ_HOST=host.docker.internal VITE_VIZ_PORT=6600 npm run dev
```

Run any Go workload that enables telemetry. Point `telemetry.ws_url` at the bridge (defaults in `cmd/cfg/config.yml` match the Vite port).

```bash
go test -ldflags='-checklinkname=0' -tags=exp_pipeline -run 'TestPipeline/Substrate_query_scaling$' ./experiment/task/
```

If you have a separate prompt-control process, point the bridge at it:

```bash
TELEMETRY_CONTROL_URL=http://127.0.0.1:8259 npm run bridge
```

## Inspection Surfaces

- **Live** renders the current event stream as a field/value canvas.
- **Fields** renders community membership, saturation, concentration, and emitted action counts.
- **Values** renders the causal graph from `PrevID` and `NextID` telemetry.
- **Stream** shows the raw decoded Value frames.
- **Telemetry** follows the canonical 1 KB Value layout from `pkg/compute/kernel/layout.go`, including property words 48–63.
- **Programs** fetches firmware source from `/api/programs` and renders each DSL line as a dataflow circuit.

## Checks

```bash
npm run lint
npm run build
```
