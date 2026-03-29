package transport

import (
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
}

/*
NewEmitter wires the pipe ends: writes go to target (pw). Reads use the
forward reader (pr). Every byte consumed is processed and then explicitly
written back to target to re-insert the (now potentially mutated) frame
into the stream ring.
*/
func NewEmitter(forward io.Reader, target io.Writer) *Emitter {
	return &Emitter{
		passthrough: target,
		forward:     forward,
	}
}

/*
Read reads from the emitter's forward reader into p (one ByteSize wire frame).

The first Read seeds wait from the forward reader. Later reads fold additional
bytes into wait via Value.Write, then serialize the folded Value
into p. Callers must pass len(p) >= ByteSize (as Stream does via ReadFull).
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

		// Re-insert the unmodified initial frame back into the ring
		if _, err = emitter.passthrough.Write(p); err != nil {
			errnie.Error(err)
			return 0, err
		}

		emitter.wait = primitive.BytesToValue(p).Clone()
		return primitive.ByteSize, nil
	}

	// One Read consumes exactly one wire frame.
	var next [primitive.ByteSize]byte
	if _, err = io.ReadFull(emitter.forward, next[:]); err != nil {
		return 0, err
	}

	// Write mutates BOTH emitter.wait AND next[:] (the incoming partner).
	if _, err = emitter.wait.Write(next[:]); err != nil {
		return 0, err
	}

	// Re-insert the mutated partner (next) back into the ring!
	// This physically allows Tombstone/Viral to survive and spread.
	if _, err = emitter.passthrough.Write(next[:]); err != nil {
		return 0, err
	}

	return emitter.wait.Read(p)
}

/*
Write writes to the emitter's passthrough writer. The first frame seeds
wait; subsequent frames are merged into wait with Value.Write.
*/
func (emitter *Emitter) Write(p []byte) (n int, err error) {
	return emitter.passthrough.Write(p)
}
