package metal

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
	msg := string(merr)
	if err != nil {
		msg += ": " + err.Error()
	}
	return &MetalError{
		Err: err,
		Msg: msg,
		Op:  op,
	}
}

// Error implements error for *MetalError.
func (err *MetalError) Error() string {
	if err == nil {
		return ""
	}
	return err.Msg
}
