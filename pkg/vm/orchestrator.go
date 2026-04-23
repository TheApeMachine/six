package vm

import (
	"context"
	"io"

	"github.com/smallnest/ringbuffer"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/gossip"
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
	conn      *gossip.Conn
	backend   *compute.Backend
	telemetry *telemetry.Bridge
	output    *transport.Collector
	field     *mesh.Field
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
	backend := compute.NewBackend(ctx)
	conn, err := gossip.NewConn(ctx, backend, telemetry)

	if err != nil {
		cancel()
		return nil, errnie.Error(err)
	}

	orchestrator := &Orchestrator{
		ctx:       ctx,
		cancel:    cancel,
		conn:      conn,
		backend:   backend,
		telemetry: telemetry,
		output:    output,
		field:     mesh.NewField(ctx, 65537),
	}

	if err := validate.Require(map[string]any{
		"ctx":     orchestrator.ctx,
		"cancel":  orchestrator.cancel,
		"conn":    orchestrator.conn,
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

	if orchestrator.backend != nil {
		return orchestrator.backend.Close()
	}

	return nil
}

/*
Error implements the error interface.
*/
func (orchestrator *Orchestrator) Error() string {
	if orchestrator.err == nil {
		return ""
	}

	return orchestrator.err.Error()
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

	orchestrator.conn.Update(orchestrator.field)

	if _, err = ringbuffer.New(core.Cfg.Value.Bytes).Copy(
		orchestrator.conn, io.MultiReader(rwcs...),
	); err != nil {
		errnie.Error(err)
	}

	for {
		select {
		case <-orchestrator.ctx.Done():
			return nil, orchestrator.ctx.Err()
		case value := <-orchestrator.output.Next(core.Cfg.Value.Bytes):
			if value == nil {
				continue
			}

			// We intercept RESOLVED values directly from the output stream.
			status, _ := value.Property(primitive.STATUS)

			if status == uint64(primitive.RESOLVED) {
				resolved = append(resolved, value)
			}
		default:
			if _, err = ringbuffer.New(
				core.Cfg.Value.Bytes*64,
			).WithCancel(orchestrator.ctx).Copy(
				orchestrator.output, orchestrator.conn,
			); err != nil {
				return nil, errnie.Error(err)
			}

			if _, err = ringbuffer.New(
				core.Cfg.Value.Bytes*64,
			).WithCancel(orchestrator.ctx).Copy(
				orchestrator.conn, orchestrator.output,
			); err != nil {
				return nil, errnie.Error(err)
			}
		}
	}
}
