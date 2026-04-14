# Six Visualizer

React/Vite inspection UI for the live `pkg/viz` event stream. It connects to the Go viz server over the binary WebSocket protocol at `/ws` and uses `/api/prompt` plus `/api/programs` for prompt injection and firmware inspection.

## Run

```bash
npm install
npm run dev
```

By default the UI connects to `ws://localhost:6600/ws`.

```bash
VITE_VIZ_HOST=host.docker.internal VITE_VIZ_PORT=6600 npm run dev
```

Start the Go event server from the repository root:

```bash
go run . viz --addr :6600
```

Use `--demo` for a self-contained event stream.

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
