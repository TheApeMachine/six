//go:build !darwin || !cgo

package metal

import "unsafe"

/*
Backend is the stub for non-darwin builds.
*/
type Backend struct{}

/*
NewBackend returns a stub Backend on non-darwin.
*/
func NewBackend() *Backend {
	return &Backend{}
}

/*
Available always returns zero on non-darwin.
*/
func (backend *Backend) Available() (int, error) {
	return 0, MetalErrorUnavailable
}

func (backend *Backend) BitwiseOr(a, b, dst unsafe.Pointer, n uint32) error {
	return MetalErrorUnavailable
}

func (backend *Backend) BitwiseAnd(a, b, dst unsafe.Pointer, n uint32) error {
	return MetalErrorUnavailable
}

func (backend *Backend) BitwiseXor(a, b, dst unsafe.Pointer, n uint32) error {
	return MetalErrorUnavailable
}

func (backend *Backend) BitwiseAndNot(a, b, dst unsafe.Pointer, n uint32) error {
	return MetalErrorUnavailable
}

func (backend *Backend) BitwiseNand(a, b, dst unsafe.Pointer, n uint32) error {
	return MetalErrorUnavailable
}

func (backend *Backend) BitwiseNor(a, b, dst unsafe.Pointer, n uint32) error {
	return MetalErrorUnavailable
}

func (backend *Backend) BitwiseXnor(a, b, dst unsafe.Pointer, n uint32) error {
	return MetalErrorUnavailable
}

func (backend *Backend) BitwiseConverseNonimplication(a, b, dst unsafe.Pointer, n uint32) error {
	return MetalErrorUnavailable
}

func (backend *Backend) BitwiseNot(a, dst unsafe.Pointer, n uint32) error {
	return MetalErrorUnavailable
}

func (backend *Backend) MotorApply(a, b, dst unsafe.Pointer, n uint32) error {
	return MetalErrorUnavailable
}

func (backend *Backend) MotorInvert(a, b, dst unsafe.Pointer, n uint32) error {
	return MetalErrorUnavailable
}

func (backend *Backend) MotorCompose(a, b, dst unsafe.Pointer, n uint32) error {
	return MetalErrorUnavailable
}

func (backend *Backend) RollLeft(src, dst unsafe.Pointer, shift, n uint32) error {
	return MetalErrorUnavailable
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

/*
Error implements the error interface for MetalErrorType.
*/
func (err MetalErrorType) Error() string {
	return string(err)
}
