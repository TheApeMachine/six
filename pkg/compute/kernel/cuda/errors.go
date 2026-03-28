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
	Err       error
	Msg       string
	Op        string
	BatchSize int
}

/*
NewCUDAError returns a new CUDAError. batchSize is the logical batch width
at dispatch (e.g. number of Value pairs); use 0 when not applicable.
*/
func NewCUDAError(cerr CUDAErrorType, err error, op string, batchSize int) *CUDAError {
	msg := string(cerr)
	if err != nil {
		msg += ": " + err.Error()
	}
	return &CUDAError{
		Err:       err,
		Msg:       msg,
		Op:        op,
		BatchSize: batchSize,
	}
}

/*
Error implements the error interface. Msg already includes Err via NewCUDAError.
*/
func (e *CUDAError) Error() string {
	if e == nil {
		return ""
	}
	if e.BatchSize > 0 {
		return fmt.Sprintf("%s (batchSize=%d)", e.Msg, e.BatchSize)
	}
	return e.Msg
}

/*
Unwrap returns the wrapped error so that errors.Is / errors.As work across the
error chain.
*/
func (e *CUDAError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
