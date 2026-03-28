package transport

import (
	"io"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
Emitter creates a side-channel for emitting the contents of a
read/write stream. It is used to perform the folding operation.

Reads use the pipe reader (pr) and optionally duplicate each chunk to dup
(e.g. a fold sink). dup must not be the same storage as pr — in
particular never TeeReader(rb, rb), which would re-feed the ring buffer
and never drain.

Writes go to the pipe writer (pw); the first frame is captured in wait
for folding.
*/
type Emitter struct {
	passthrough io.Writer
	tee         io.Reader
	wait        *primitive.Value
}

/*
NewEmitter wires the pipe ends: reads from pr, writes to pw, and tees
read data to dup (io.Discard if dup is nil).
*/
func NewEmitter(forward io.Reader, target io.Writer, dup io.Writer) *Emitter {
	return &Emitter{
		passthrough: target,
		tee:         io.TeeReader(forward, target),
	}
}

/*
Read reads from the emitter's tee reader.
*/
func (emitter *Emitter) Read(p []byte) (n int, err error) {
	if emitter.wait != nil {
		_, _ = io.Copy(emitter.wait, emitter.tee)
		return emitter.wait.Read(p)
	}

	return io.ReadFull(emitter.tee, p)
}

/*
Write writes to the emitter's passthrough writer.
*/
func (emitter *Emitter) Write(p []byte) (n int, err error) {
	if emitter.wait == nil {
		clone := make([]byte, len(p))
		copy(clone, p)

		emitter.wait = primitive.BytesToValue(clone)
	}

	return emitter.passthrough.Write(p)
}
