package vm

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/smallnest/ringbuffer"
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
	ctx          context.Context
	cancel       context.CancelFunc
	err          error
	queue        *compute.Queue
	field        *mesh.Field
	backend      *compute.Backend
	telemetry    *telemetry.Bridge
	output       *transport.Collector
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

	output := transport.NewCollector()
	queue := compute.NewQueue(ctx)

	orchestrator := &Orchestrator{
		ctx:       ctx,
		cancel:    cancel,
		queue:     queue,
		field:     mesh.NewField(ctx, 65537, telemetry, queue),
		backend:   compute.NewBackend(ctx, queue),
		telemetry: telemetry,
		output:    output,
	}

	// Wire the post-ALU outbound publisher: any Association Values minted
	// by the kernel post-hook (see pkg/compute/postexec.go) flow back into
	// our root field as honest residents. Without this the hook's emissions
	// would be orphaned and the structural graph from the README "Signals"
	// algorithm would never accumulate.
	orchestrator.backend.SetEmitter(orchestrator)

	if err := validate.Require(map[string]any{
		"ctx":     orchestrator.ctx,
		"cancel":  orchestrator.cancel,
		"queue":   orchestrator.queue,
		"field":   orchestrator.field,
		"backend": orchestrator.backend,
		"output":  orchestrator.output,
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
Error returns the most recent orchestrator error.
*/
func (orchestrator *Orchestrator) Error() error {
	if orchestrator == nil {
		return nil
	}

	return orchestrator.err
}

/*
Emit publishes a freshly-minted Value into the orchestrator's root field
so it gets routed by affinity into the matching community. Used by the
kernel post-ALU hook (Backend.updateKernels → runStructuralPostExec) to
land structural Association Values in the substrate without any out-of-
band wiring.

The Value is consumed: Field.Write copies the wire frame into a
freshly-allocated resident, so we free the caller's copy immediately to
keep the arena healthy. Errors during routing are returned so the hook
can fall back to freeing without retry storms.
*/
func (orchestrator *Orchestrator) Emit(value *primitive.Value) error {
	if orchestrator == nil || value == nil || orchestrator.field == nil {
		if value != nil {
			primitive.FreeValue(value)
		}

		return nil
	}

	_, err := orchestrator.field.Write(value.Bytes())
	primitive.FreeValue(value)

	if err != nil {
		return errnie.Error(err)
	}

	return nil
}

/*
Cycle is the processing loop the system uses to process Values.
It starts by providing the initial "impulse" to the system by writing
any incoming Values to the pipeline. If anywhere in the pipeline new
Values are emitted, or the program of a Value changes, these Values
should be what comes out of the Read end of the pipeline, and should
subsequently be written back to the pipeline, so gossip.Conn can use
the feedback (which is essentially a tee) those Values to the queue
for ALU execution.
The cycle is considered complete when either there are Values which
have a STATUS word of RESOLVED, or when there are no more Values to
process (which is most likely a failure to resolve the task).
Additionally we can use a timeout deadline to prevent the cycle from
running forever, however that too would be a clear failure mode.
*/
func (orchestrator *Orchestrator) Cycle(
	values ...*primitive.Value,
) (resolved []*primitive.Value, err error) {
	rwcs := make([]io.Reader, 0, len(values))

	for _, value := range values {
		if value != nil {
			rwcs = append(rwcs, value)
		}
	}

	// gossip.Conn acts as a pipeline that moves Values through the system,
	// while taking care of the feedback mechanism that routes Values to
	// the queue for ALU execution, and to the telemetry bridge. The gossip
	// protocol is essentially a nested structure of gossip.Conn instances,
	// so think about it as the community fields using a gossip.Conn pipeline
	// to fold the Values in their community with each other, while the global
	// field uses a gossip.Conn pipeline to fold the Values of each community
	// with the Values in the other communities. This is the mechanism that
	// eventually makes all Values encounter each other, and thus spread the
	// knowledge of the system.
	if _, err = ringbuffer.New(core.Cfg.Value.Bytes).Copy(
		orchestrator.field, io.MultiReader(rwcs...),
	); err != nil {
		return nil, errnie.Error(err)
	}

	for {
		select {
		case <-orchestrator.ctx.Done():
			return nil, orchestrator.ctx.Err()
		default:
			errnie.Trace("orchestrator.Cycle: entering loop")
			var nn int64

			// This makes more sense that it may seem at first glance.
			// What is important to understand is that gossip.Conn only
			// streams out the values that have a reason for being
			// re-cycled, meaning they are written back to the pipeline
			// to make another pass through the ALU. Each cycle effectively
			// is one update, or "tick" of the system.
			if nn, err = ringbuffer.New(core.Cfg.Value.Bytes).Copy(
				orchestrator.field, orchestrator.field,
			); err != nil {
				return nil, errnie.Error(err)
			}

			// Drain anything the queue published as a "computed result" and
			// run the lifecycle finalizer on each frame. evaluateResult is
			// the ONLY producer of STATUS=RESOLVED and the only place that
			// stamps confidence — leaving it unwired (as it was for weeks)
			// meant the cycle never knew when to exit and Readout/Association
			// frames were silently dropped. Decisions:
			//
			//   Resolved && Reinject : reinject the updated frame back into
			//                          the field (so the resident copy gets
			//                          the new context/gradient), AND keep
			//                          a copy in `resolved` for the caller.
			//   Reinject only        : write back into the field; no
			//                          caller-visible result.
			//   neither              : free the frame; the resident copy
			//                          already absorbed whatever it needed
			//                          before the kernel ran.
			buf := make([]byte, core.Cfg.Value.Bytes)
			for {
				if n, err := orchestrator.queue.Read(buf); err == nil && n == core.Cfg.Value.Bytes {
					val := primitive.AllocValue()
					if err := val.LoadFullFrame(buf); err != nil {
						primitive.FreeValue(val)

						continue
					}

					decision := orchestrator.evaluateResult(val)

					if decision.Reinject && orchestrator.field != nil {
						if _, werr := orchestrator.field.Write(val.Bytes()); werr != nil {
							errnie.Error(werr)
						}
					}

					if decision.Resolved {
						resolved = append(resolved, val)

						continue
					}

					primitive.FreeValue(val)
				} else {
					break
				}
			}

			// No more values to process, return the resolved values.
			if nn == 0 && orchestrator.backend.Inflight() == 0 && orchestrator.queue.Len() == 0 {
				return resolved, nil
			} else if nn == 0 {
				time.Sleep(time.Millisecond)
			}
		}
	}
}
