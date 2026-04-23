package gossip

import (
	"context"
	"io"

	"github.com/smallnest/ringbuffer"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/telemetry"
)

type Folder interface {
	Update(components ...io.ReadWriteCloser)
}

/*
Conn is a specialized pipeline that acts as a nested gossip protocol.
It does so by feeding data into the given sink on Write, while Read
will perform a "fold" operation on the data. A fold is essentially
an io.Copy over two objects (Values) that are designed to interact
when one is written to the other. It also includes a feedback (Tee)
which is used to route data to a telemetry bridge and a compute queue.
*/
type Conn struct {
	ctx        context.Context
	cancel     context.CancelFunc
	err        error
	components []io.ReadWriteCloser
	backend    *compute.Backend
	telemetry  *telemetry.Bridge
}

/*
NewConn allocates a pipeline that feeds data into the given sink
on Write, while Read will perform a "fold" operation on the data.
*/
func NewConn(
	ctx context.Context,
	backend *compute.Backend,
	telemetry *telemetry.Bridge,
) (*Conn, error) {
	ctx, cancel := context.WithCancel(ctx)

	return &Conn{
		ctx:        ctx,
		cancel:     cancel,
		components: make([]io.ReadWriteCloser, 0),
		backend:    backend,
		telemetry:  telemetry,
	}, nil
}

/*
Update adds more readers and writers to the connection.
*/
func (conn *Conn) Update(components ...io.ReadWriteCloser) {
	errnie.Trace("gossip.Conn.Update")
	conn.components = append(conn.components, components...)
}

func (conn *Conn) Fold() {
	readers := make([]io.Reader, 0, len(conn.components))
	writers := make([]io.Writer, 0, len(conn.components))

	for _, component := range conn.components {
		readers = append(readers, component)
		writers = append(writers, component)
	}

	var (
		err error
	)

	// Perform the fold operation.
	if _, err = ringbuffer.New(
		core.Cfg.Value.Bytes,
	).Copy(
		io.MultiWriter(writers...),
		io.MultiReader(readers...),
	); err != nil {
		errnie.Error(err)
	}

	if _, err = ringbuffer.New(
		core.Cfg.Value.Bytes,
	).Copy(
		io.MultiWriter(conn.backend, conn.telemetry),
		io.MultiReader(readers...),
	); err != nil {
		errnie.Error(err)
	}
}

func (conn *Conn) Read(p []byte) (int, error) {
	errnie.Trace("gossip.Conn.Read")
	conn.Fold()
	return conn.backend.Read(p)
}

func (conn *Conn) Write(p []byte) (int, error) {
	errnie.Trace("gossip.Conn.Write")

	return io.MultiWriter(
		conn.backend, conn.telemetry,
	).Write(p)
}

/*
Close closes the connection.
*/
func (conn *Conn) Close() (err error) {
	if conn == nil {
		return
	}

	conn.cancel()

	return nil
}

/*
Error implements the error interface.
*/
func (conn *Conn) Error() string {
	if conn == nil {
		return ""
	}

	return conn.err.Error()
}
