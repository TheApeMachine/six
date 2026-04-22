package gossip

import (
	"context"
	"errors"
	"io"

	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
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
Error returns the most recent error that occurred during reading or writing.
*/
func (conn *Conn) Error() error {
	if conn == nil {
		return nil
	}

	return conn.err
}
