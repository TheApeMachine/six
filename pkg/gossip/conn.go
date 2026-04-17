package gossip

import (
	"context"
	"errors"
	"io"

	"github.com/theapemachine/six/pkg/compute/programmer"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/data"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
)

/*
QueueScheduler is the pool-facing side of a Conn: it submits work like
pool.Queue and accepts raw bytes into the pool stream ring (io.Writer)
so telemetry and tokenizer paths can observe the same frames without
sharing the task rings used for Schedule.
*/
type QueueScheduler interface {
	programmer.Scheduler
	io.Writer
}

/*
Conn is a single io.ReadWriteCloser that bundles a set of
Values into a single stream. It is the terminal stage of the
gossip pipeline.
*/
type Conn struct {
	ctx     context.Context
	cancel  context.CancelFunc
	err     error
	forward io.Writer
	tee     io.Reader
	queue   QueueScheduler
	values  []*primitive.Value
	stage   *primitive.Value
}

/*
NewConn allocates a Conn over the given bundle of Values. queue must be
non-nil: it is both the task scheduler and the destination for tee'd
wire frames (pool.Queue satisfies QueueScheduler).
*/
func NewConn(
	ctx context.Context,
	queue QueueScheduler,
	values ...*primitive.Value,
) (*Conn, error) {
	if queue == nil {
		return nil, errnie.Error(errors.New(
			"gossip Conn requires a non-nil queue",
		))
	}

	ctx, cancel := context.WithCancel(ctx)

	forward, err := data.NewRing(ctx, data.RingCapacity)

	if err != nil {
		cancel()

		return nil, errnie.Error(err)
	}

	conn := &Conn{
		ctx:    ctx,
		cancel: cancel,
		forward: io.MultiWriter(
			forward,
			telemetry.NewClient(ctx, core.Cfg.TelemetryWebSocketURL),
		),
		tee: io.TeeReader(
			forward,
			io.MultiWriter(
				telemetry.NewClient(ctx, core.Cfg.TelemetryWebSocketURL),
				queue,
			),
		),
		queue:  queue,
		values: values,
		stage:  nil,
	}

	if err := validate.Require(map[string]any{
		"ctx":     conn.ctx,
		"cancel":  conn.cancel,
		"forward": conn.forward,
		"queue":   conn.queue,
	}); err != nil {
		cancel()

		return nil, errnie.Error(err)
	}

	return conn, nil
}

/*
Close tears down the Conn. Bundled Values are not closed here; they
are owned by whoever passed them in (usually the Field that spawned
them) and may be shared across several Conns. The staging Value is
released.
*/
func (conn *Conn) Close() error {
	if conn == nil {
		return nil
	}

	conn.cancel()

	if conn.stage != nil {
		conn.stage.Close()
		conn.stage = nil
	}

	return conn.err
}

/*
Values returns the bundled Values passed to NewConn.
*/
func (conn *Conn) Values() []*primitive.Value {
	if conn == nil {
		return nil
	}

	return conn.values
}

/*
Error returns the Conn's retained error, if any.
*/
func (conn *Conn) Error() error {
	if conn == nil {
		return nil
	}

	return conn.err
}

/*
Update sets the Conn's bundled Values.
*/
func (conn *Conn) Update(values []*primitive.Value) {
	if conn == nil {
		return
	}

	conn.values = values
}

/*
Write is the in-band dispatch path. It decodes the inbound frame
into a transient staging Value (the only endian- and alignment-safe
way to consume arbitrary byte buffers), copies the inbound frame's
signals+context+gradient+properties (48 contiguous words) into each
bundled Value's asset region, then submits one resident-program
Executable per bundled Value to the pool.

The pool worker dequeues the task, receives the Executable, and
hands it to the registered dispatch (compute.Backend.Dispatch) which
picks a substrate and runs the Value's program word against its
freshly staged Asset region. Write is fire-and-forget: it returns
once the staging is finished and the tasks are submitted, not after
the ALU completes.
*/
func (conn *Conn) Write(p []byte) (n int, err error) {
	if conn == nil {
		return 0, io.ErrClosedPipe
	}

	return conn.forward.Write(p)
}

/*
Read returns the next bundled Value's wire frame in round-robin
order. Round-robin keeps any one Value from starving the rest under
sustained read pressure. Each Read returns exactly one frame
(Value.Read signals io.EOF as a frame delimiter, matching the
tokenizer contract). For io.Copy and io.LimitReader, wrap with
FrameDelimitedReader so per-frame EOF is not treated as end-of-stream.

After serialising the frame, Read forks the Value into the firmware
chain via the pool. The caller gets the raw frame immediately (the
Field stores it, telemetry tees it) while the ALU catches up in the
background — link, affinity, then resident.
*/
func (conn *Conn) Read(p []byte) (n int, err error) {
	if conn == nil {
		return 0, io.ErrClosedPipe
	}

	if len(p) < core.Cfg.Value.Bytes {
		return 0, io.ErrShortBuffer
	}

	teeValue := primitive.AllocValue()

	if conn.stage != nil {
		if _, err := io.Copy(teeValue, conn.stage); err != nil {
			teeValue.Close()

			return 0, errnie.Error(err)
		}

		conn.stage.Close()
		conn.stage = nil
	}

	frame := make([]byte, core.Cfg.Value.Bytes)

	if _, err := io.ReadFull(conn.tee, frame); err != nil {
		teeValue.Close()

		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return 0, io.EOF
		}

		return 0, errnie.Error(err)
	}

	if _, err := teeValue.Write(frame); err != nil {
		teeValue.Close()

		return 0, errnie.Error(err)
	}

	n, err = teeValue.Read(p)

	if err != nil && err != io.EOF {
		teeValue.Close()

		return n, errnie.Error(err)
	}

	if _, werr := conn.queue.Write(p[:n]); werr != nil {
		teeValue.Close()

		return n, errnie.Error(werr)
	}

	conn.stage = teeValue

	return n, err
}
