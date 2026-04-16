package vm

import (
	"context"
	"errors"
	"io"

	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/compute/programmer"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/gossip"
	"github.com/theapemachine/six/pkg/mesh"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
)

/*
Orchestrator seeds the in-value pipeline. Its job is three things:
owning the compute.Backend that runs ALU work, owning the root Field
where published Values land, and kicking off the rule-driven firmware
chain for each freshly ingested Value so neighborhood topology exists
before any downstream Conn pulls from the Field. The rule walk itself
lives in programmer.Firmware.Chain; the orchestrator just submits the
first step on the pool and each firmware Executable re-enters Chain
through the same scheduler until the Value reaches steady state and
its resident program takes over.

The Backend is wired into the pool.Queue at construction time as the
dispatch callback. Without this the pool worker pulls a task, receives
the Executable, and drops it — a silent stall that manifests in the
visualizer as every Value parked at "awaiting first ALU dispatch".
*/
type Orchestrator struct {
	ctx       context.Context
	cancel    context.CancelFunc
	err       error
	backend   *compute.Backend
	queue     *pool.Queue
	firmware  *programmer.Firmware
	scheduler programmer.Scheduler
	field     *mesh.Field
	telemetry io.ReadWriteCloser
	emitter   *Emitter
}

type orchestratorOption func(*Orchestrator)

/*
NewOrchestrator creates a new orchestrator wired to the queue and a
compute.Backend whose Dispatch is registered with the pool so workers
actually run Executables against a substrate.
*/
func NewOrchestrator(
	ctx context.Context,
	options ...orchestratorOption,
) (*Orchestrator, error) {
	ctx, cancel := context.WithCancel(ctx)

	// The Backend must be live before the pool so its Dispatch is the
	// very first thing workers see. A nil backend here is fatal — the
	// orchestrator has nothing for the pool to run.
	backend := compute.NewBackend(ctx)

	if backend == nil {
		cancel()
		return nil, errnie.Error(errors.New("vm.NewOrchestrator: compute.NewBackend returned nil"))
	}

	queue, err := pool.NewQueue(ctx, backend.Dispatch)

	if err != nil {
		// Cancel the derived context so the parent doesn't retain a
		// stranded child on the failure path — vet flags the leak.
		cancel()
		errors.Join(err, backend.Close())
		return nil, errnie.Error(err)
	}

	telemetryClient, err := telemetry.NewClient(ctx, core.Cfg.TelemetryWebSocketURL)

	if err != nil {
		cancel()
		return nil, errnie.Error(err)
	}

	emitter, err := NewEmitter(ctx)

	if err != nil {
		cancel()
		return nil, errnie.Error(err)
	}

	orchestrator := &Orchestrator{
		ctx:       ctx,
		cancel:    cancel,
		backend:   backend,
		queue:     queue,
		firmware:  programmer.NewFirmware(),
		scheduler: programmer.Scheduler(queue),
		// The root field runs in routing mode: incoming Values land in a
		// GF(8191) community child keyed by affinity Hamming distance,
		// capped at a 48-bit budget before a new community is spawned.
		// Leaf storage lives one level down in those children; the root
		// only carries the XOR-folded parent fingerprint.
		field:     mesh.NewField(ctx, 65537, mesh.WithCommunities(8191, 48)),
		telemetry: telemetryClient,
		emitter:   emitter,
	}

	for _, option := range options {
		option(orchestrator)
	}

	if err := validate.Require(map[string]any{
		"ctx":      orchestrator.ctx,
		"cancel":   orchestrator.cancel,
		"backend":  orchestrator.backend,
		"queue":    orchestrator.queue,
		"firmware": orchestrator.firmware,
		"field":    orchestrator.field,
	}); err != nil {
		cancel()
		errors.Join(err, backend.Close())
		return nil, errnie.Error(err)
	}

	return orchestrator, nil
}

/*
Close the orchestrator.
*/
func (orchestrator *Orchestrator) Close() error {
	orchestrator.cancel()

	if orchestrator.emitter != nil {
		if emitterErr := orchestrator.emitter.Close(); emitterErr != nil {
			orchestrator.err = errors.Join(orchestrator.err, emitterErr)
		}
	}

	if orchestrator.telemetry != nil {
		if telErr := orchestrator.telemetry.Close(); telErr != nil {
			orchestrator.err = errors.Join(orchestrator.err, telErr)
		}
	}

	if orchestrator.backend != nil {
		if backendErr := orchestrator.backend.Close(); backendErr != nil {
			orchestrator.err = errors.Join(orchestrator.err, backendErr)
		}
	}

	return orchestrator.err
}

