//go:build !darwin || !cgo

package metal

import (
	"io"
	"unsafe"
)

/*
Backend is the stub for non-darwin builds.
*/
type Backend struct {
	idx int
}

/*
NewBackend returns a stub Backend on non-darwin.
*/
func NewBackend(idx int) *Backend {
	return &Backend{
		idx: idx,
	}
}

/*
Available always returns zero on non-darwin.
*/
func Available() int {
	return 0
}

func (backend *Backend) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}

func (backend *Backend) Write(p []byte) (n int, err error) {
	return
}

func (backend *Backend) Close() error {
	return nil
}

func (backend *Backend) UniversalBitwise(a, b, dst unsafe.Pointer, n uint32) error {
	return NewMetalError(MetalErrorUnavailable, nil, "UniversalBitwise", n)
}

/*
MetalErrorType is a typed error for Metal backend failures.
*/
type MetalErrorType string

const (
	MetalErrorUnavailable    MetalErrorType = "metal backend unavailable"
	MetalErrorInitFailed     MetalErrorType = "metal backend init failed"
	MetalErrorDispatchFailed MetalErrorType = "metal backend dispatch failed"
)

type MetalError struct {
	Err error
	Msg string
	Op  string
	N   uint32
}

func NewMetalError(merr MetalErrorType, err error, op string, n uint32) *MetalError {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return &MetalError{
		Err: err,
		Msg: msg,
		Op:  op,
		N:   n,
	}
}

/*
Error implements the error interface for MetalErrorType.
*/
func (err *MetalError) Error() string {
	return err.Msg
}
