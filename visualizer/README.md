# Six Visualizer

React/Vite inspection UI and local bridge for live Six telemetry. The Go runtime now emits binary VZB frames over the configured telemetry transport, and the bridge inside `visualizer/` owns the browser-facing `/ws`, `/api/prompt`, and `/api/programs` endpoints.

## Run

```bash
npm install
npm run dev
npm run bridge
```

By default the browser connects to `ws://localhost:6600/ws`, while the bridge listens for raw Go telemetry on UDP `127.0.0.1:8258`.

```bash
VITE_VIZ_HOST=host.docker.internal VITE_VIZ_PORT=6600 npm run dev
```

Run any Go workload that enables telemetry. The default config already points telemetry at `127.0.0.1:8258`.

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
- **Stream** shows the raw decoded wire events, including every `vals` and `meta` payload.
- **Telemetry** follows the canonical 1 KB Value layout from `pkg/compute/kernel/layout.go`, including property words 48-55 and scheduler word 117 when the backend emits them.
- **Programs** fetches firmware source from `/api/programs` and renders each DSL line as a dataflow circuit.

## Checks

```bash
npm run lint
npm run build
```
