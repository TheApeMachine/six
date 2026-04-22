package gossip

import (
	"context"
	"errors"
	"io"

	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
	"github.com/theapemachine/six/pkg/transport"
)

/*
Conn is a specialized pipeline that acts as a nested gossip protocol.
It does so by feeding data into the given sink on Write, while Read
will perform a "fold" operation on the data. A fold is essentially
an io.Copy over two objects (Values) that are designed to interact
when one is written to the other. It also includes a feedback (Tee)
which is used to route data to a telemetry bridge and a compute queue.
*/
type Conn struct {
	ctx       context.Context
	cancel    context.CancelFunc
	err       error
	queue     compute.Scheduler
	telemetry *telemetry.Bridge
	pipeline  *transport.Pipeline
}

/*
NewConn allocates a pipeline that feeds data into the given sink
on Write, while Read will perform a "fold" operation on the data.
*/
func NewConn(
	ctx context.Context,
	queue compute.Scheduler,
	telemetry *telemetry.Bridge,
	sink io.Writer,
	rwcs ...io.ReadWriter,
) (*Conn, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if err := validate.Require(map[string]any{
		"queue": queue,
	}); err != nil {
		return nil, errnie.Error(err)
	}

	ctx, cancel := context.WithCancel(ctx)

	writers := make([]io.Writer, 0, 3)
	if sink != nil {
		writers = append(writers, sink)
	}
	writers = append(writers, queue)
	if telemetry != nil {
		writers = append(writers, telemetry)
	}

	pipeline := transport.NewPipeline(
		ctx,
		io.MultiWriter(writers...),
		rwcs...,
	)

	return &Conn{
		ctx:       ctx,
		cancel:    cancel,
		queue:     queue,
		telemetry: telemetry,
		pipeline:  pipeline,
	}, nil
}

func (conn *Conn) Update(components ...io.Reader) {
	errnie.Trace("gossip.Conn.Update")

	if conn == nil {
		return
	}

	conn.pipeline.Update(components...)
}

/*
Fold implements the true gossip protocol mechanic: it arbitrarily connects
things together that need to interact/communicate. By writing one value
to another via io.Copy(value1, value2), the signals, context, gradient,
and properties regions of one value are written to another, allowing it
to react to the other's state.
*/
func (conn *Conn) Fold(value1, value2 io.ReadWriter) error {
	if conn == nil {
		return io.ErrClosedPipe
	}
	_, err := io.Copy(value1, value2)
	return err
}

func (conn *Conn) Read(p []byte) (int, error) {
	errnie.Trace("gossip.Conn.Read")

	if conn == nil || conn.pipeline == nil {
		return 0, io.ErrClosedPipe
	}

	if len(p) < core.Cfg.Value.Bytes {
		return 0, io.ErrShortBuffer
	}

	return conn.pipeline.Read(p)
}

func (conn *Conn) Write(p []byte) (int, error) {
	errnie.Trace("gossip.Conn.Write")

	if conn == nil || conn.pipeline == nil {
		return 0, io.ErrClosedPipe
	}

	if len(p) < core.Cfg.Value.Bytes {
		return 0, io.ErrShortWrite
	}

	return conn.pipeline.Write(p)
}

func (conn *Conn) Close() (err error) {
	if conn == nil {
		return
	}

	conn.cancel()

	if err = conn.pipeline.Close(); err != nil {
		err = errors.Join(err, errnie.Error(err))
	}

	return err
}

/*
Swarm creates an ephemeral communication substrate for a given set of values.
This is important for when we have a swarm of unsupervised learning programmed
values, because each value in that swarm will have to collect the structural
components of other values, write it into their context field, and communicate
to figure out what are the N most common structures.
*/
func Swarm(ctx context.Context, queue compute.Scheduler, values []*primitive.Value) error {
	if len(values) < 2 {
		return nil
	}

	conn, err := NewConn(ctx, queue, nil, io.Discard)
	if err != nil {
		return err
	}
	defer conn.Close()

	for _, v1 := range values {
		for _, v2 := range values {
			if v1 != v2 {
				_ = conn.Fold(v1, v2)
			}
		}
	}
	return nil
}

/*
Error returns the most recent error that occurred during reading or writing.
*/
func (conn *Conn) Error() error {
	if conn == nil {
		return nil
	}

	return conn.err
}
