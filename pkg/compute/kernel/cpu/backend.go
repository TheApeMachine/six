package cpu

import (
	"bytes"
	"runtime"
)

/*
Backend is the CPU kernel backend. It is an io.ReadWriteCloser backed
by a bytes.Buffer. Data flows in via Write, out via Read. The actual
compute transformations happen at the Stream operation level — this
type just provides the buffered io surface and reports CPU availability.
*/
type Backend struct {
	buf bytes.Buffer
}

/*
Available returns the number of logical CPU cores.
*/
func (backend *Backend) Available() (int, error) {
	return runtime.NumCPU(), nil
}

/*
Read drains the result buffer.
*/
func (backend *Backend) Read(p []byte) (n int, err error) {
	return backend.buf.Read(p)
}

/*
Write accepts incoming data.
*/
func (backend *Backend) Write(p []byte) (n int, err error) {
	return backend.buf.Write(p)
}

/*
Close resets the buffer for reuse.
*/
func (backend *Backend) Close() error {
	backend.buf.Reset()
	return nil
}

/*
Reset clears state for another round-trip on the same Backend.
*/
func (backend *Backend) Reset() {
	backend.buf.Reset()
}
