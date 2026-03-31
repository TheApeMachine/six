package transport

import (
	"errors"
	"io"
	"sync"

	"github.com/theapemachine/six/pkg/primitive"
)

var bufPool = sync.Pool{
	New: func() any {
		buf := make([]byte, primitive.ByteSize)
		return buf
	},
}

/*
Emitter creates a side-channel for emitting the contents of a
read/write stream. It is used to perform the folding operation.
*/
type Emitter struct {
	passthrough io.Reader
	emit        io.Writer
	tee         io.Reader
	wait        *primitive.Value
}

/*
NewEmitter wires the pipe ends: writes go to target (pw). Reads use the
forward reader (pr).

The emitter keeps one in-memory "wait" Value outside the ring and rotates it
with every consumed partner. This preserves a constant population without
pinning the same catalyst frame forever: after a fold, the old wait is written
back into the ring and the just-collided partner becomes the new wait.
*/
func NewEmitter(passthrough io.Reader, emit io.Writer) *Emitter {
	return &Emitter{
		passthrough: passthrough,
		emit:        emit,
		tee:         io.NopCloser(io.TeeReader(passthrough, emit)),
	}
}

/*
Read reads from the emitter's forward reader into p (one ByteSize wire frame).

The first Read seeds wait from the forward reader without duplicating the
resident frame back into the ring. Later reads fold the current wait with the
next partner directly in Value space, rotate the partner into wait, and
reinsert the mutated previous wait into the ring.
*/
func (emitter *Emitter) Read(p []byte) (n int, err error) {
	if len(p) < primitive.ByteSize {
		return 0, io.ErrShortBuffer
	}

	// We make a new buffer for the incoming Value.
	buf := bufPool.Get().([]byte)
	defer bufPool.Put(buf)

	// We read the side-channel so we have a copy of the Value that
	// has already been passed through unmodified.
	if _, err = io.ReadFull(emitter.tee, buf); err != nil {
		return 0, err
	}

	// We convert the incoming data to a Value.
	next := primitive.BytesToValue(buf)

	if emitter.wait == nil {
		emitter.wait = next
		return emitter.wait.Read(p)
	}

	if !emitter.wait.HasProgram() {
		oldWait := emitter.wait
		emitter.wait = next
		n, err = emitter.wait.Read(p)
		_ = oldWait.Close()
		return n, err
	}

	// We perform the fold, this is now happening entirely on
	// copies of the original Values.
	if _, err = emitter.wait.Write(buf); err != nil {
		emitter.wait.Close()
		return n, err
	}

	// The Value makes sure that whenever Read is called on it, it will
	// produce the result of the fold.
	if n, err = emitter.wait.Read(p); err != nil {
		emitter.wait.Close()
		return n, err
	}

	// We rotate the wait to the incoming Value, so it can wait on the
	// next one and fold with it.
	emitter.wait.Close()
	emitter.wait = next

	return n, err
}

/*
Write writes to the emitter's passthrough writer. Folding happens on Read;
Write simply injects new wire frames into the ring.
*/
func (emitter *Emitter) Write(p []byte) (n int, err error) {
	// We need to set the first wait, which is the value being
	// held back to fold with the next incoming value.
	if emitter.wait == nil {
		emitter.wait = primitive.BytesToValue(p)
	}

	// This is where the original value is passed through completely
	// unmodified, so we do not need to think about it anymore.
	return emitter.emit.Write(p)
}

// Close releases any Values retained by the emitter.
func (emitter *Emitter) Close() error {
	if emitter == nil {
		return nil
	}

	var errs []error

	if emitter.wait != nil {
		if err := emitter.wait.Close(); err != nil {
			errs = append(errs, err)
		}

		emitter.wait = nil
	}

	return errors.Join(errs...)
}
