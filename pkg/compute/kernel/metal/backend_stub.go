//go:build !darwin || !cgo

package metal

import (
	"context"
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

func (backend *Backend) UniversalBitwise(a, b unsafe.Pointer) error {
	return NewMetalError(MetalErrorUnavailable, nil, "UniversalBitwise")
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
}

func NewMetalError(merr MetalErrorType, err error, op string) *MetalError {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return &MetalError{
		Err: err,
		Msg: msg,
		Op:  op,
	}
}

/*
Error implements the error interface for MetalErrorType.
*/
func (err *MetalError) Error() string {
	return err.Msg
}

func (backend *Backend) Schedule(job func(ctx context.Context) error) {
	_ = job(context.Background())
}
