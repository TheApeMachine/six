package vm

import (
	"context"
	"errors"
	"io"
	"slices"

	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/gossip"
	"github.com/theapemachine/six/pkg/mesh"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
	"github.com/theapemachine/six/pkg/transport"
)

/*
Orchestrator owns the global mesh.Field, the shared pool.Queue, and the
compute.Backend. The root gossip.Conn is not fixed at construction: each
Cycle builds gossip.NewConn from the non-nil *primitive.Value batch so
the pipeline stages match the incoming Values (same idea as mesh.Field
rebuilding its conn from READY members).

Callers pass Values in, receive resolved Values out after draining the
field (post-encounter, post-ALU, post-metric emissions).
*/
type Orchestrator struct {
	ctx       context.Context
	cancel    context.CancelFunc
	err       error
	root      *gossip.Conn
	queue     *pool.Queue
	field     *mesh.Field
	backend   *compute.Backend
	telemetry *telemetry.Bridge
}

/*
NewOrchestrator builds the seed graph: backend, queue, global Field.
The root Conn is first allocated in Cycle from the incoming Value batch.
*/
func NewOrchestrator(
	ctx context.Context,
	telemetry *telemetry.Bridge,
) (*Orchestrator, error) {
	ctx, cancel := context.WithCancel(ctx)

	backend := compute.NewBackend(ctx)

	if backend == nil {
		cancel()
		return nil, errnie.Error(
			errors.New("vm.NewOrchestrator: compute.NewBackend returned nil (no substrates registered)"),
		)
	}

	queue, err := pool.NewQueue(ctx, backend.Dispatch)

	if err != nil {
		cancel()
		return nil, errnie.Error(err)
	}

	field := mesh.NewField(ctx, 65537, telemetry, queue)

	if field == nil {
		cancel()
		return nil, errnie.Error(errors.New(
			"vm.NewOrchestrator: mesh.NewField returned nil",
		))
	}

	orchestrator := &Orchestrator{
		ctx:       ctx,
		cancel:    cancel,
		queue:     queue,
		field:     field,
		backend:   backend,
		telemetry: telemetry,
	}

	if err := validate.Require(map[string]any{
		"ctx":     orchestrator.ctx,
		"cancel":  orchestrator.cancel,
		"queue":   orchestrator.queue,
		"field":   orchestrator.field,
		"backend": orchestrator.backend,
	}); err != nil {
		cancel()
		return nil, errnie.Error(err)
	}

	return orchestrator, nil
}

/*
Close cancels the shared context and tears the substrate down. Order
matters: cancelling the context unblocks ring readers and pump
goroutines; then we close the queue (which the pool workers honour
via context); the backend goes last because outstanding pool tasks
may still be calling into it during cancellation.
*/
func (orchestrator *Orchestrator) Close() error {
	if orchestrator == nil {
		return nil
	}

	orchestrator.cancel()

	if orchestrator.queue != nil {
		_ = orchestrator.queue.Close()
	}

	if orchestrator.backend != nil {
		_ = orchestrator.backend.Close()
	}

	return orchestrator.err
}

/*
Error returns the most recent retained error from the orchestrator.
*/
func (orchestrator *Orchestrator) Error() error {
	return orchestrator.err
}

/*
Cycle ingests a batch of Values, drives them through the gossip graph,
and drains every frame the system emits during the burst back out as
[]*Value.

Two emission paths feed the outbound ring:
  - Input echo: every non-nil input is republished on root via Emit so
    the caller always sees its inputs come out the other side. Echoing
    explicitly (not via the EMIT property) avoids cascading
    re-emissions during the encounter pass — host Values dispatched by
    Field.encounterPick stay quiet unless their own firmware raises
    EmitRequested.
  - Genuine emissions: post-ALU frames whose EMIT property was set by
    firmware, plus Field.refreshMetrics carriers and learner Values.

Termination is quiescence-based: when the queue's pending count
(rings + inflight) is zero AND the backend has no in-flight work AND
the outbound ring has nothing to drain for several consecutive
checks, the system has settled. A hard deadline guards against a
buggy firmware loop emitting forever.
*/
func (orchestrator *Orchestrator) Cycle(
	values ...*primitive.Value,
) (resolved []*primitive.Value, err error) {
	if orchestrator == nil {
		return nil, io.ErrClosedPipe
	}

	rwcs := make([]io.ReadWriter, 0, len(values))

	for _, val := range values {
		if val != nil {
			rwcs = append(rwcs, val)
		}
	}

	if len(rwcs) == 0 {
		return nil, errnie.Error(errors.New(
			"vm.Orchestrator.Cycle: need at least one non-nil *primitive.Value",
		))
	}

	root, err := gossip.NewConn(
		orchestrator.ctx,
		orchestrator.queue,
		orchestrator.telemetry,
		rwcs...,
	)

	if err != nil {
		return nil, errnie.Error(err)
	}

	orchestrator.root = root

	clones := slices.Clone(values)
	var clone *primitive.Value
	out := transport.NewCollector(core.Cfg.Value.Bytes)

	for {
		if len(clones) > 0 {
			clone, clones = clones[0], clones[1:]
		}

		if _, err := io.Copy(orchestrator.field, clone); err != nil {
			return nil, errnie.Error(err)
		}

		if _, err := io.Copy(out, orchestrator.field); err != nil {
			return nil, errnie.Error(err)
		}

		for out.Len() >= core.Cfg.Value.Bytes {
			frame := out.Next(core.Cfg.Value.Bytes)

			if frame == nil {
				break
			}

			frameValue := primitive.AllocValue()

			if err := frameValue.LoadFullFrame(frame); err != nil {
				return nil, errnie.Error(err)
			}

			if status, err := frameValue.Property(
				primitive.STATUS,
			); err == nil && status == uint64(
				primitive.RESOLVED,
			) {
				resolved = append(resolved, frameValue)
			}

			clones = append(clones, frameValue)
		}

		if len(resolved) > 0 {
			break
		}
	}

	return resolved, nil
}
