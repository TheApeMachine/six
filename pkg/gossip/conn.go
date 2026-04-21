package gossip

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/telemetry"
	"github.com/theapemachine/six/pkg/transport"
)

type Conn struct {
	ctx       context.Context
	cancel    context.CancelFunc
	err       atomic.Value
	queue     compute.Scheduler
	telemetry *telemetry.Bridge
	pipeline  *transport.Feedback
}

func NewConn(
	ctx context.Context,
	queue compute.Scheduler,
	telemetry *telemetry.Bridge,
	rwcs ...io.ReadWriter,
) (*Conn, error) {
	ctx, cancel := context.WithCancel(ctx)

	if queue == nil {
		errnie.Error(errors.New("gossip.NewConn: queue is nil"))
		cancel()
		return nil, errors.New("queue is nil")
	}

	pipeline := transport.NewPipeline(ctx, rwcs...)
	feedback := transport.NewFeedback(ctx, pipeline, io.MultiWriter(queue, telemetry))

	conn := &Conn{
		ctx:       ctx,
		cancel:    cancel,
		queue:     queue,
		telemetry: telemetry,
		pipeline:  feedback,
	}

	return conn, errnie.Error(validate.Require(map[string]any{
		"ctx":      conn.ctx,
		"cancel":   conn.cancel,
		"queue":    conn.queue,
		"pipeline": pipeline,
	}))
}

func (conn *Conn) Update(components ...io.ReadWriter) {
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

	return conn.pipeline.Read(p[:core.Cfg.Value.Bytes])
}

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

func (conn *Conn) Close() error {
	if conn == nil {
		return nil
	}

	conn.cancel()
	conn.pipeline.Close()

	return conn.Error()
}

func (conn *Conn) Error() error {
	if conn == nil {
		return io.ErrClosedPipe
	}

	err := conn.err.Load()

	if err == nil {
		return nil
	}

	if e, ok := err.(error); ok {
		return e
	}

	return fmt.Errorf("%v", err)
}
