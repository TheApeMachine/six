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
	sink io.ReadWriter,
	rwcs ...io.ReadWriter,
) (*Conn, error) {
	ctx, cancel := context.WithCancel(ctx)

	feedback := transport.NewFeedback(
		ctx, io.MultiWriter(queue, telemetry),
	)

	pipeline := transport.NewPipeline(
		ctx,
		sink,
		append([]io.ReadWriter{feedback}, rwcs...)...,
	)

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

func (conn *Conn) Update(components ...io.Reader) {
	if conn == nil {
		return
	}

	conn.pipeline.Update(components...)
}

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

	return conn.pipeline.Read(p)
}

func (conn *Conn) Write(p []byte) (int, error) {
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
func (conn *Conn) Error() string {
	if conn == nil {
		return ""
	}

	return conn.err.Error()
}