/*
Error returns the error of the orchestrator.
*/
func (orchestrator *Orchestrator) Error() error {
	return orchestrator.err
}

/*
Cycle seeds the in-value pipeline with a wave of Values and lets the
system develop. Each Value's asset region is pre-loaded with its
predecessor and successor IDs so the link firmware has material to
copy into prev/next on the first ALU pass.

Pass 1 (raw): Conn.Read serialises each Value through the io pipeline
(emitter → telemetry → field) for immediate visibility. For Values
that are still STATUS_RAW, Read also tees them into the firmware chain
(link → affinity → resident). The terminal finalizer stamps
STATUS_READY and sends the Value to the loopback channel.

Pass 2 (bootstrapped): after the first io.Copy drains, Cycle collects
every loopback Value and re-cycles them. This time the Values carry
real prev/next and affinity, so telemetry shows populated frames and
the field routes with real Hamming distances. Conn.Read sees
STATUS_READY and skips the firmware tee — the Value just flows through.
*/
func (orchestrator *Orchestrator) Cycle(
	values ...*primitive.Value,
) (resolved []*primitive.Value, err error) {
	bundle := make([]*primitive.Value, 0, len(values))

	for _, value := range values {
		if value != nil {
			bundle = append(bundle, value)
		}
	}

	if len(bundle) == 0 {
		return nil, nil
	}

	// Seed the link chain: stamp each Value's asset[0] with the
	// predecessor's ID and asset[1] with the successor's ID so the
	// link firmware (asset[0,1]→prev, asset[1,1]→next) has material
	// to copy on the first ALU pass. Values at the head/tail of the
	// wave get a zero in the missing direction, which is fine — the
	// rule evaluator treats zero as "not linked" and the affinity rule
	// only requires (prev OR next), not both.
	assetStart, _ := core.Cfg.Value.Region.Asset.WordExtent()

	for idx, value := range bundle {
		if idx > 0 {
			value.Set(assetStart, bundle[idx-1].ID())
		}

		if idx+1 < len(bundle) {
			value.Set(assetStart+1, bundle[idx+1].ID())
		}
	}

	orchestrator.emitter.values = make([]*primitive.Value, 0)

	// Loopback channel: the terminal finalizer sends bootstrapped
	// values here after the firmware chain stamps STATUS_READY.
	// Buffered to bundle size so finalizers never block the pool.
	loopback := make(chan *primitive.Value, len(bundle))

	conn, err := gossip.NewConn(
		orchestrator.ctx,
		orchestrator.scheduler,
		bundle...,
	)

	if err != nil {
		return nil, errnie.Error(err)
	}

	conn.Loopback(func(value *primitive.Value) {
		loopback <- value
	})

	if err = orchestrator.pumpPipeline(conn, len(bundle), false); err != nil {
		return nil, err
	}

	// Drain loopback: collect every bootstrapped Value and re-cycle.
	// The firmware chain is async so we drain until the channel has
	// as many values as we submitted (one loopback per raw value).
	bootstrapped := make([]*primitive.Value, 0, len(bundle))

	for range len(bundle) {
		bootstrapped = append(bootstrapped, <-loopback)
	}

	close(loopback)

	// Pass 2: bootstrapped values flow through the pipeline again.
	// Conn.Read sees STATUS_READY and skips the firmware tee, so
	// they just pass through to telemetry (visible) and field (routed).
	conn2, err := gossip.NewConn(
		orchestrator.ctx,
		orchestrator.scheduler,
		bootstrapped...,
	)

	if err != nil {
		return nil, errnie.Error(err)
	}

	if err = orchestrator.pumpPipeline(conn2, len(bootstrapped), true); err != nil {
		return nil, err
	}

	return nil, nil
}

/*
pumpPipeline drives one pass of the io pipeline. The full pass wires
Conn → emitter → telemetry → field so downstream consumers see every
frame. The drain-only variant (full=false) just reads Conn to
completion — its only purpose is triggering Conn.Read's firmware tee
for raw values that have no meaningful affinity yet.
*/
func (orchestrator *Orchestrator) pumpPipeline(conn *gossip.Conn, count int, full bool) error {
	limit := int64(count) * int64(core.Cfg.Value.Bytes)
	src := io.LimitReader(gossip.FrameDelimitedReader(conn), limit)

	if !full {
		_, err := io.Copy(io.Discard, src)
		return errnie.Error(err)
	}

	teeEmitter := io.TeeReader(src, orchestrator.emitter)
	teeTelemetry := io.TeeReader(teeEmitter, orchestrator.telemetry)

	if _, err := io.Copy(orchestrator.field, teeTelemetry); err != nil {
		return errnie.Error(err)
	}

	return nil
}
