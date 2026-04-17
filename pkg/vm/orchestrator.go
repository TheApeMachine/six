package vm

import (
	"context"
	"io"
	"slices"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/gossip"
	"github.com/theapemachine/six/pkg/mesh"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Orchestrator owns the root Field and the pool.Queue shared with that
Field's gossip.Conn. Cycle ingests tokenizer Values into the Field via
the Conn ring; compute dispatch from pool.Stream into Executable tasks
is wired separately when a compute.Backend is registered with the queue.
*/
type Orchestrator struct {
	ctx    context.Context
	cancel context.CancelFunc
	err    error
	queue  *pool.Queue
	field  *mesh.Field
}

/*
NewOrchestrator creates a new orchestrator with a fresh queue and root Field.
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
Cycle pushes each non-nil Value's wire frame into the Conn ring, closes
the Conn so the ring read side can finish, then drains Conn.Read into
the root Field. Conn.Read returns io.EOF after every full frame (tokenizer
delimiter); io.Copy would stop after the first frame, so the drain is
implemented as an explicit loop that only stops on (0, io.EOF).

The returned slice is a snapshot of the Field population after ingest
(ValueFromWireFrame copies held by the Field).
*/
func (orchestrator *Orchestrator) Cycle(
	values ...*primitive.Value,
) (resolved []*primitive.Value, err error) {
	clones := slices.Clone(values)

	conn, err := gossip.NewConn(orchestrator.ctx, orchestrator.queue, clones...)

	if err != nil {
		return nil, errnie.Error(err)
	}

	defer func() {
		_ = conn.Close()
	}()

	frame := make([]byte, core.Cfg.Value.Bytes)

	for _, value := range clones {
		if value == nil {
			continue
		}

		if _, readErr := value.Read(frame); readErr != nil && readErr != io.EOF {
			return nil, errnie.Error(readErr)
		}

		if _, writeErr := conn.Write(frame); writeErr != nil {
			return nil, errnie.Error(writeErr)
		}
	}

	/*
		Close ends the ring stream so Conn.Read returns (0, EOF) once the
		queued frames drain. Without this, Read blocks forever on an empty
		Vyukov queue.
	*/
	if closeErr := conn.Close(); closeErr != nil {
		return nil, errnie.Error(closeErr)
	}

	/*
		Conn.Read returns io.EOF after each full frame (tokenizer delimiter
		contract). io.Copy treats (n>0, EOF) as end-of-stream and stops
		after one frame, so we drain manually until a true stream EOF (0, EOF).
	*/
	frameIn := make([]byte, core.Cfg.Value.Bytes)

	for {
		n, readErr := conn.Read(frameIn)

		if readErr == io.EOF && n == 0 {
			break
		}

		if readErr != nil && readErr != io.EOF {
			return nil, errnie.Error(readErr)
		}

		if n > 0 {
			if _, writeErr := orchestrator.field.Write(frameIn[:n]); writeErr != nil {
				return nil, errnie.Error(writeErr)
			}
		}
	}

	resolved = slices.Clone(orchestrator.field.Values())

	return resolved, nil
}
