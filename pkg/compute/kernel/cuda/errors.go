package cuda

import "fmt"

/*
CUDAErrorType is a typed string for CUDA backend failure categories.
*/
type CUDAErrorType string

const (
	CUDAErrorUnavailable    CUDAErrorType = "cuda backend unavailable"
	CUDAErrorDispatchFailed CUDAErrorType = "cuda backend dispatch failed"
)

/*
CUDAError carries a wrapped error, a human-readable message, the operation
name, and the batch size at the time of failure.
*/
type CUDAError struct {
	Err error
	Msg string
	Op  string
}

/*
NewCUDAError returns a new CUDAError.
*/
func NewCUDAError(cerr CUDAErrorType, err error, op string) *CUDAError {
	msg := string(cerr)
	if err != nil {
		msg += ": " + err.Error()
	}
	return &CUDAError{
		Err: err,
		Msg: msg,
		Op:  op,
	}
}

/*
Error implements the error interface. It combines Msg and Err so that callers
see the full context rather than only the typed category string.
*/
func (e *CUDAError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Msg, e.Err)
	}
	return e.Msg
}

/*
Unwrap returns the wrapped error so that errors.Is / errors.As work across the
error chain.
*/
func (e *CUDAError) Unwrap() error {
	return e.Err
}
