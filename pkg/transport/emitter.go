package transport

import (
	"errors"
	"io"

	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Emitter creates a side-channel for emitting the contents of a
read/write stream. It is used to perform the folding operation.
*/
type Emitter struct {
	passthrough io.Writer
	forward     io.Reader
	wait        *primitive.Value
	scratch     *primitive.Value
}

/*
NewEmitter wires the pipe ends: writes go to target (pw). Reads use the
forward reader (pr).

The emitter keeps one in-memory "wait" Value outside the ring and rotates it
with every consumed partner. This preserves a constant population without
pinning the same catalyst frame forever: after a fold, the old wait is written
back into the ring and the just-collided partner becomes the new wait.
*/
func NewEmitter(forward io.Reader, target io.Writer) *Emitter {
	return &Emitter{
		passthrough: target,
		forward:     forward,
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
	p = p[:primitive.ByteSize]

	if emitter.wait == nil {
		if _, err = io.ReadFull(emitter.forward, p); err != nil {
			errnie.Error(err)
			return 0, err
		}

		emitter.wait = primitive.BytesToValue(p)
		return primitive.ByteSize, nil
	}

	if emitter.scratch == nil {
		emitter.scratch, err = primitive.NewValue(nil)
		if err != nil {
			return 0, err
		}
	}

	var next [primitive.ByteSize]byte
	if _, err = io.ReadFull(emitter.forward, next[:]); err != nil {
		return 0, err
	}

	if err = emitter.scratch.ApplyWireFrame(next[:]); err != nil {
		return 0, err
	}

	oldWait := emitter.wait
	newWait := emitter.scratch

	if err = oldWait.Fold(newWait); err != nil {
		return 0, err
	}

	if err = primitive.ValueToBytes(oldWait, next[:]); err != nil {
		return 0, err
	}
	if _, err = emitter.passthrough.Write(next[:]); err != nil {
		return 0, err
	}

	emitter.wait = newWait
	emitter.scratch = oldWait

	if err = primitive.ValueToBytes(emitter.wait, p); err != nil {
		return 0, err
	}

	return primitive.ByteSize, nil
}

/*
Write writes to the emitter's passthrough writer. Folding happens on Read;
Write simply injects new wire frames into the ring.
*/
func (emitter *Emitter) Write(p []byte) (n int, err error) {
	return emitter.passthrough.Write(p)
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
	if emitter.scratch != nil {
		if err := emitter.scratch.Close(); err != nil {
			errs = append(errs, err)
		}
		emitter.scratch = nil
	}
	return errors.Join(errs...)
}
