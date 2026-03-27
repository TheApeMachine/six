package cuda

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
Error implements the error interface. Msg already includes Err via NewCUDAError.
*/
func (e *CUDAError) Error() string {
	return e.Msg
}

/*
Unwrap returns the wrapped error so that errors.Is / errors.As work across the
error chain.
*/
func (e *CUDAError) Unwrap() error {
	return e.Err
}
