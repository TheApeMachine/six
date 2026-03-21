//go:build !darwin || !cgo

package metal

import "bytes"

/*
MetalBackend is the stub for non-darwin builds. Satisfies the
kernel.Backend interface so the package compiles without Metal.
*/
type MetalBackend struct {
	buf bytes.Buffer
}

/*
Available always returns zero on non-darwin.
*/
func (backend *MetalBackend) Available() (int, error) {
	return 0, MetalErrorUnavailable
}

/*
Read drains the result buffer.
*/
func (backend *MetalBackend) Read(p []byte) (n int, err error) {
	return backend.buf.Read(p)
}

/*
Write accepts incoming data.
*/
func (backend *MetalBackend) Write(p []byte) (n int, err error) {
	return backend.buf.Write(p)
}

/*
Close resets the buffer.
*/
func (backend *MetalBackend) Close() error {
	backend.buf.Reset()
	return nil
}

/*
MetalError enumerates Metal failure kinds.
*/
type MetalError string

const (
	MetalErrorUnavailable   MetalError = "metal backend unavailable"
	MetalErrorInitFailed    MetalError = "metal backend init failed"
	MetalErrorResolveFailed MetalError = "metal backend resolve failed"
)

/*
Error implements error.
*/
func (err MetalError) Error() string {
	return string(err)
}
