package transport

import (
	"io"
)

/*
Observer creates a side-channel for observing the contents of a
read/write stream. It is used to pull out query results from the wire.

Reads come from pr with an optional duplicate to observe (nil → io.Discard).
Writes go to pw. observe must not be the same underlying buffer as pr.
*/
type Observer struct {
	out io.Writer
	tee io.Reader
}

/*
NewObserver wires pipe ends like Emitter: read from pr, write to pw,
tee read bytes to observe.
*/
func NewObserver(pr io.Reader, pw io.Writer, observe io.Writer) *Observer {
	if observe == nil {
		observe = io.Discard
	}
	return &Observer{
		out: pw,
		tee: io.TeeReader(pr, observe),
	}
}

/*
Read reads from the observer's tee reader.
*/
func (observer *Observer) Read(p []byte) (n int, err error) {
	return observer.tee.Read(p)
}

/*
Write writes to the observer's passthrough writer.
*/
func (observer *Observer) Write(p []byte) (n int, err error) {
	return observer.out.Write(p)
}
