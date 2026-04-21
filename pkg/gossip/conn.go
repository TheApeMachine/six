package gossip

import (
	"context"
	"errors"
	"io"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/telemetry"
	"github.com/theapemachine/six/pkg/transport"
)

/*
Conn is the universal connector. It is an io.ReadWriteCloser that
fans every Write out to a list of attached sinks (other Conns, a
Field, an io.Writer probe), tees the same frame into a telemetry
sink, and submits the decoded Value to the shared pool for an ALU
pass — concurrently and lock-free on the hot path.

Read and Drain consume frames that the system has *emitted*: anything
inside the substrate (post-ALU hooks, Field metric carriers, learner
Values, downstream Conns whose emit ports point back here) writes
those frames in via Emit. Cross-connecting two Conns is one call:

	upstream.AttachSink(downstream)        // upstream.Write → downstream.Write
	downstream.AttachEmit(upstream.Emit)   // downstream emissions → upstream out ring

The fan-out itself runs as queue.Schedule jobs so a slow sink cannot
block the caller or the other sinks. The hot path takes one RLock to
snapshot the sink list — there is no per-sink mutex on Write.
*/
type Conn struct {
	ctx       context.Context
	cancel    context.CancelFunc
	err       atomic.Value
	queue     compute.Scheduler
	telemetry *telemetry.Bridge
	pipeline  *transport.Pipeline
}

/*
NewConn allocates a Conn over the given scheduler. queue must be non-nil:
fan-out, telemetry, and dispatch all hop through it so the caller never
spawns a fresh goroutine on the hot path.
*/
func NewConn(
	ctx context.Context,
	queue compute.Scheduler,
	telemetry *telemetry.Bridge,
	rwcs ...io.ReadWriter,
) (*Conn, error) {
	if queue == nil {
		return nil, errnie.Error(errors.New(
			"gossip Conn requires a non-nil queue",
		))
	}

	if len(rwcs) == 0 {
		return nil, errnie.Error(errors.New(
			"gossip Conn requires at least one read writer",
		))
	}

	ctx, cancel := context.WithCancel(ctx)

	feedback := transport.NewFeedback(ctx, queue, telemetry)
	pipeline := transport.NewPipeline(ctx, append([]io.ReadWriter{feedback}, rwcs...)...)

	conn := &Conn{
		ctx:       ctx,
		cancel:    cancel,
		queue:     queue,
		telemetry: telemetry,
		pipeline:  pipeline,
	}

	return conn, errnie.Error(validate.Require(map[string]any{
		"ctx":      conn.ctx,
		"cancel":   conn.cancel,
		"queue":    conn.queue,
		"pipeline": pipeline,
	}))
}

func (conn *Conn) Update(components ...io.ReadWriter) {
	conn.pipeline.Update(components...)
}

/*
Read drains exactly one emitted frame from the outbound path. The slice
must hold at least core.Cfg.Value.Bytes (e.g. 1024); otherwise ErrShortBuffer.
Only that prefix is passed to the underlying reader so callers using
io.Copy with a large buffer still advance one Value frame per Read.
*/
func (conn *Conn) Read(p []byte) (int, error) {
	if conn == nil || conn.pipeline == nil {
		return 0, io.ErrClosedPipe
	}

	if len(p) < core.Cfg.Value.Bytes {
		return 0, errors.Join(
			io.ErrShortBuffer,
			errors.New("conn.Read: len(p) < core.Cfg.Value.Bytes"),
		)
	}

	return conn.pipeline.Read(p[:core.Cfg.Value.Bytes])
}

/*
Write fans p to every attached sink, the telemetry writer, and the
ALU dispatch path. Each fan-out leg is scheduled on the shared queue
so a slow sink (or a slow dispatch) never blocks another. p is copied
once because callers reuse the buffer.
*/
func (conn *Conn) Write(p []byte) (int, error) {
	if conn == nil {
		return 0, io.ErrClosedPipe
	}

	if len(p) < core.Cfg.Value.Bytes {
		return 0, io.ErrShortWrite
	}

	if conn.ctx.Err() != nil {
		return 0, io.ErrClosedPipe
	}

	return conn.pipeline.Write(p)
}

/*
Close cancels the Conn's context. Sinks are not closed here; callers own
their own lifetimes (a Field outlives any one Conn that points at it).
*/
func (conn *Conn) Close() error {
	if conn == nil {
		return nil
	}

	conn.cancel()
	conn.pipeline.Close()

	return conn.Error()
}

/*
Error returns the most recent fan-out / dispatch error observed by the
Conn. Errors from telemetry are intentionally swallowed (telemetry is
best-effort) — only sink Write and dispatch failures propagate here.
*/
func (conn *Conn) Error() error {
	if conn == nil {
		return nil
	}

	if v := conn.err.Load(); v != nil {
		if e, ok := v.(error); ok {
			return e
		}
	}

	return nil
}
