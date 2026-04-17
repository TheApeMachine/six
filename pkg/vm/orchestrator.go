package vm

import (
	"context"
	"io"
	"slices"

	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/gossip"
	"github.com/theapemachine/six/pkg/mesh"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
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
	ctx    context.Context
	cancel context.CancelFunc
	err    error
	queue  *pool.Queue
	field  *mesh.Field
}

/*
NewOrchestrator creates a new orchestrator wired to the queue and a
compute.Backend whose Dispatch is registered with the pool so workers
actually run Executables against a substrate.
*/
func NewOrchestrator(
	ctx context.Context,
) (*Orchestrator, error) {
	ctx, cancel := context.WithCancel(ctx)

	queue, err := pool.NewQueue(ctx, nil)

	if err != nil {
		cancel()
		return nil, errnie.Error(err)
	}

	orchestrator := &Orchestrator{
		ctx:    ctx,
		cancel: cancel,
		queue:  queue,
		field:  mesh.NewField(ctx, 65537, queue),
	}

	if err := validate.Require(map[string]any{
		"ctx":    orchestrator.ctx,
		"cancel": orchestrator.cancel,
		"field":  orchestrator.field,
	}); err != nil {
		cancel()
		return nil, errnie.Error(err)
	}

	return orchestrator, nil
}

/*
Close the orchestrator.
*/
func (orchestrator *Orchestrator) Close() error {
	orchestrator.cancel()
	orchestrator.queue.Close()
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

The returned slice is whatever the terminal emitter collected on pass 2.
That slice is the resolution the caller sees: every frame that travelled
the full io.ReadWriteCloser chain end-to-end, already serialised back
through ValueFromWireFrame so the caller holds independent Value copies.
*/
func (orchestrator *Orchestrator) Cycle(
	values ...*primitive.Value,
) (resolved []*primitive.Value, err error) {
	clones := slices.Clone(values)

	conn, err := gossip.NewConn(orchestrator.ctx, orchestrator.queue, clones...)

	if err != nil {
		return nil, errnie.Error(err)
	}

	for {
		if _, err = io.Copy(orchestrator.field, conn); err != nil {
			return nil, errnie.Error(err)
		}

		if _, err = io.Copy(conn, orchestrator.field); err != nil {
			return nil, errnie.Error(err)
		}

		if err == io.EOF {
			break
		}
	}

	return resolved, nil
}
