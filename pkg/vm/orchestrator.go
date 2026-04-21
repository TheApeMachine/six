package vm

import (
	"context"
	"errors"
	"io"
	"runtime"
	"time"

	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/mesh"
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
	queue     *compute.Queue
	field     *mesh.Field
	backend   *compute.Backend
	telemetry *telemetry.Bridge
	output    *transport.Collector
	// DrainTimeout caps the post-quiescence drain loop. Zero uses
	// core.Cfg.System.DrainTimeout, or 100ms when that is also unset.
	DrainTimeout time.Duration
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

	output := transport.NewCollector(core.Cfg.Value.Bytes)

	queue := compute.NewQueue(ctx)
	field := mesh.NewField(ctx, 65537, telemetry, queue)
	backend := compute.NewBackend(ctx, queue, field)

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
		output:    output,
	}

	if err := validate.Require(map[string]any{
		"ctx":       orchestrator.ctx,
		"cancel":    orchestrator.cancel,
		"queue":     orchestrator.queue,
		"field":     orchestrator.field,
		"backend":   orchestrator.backend,
		"output":    orchestrator.output,
		"telemetry": orchestrator.telemetry,
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
		orchestrator.err = errors.Join(
			orchestrator.err,
			orchestrator.queue.Close(),
		)
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

	// 1. Write all initial values to the field
	for _, val := range values {
		if val != nil {
			if _, err := io.CopyN(orchestrator.field, val, int64(core.Cfg.Value.Bytes)); err != nil {
				return nil, errnie.Error(err)
			}
		}
	}

	// 2. Wait for quiescence (ALU to finish processing)
	// We check if the queue has any pending work or if the backend has inflight work.
	// We also need to yield to allow workers to run.
	quiescentCount := 0
	qt := core.Cfg.System.QuiescenceTimeout
	if qt <= 0 {
		qt = 100 * time.Millisecond
	}
	deadline := time.Now().Add(qt)
	buf := make([]byte, core.Cfg.Value.Bytes)
	for {
		if time.Now().After(deadline) {
			break
		}
		pending := orchestrator.queue.Len()
		inflight := orchestrator.backend.Inflight()
		streamLen := orchestrator.queue.StreamLen()

		if pending == 0 && inflight == 0 {
			quiescentCount++
			if quiescentCount > 10 {
				break
			}
		} else {
			quiescentCount = 0
		}

		// Read any available frames from the output to prevent the stream from filling up
		// and blocking the ALU from writing its results.
		if streamLen >= 1 {
			if _, err := io.ReadFull(orchestrator.queue, buf); err != nil {
				if err != io.EOF && err != io.ErrUnexpectedEOF {
					return nil, errnie.Error(err)
				}
			} else {
				orchestrator.output.Write(buf)
			}
		} else {
			runtime.Gosched()
		}
	}

	// 3. Drain any remaining frames
	dt := orchestrator.DrainTimeout
	if dt <= 0 {
		dt = core.Cfg.System.DrainTimeout
	}
	if dt <= 0 {
		dt = 100 * time.Millisecond
	}
	drainDeadline := time.Now().Add(dt)
	for orchestrator.queue.StreamLen() >= 1 {
		if time.Now().After(drainDeadline) {
			break
		}
		if _, err := io.ReadFull(orchestrator.queue, buf); err != nil {
			if err != io.EOF && err != io.ErrUnexpectedEOF {
				return nil, errnie.Error(err)
			}
		} else {
			orchestrator.output.Write(buf)
		}
	}

	// 4. Process all drained frames
	for orchestrator.output.Len() >= core.Cfg.Value.Bytes {
		frame := orchestrator.output.Next(core.Cfg.Value.Bytes)

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
		} else {
			primitive.FreeValue(frameValue)
		}
	}

	return resolved, nil
}
