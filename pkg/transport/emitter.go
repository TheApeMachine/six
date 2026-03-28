package transport

import (
	"io"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
Emitter creates a side-channel for emitting the contents of a
read/write stream. It is used to perform the folding operation.

Reads use the pipe reader (pr). When dup is non-nil, data read from pr is
teed to dup. When dup is nil, reads use TeeReader(forward, target) so the
pipe writer still receives each read chunk; stream consumers then fold via
io.Copy into wait (see Read).

Writes go to the pipe writer (pw); frames are captured in wait for folding
(first full frame from Write; later writes are merged via Value.Write).

When dup is non-nil and wait is set, Read uses tee.Read and mirrors full
ByteSize chunks into wait instead of draining the tee in one Copy.
*/
type Emitter struct {
	passthrough io.Writer
	tee         io.Reader
	wait        *primitive.Value
	mirrorBuf   []byte
	mirrorReads bool // true when dup != nil: mirror tee reads into wait in ByteSize chunks
}

/*
NewEmitter wires the pipe ends: reads from pr, writes to pw. When dup is
non-nil, read data is teed to dup; when dup is nil, read data is teed to
target (same as passthrough), matching the stream folding layout.
*/
func NewEmitter(forward io.Reader, target io.Writer, dup io.Writer) *Emitter {
	teeTo := dup
	if teeTo == nil {
		teeTo = target
	}
	return &Emitter{
		passthrough: target,
		tee:         io.TeeReader(forward, teeTo),
		mirrorReads: dup != nil,
	}
}

/*
Read reads from the emitter's tee reader. For the stream case (dup nil),
once wait is set, the prior behavior is preserved: drain the tee into wait
then Read one frame from wait. When dup is set, Read uses the tee directly
and mirrors full ByteSize chunks into wait.
*/
func (emitter *Emitter) Read(p []byte) (n int, err error) {
	if emitter.wait != nil && !emitter.mirrorReads {
		if _, err = io.Copy(emitter.wait, emitter.tee); err != nil {
			return
		}

		return emitter.wait.Read(p)
	}

	if emitter.wait == nil {
		return io.ReadFull(emitter.tee, p)
	}

	n, err = emitter.tee.Read(p)

	if n > 0 {
		emitter.mirrorBuf = append(emitter.mirrorBuf, p[:n]...)

		for len(emitter.mirrorBuf) >= primitive.ByteSize {
			chunk := emitter.mirrorBuf[:primitive.ByteSize]
			emitter.mirrorBuf = emitter.mirrorBuf[primitive.ByteSize:]

			if _, err = emitter.wait.Write(chunk); err != nil {
				return
			}
		}
	}

	return n, err
}

/*
Write writes to the emitter's passthrough writer. The first frame seeds
wait; subsequent frames are merged into wait with Value.Write.
*/
func (emitter *Emitter) Write(p []byte) (n int, err error) {
	if emitter.wait == nil {
		clone := make([]byte, len(p))
		copy(clone, p)
		emitter.wait = primitive.BytesToValue(clone)
	} else {
		if _, err = emitter.wait.Write(p); err != nil {
			return
		}
	}

	return emitter.passthrough.Write(p)
}
