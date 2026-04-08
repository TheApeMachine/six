package transport

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/theapemachine/six/pkg/core"
)

/*
FramedBytePipe is a single endpoint whose Write side accepts raw bytes and
whose Read side emits fixed-width Value wire frames. CloseWrite must run when
the raw source is finished so Read can unblock after the internal buffer drains.
*/
type FramedBytePipe interface {
	io.ReadWriter
	CloseWrite() error
}

var ErrPipelineNoEgress = errors.New("transport.Pipeline: Read requires egress mode")

/*
copyDrainBufPool backs copyContext so each pipeline drain does not allocate a
fresh 32 KiB buffer.
*/
var copyDrainBufPool = sync.Pool{
	New: func() any {
		return make([]byte, 32*1024)
	},
}

/*
Pipeline moves bytes from its Write side through frame, invokes Publish for
each logical record, and optionally exposes duplicate wire frames on its Read
side for chaining (io.Copy(next, pipeline)).

When frame implements PublishedValueDrainer (vm.Tokenizer), the drain path
publishes live *primitive.Value instances and only serializes to the optional
egress tee. Other frames still use Stream and ValueFromWireFrame.

CloseWrite must follow the last Write when the byte source ends; Finish (and
Close, same behavior) waits for the background drain and runs ResetAfterEOF
on the frame when present (for example *vm.Tokenizer).

With egress enabled, the same frames sent to publishers are readable on Read
(full Value.Bytes records), so another Pipeline or any io.Writer can io.Copy
from this stage. A Pipeline satisfies FramedBytePipe and can therefore be the
frame argument to NewPipeline.
*/
type Pipeline struct {
	ctx context.Context

	frame  FramedBytePipe
	stream *Stream

	publishers []Publishable

	egressR io.ReadCloser
	egressW *io.PipeWriter

	wg sync.WaitGroup

	drainMu  sync.Mutex
	drainErr error

	closeOnce sync.Once
}

/*
NewPipeline builds a pipeline. When egress is true, Read returns a copy of
each frame sent to publishers (for nesting or tapping); when false, Read
returns ErrPipelineNoEgress and publishers alone consume frames.

ctx is honored on Write and Read via ctx.Err before delegating to frame /
egress. The background drain uses copyContext so cancellation stops the
stream ingest path once the frame reader unblocks.
*/
func NewPipeline(
	ctx context.Context,
	egress bool,
	frame FramedBytePipe,
	publishers ...Publishable,
) (*Pipeline, error) {
	if len(publishers) == 0 {
		return nil, errors.New("transport.NewPipeline: need at least one Publishable")
	}

	pubs := append([]Publishable(nil), publishers...)

	var stream *Stream
	var err error

	if _, direct := frame.(PublishedValueDrainer); !direct {
		stream, err = NewStream(core.Cfg.Value.Bytes, pubs...)

		if err != nil {
			return nil, err
		}
	}

	p := &Pipeline{
		ctx:        ctx,
		frame:      frame,
		stream:     stream,
		publishers: pubs,
	}

	if egress {
		r, w := io.Pipe()
		p.egressR = r
		p.egressW = w

		if stream != nil {
			stream.SetFrameTee(w)
		}
	}

	p.wg.Add(1)

	go p.runDrain()

	return p, nil
}

func (pipeline *Pipeline) runDrain() {
	defer pipeline.wg.Done()

	var drainErr error

	if drainer, ok := pipeline.frame.(PublishedValueDrainer); ok {
		var tee io.Writer

		if pipeline.egressW != nil {
			tee = pipeline.egressW
		}

		drainErr = drainer.DrainPublishedValues(
			pipeline.ctx,
			"",
			pipeline.publishers,
			tee,
		)
	} else {
		_, drainErr = copyContext(pipeline.ctx, pipeline.stream, pipeline.frame)

		if pipeline.stream != nil {
			drainErr = errors.Join(drainErr, pipeline.stream.Close())
		}
	}

	if pipeline.egressW != nil {
		_ = pipeline.egressW.Close()
	}

	pipeline.drainMu.Lock()
	pipeline.drainErr = drainErr
	pipeline.drainMu.Unlock()
}

func (pipeline *Pipeline) Write(p []byte) (n int, err error) {
	if pipeline == nil {
		return 0, errors.New("transport.Pipeline.Write: nil Pipeline")
	}

	if err := pipeline.ctx.Err(); err != nil {
		return 0, err
	}

	return pipeline.frame.Write(p)
}

func (pipeline *Pipeline) Read(p []byte) (n int, err error) {
	if pipeline == nil {
		return 0, errors.New("transport.Pipeline.Read: nil Pipeline")
	}

	if pipeline.egressR == nil {
		return 0, ErrPipelineNoEgress
	}

	if err := pipeline.ctx.Err(); err != nil {
		return 0, err
	}

	return pipeline.egressR.Read(p)
}

/*
CloseWrite ends the raw ingest side (for example tokenizer pipe writer) so
the drain loop can complete.
*/
func (pipeline *Pipeline) CloseWrite() error {
	if pipeline == nil || pipeline.frame == nil {
		return nil
	}

	return pipeline.frame.CloseWrite()
}

/*
LoadFrom copies r through the pipeline, ends the write side, then finishes.
*/
func (pipeline *Pipeline) LoadFrom(r io.Reader) (err error) {
	if pipeline == nil {
		return errors.New("transport.Pipeline.LoadFrom: nil Pipeline")
	}

	_, err = io.Copy(pipeline, r)
	err = errors.Join(err, pipeline.CloseWrite())
	err = errors.Join(err, pipeline.Finish())

	return err
}

/*
Finish waits for the background drain, closes the egress reader when present,
resets the frame when it implements ResetAfterEOF, and is idempotent.

Publishable sinks may enqueue follow-on work (for example kadabra.Store on a
pool.Queue) that outlives the drain goroutine. A caller that needs that work
finished before treating bytes as fully applied must flush the same Queue
after Finish (vm.Machine.Load does this).
*/
func (pipeline *Pipeline) Finish() error {
	if pipeline == nil {
		return nil
	}

	var joined error

	pipeline.closeOnce.Do(func() {
		if pipeline.egressR != nil {
			joined = errors.Join(joined, pipeline.egressR.Close())
		}

		pipeline.wg.Wait()

		pipeline.drainMu.Lock()
		joined = errors.Join(joined, pipeline.drainErr)
		pipeline.drainMu.Unlock()

		if nested, nestedOK := pipeline.frame.(*Pipeline); nestedOK {
			joined = errors.Join(joined, nested.Finish())

			return
		}

		if downstreamReset, ok := pipeline.frame.(interface {
			ResetAfterEOF()
		}); ok {
			downstreamReset.ResetAfterEOF()
		}
	})

	return joined
}

/*
Close implements io.Closer as an alias for Finish so Pipeline can serve as an
io.ReadWriteCloser stage. You must still call CloseWrite after the last Write
before drain can complete.
*/
func (pipeline *Pipeline) Close() error {
	return pipeline.Finish()
}

/*
ResetAfterEOF forwards to the framed endpoint when it supports recycling, so a
Pipeline can itself act as a FramedBytePipe stage inside an outer Pipeline.
*/
func (pipeline *Pipeline) ResetAfterEOF() {
	if pipeline == nil {
		return
	}

	if downstreamReset, ok := pipeline.frame.(interface {
		ResetAfterEOF()
	}); ok {
		downstreamReset.ResetAfterEOF()
	}
}

func copyContext(ctx context.Context, dst io.Writer, src io.Reader) (written int64, err error) {
	buf := copyDrainBufPool.Get().([]byte)
	buf = buf[:cap(buf)]

	defer func() {
		copyDrainBufPool.Put(buf)
	}()

	for {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return written, err
			}
		}

		nr, rErr := src.Read(buf)

		if nr > 0 {
			nw, wErr := dst.Write(buf[:nr])
			written += int64(nw)

			if wErr != nil {
				return written, wErr
			}

			if nw != nr {
				return written, io.ErrShortWrite
			}
		}

		if rErr != nil {
			if errors.Is(rErr, io.EOF) {
				return written, nil
			}

			return written, rErr
		}
	}
}

var (
	_ FramedBytePipe     = (*Pipeline)(nil)
	_ io.ReadWriteCloser = (*Pipeline)(nil)
)
