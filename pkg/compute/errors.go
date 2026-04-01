package compute

import (
	"errors"
	"fmt"
)

type BackendErrorType string

const (
	BackendErrorNoHardware         BackendErrorType = "no hardware initialized"
	BackendErrorCompleteSaturation BackendErrorType = "complete saturation"
	BackendErrorNoComputeResource  BackendErrorType = "no compute resource"
	BackendErrorNoValues           BackendErrorType = "no values"
	BackendErrorPoolEnqueueFailed  BackendErrorType = "pool enqueue failed"
	BackendErrorInlineJobFailed    BackendErrorType = "inline job failed"
)

type BackendError struct {
	Type BackendErrorType
	Err  error
	Msg  string
	Op   string
}

func NewBackendError(typ BackendErrorType, err error, op string) *BackendError {
	msg := string(typ)
	if msg == "" && err != nil {
		msg = err.Error()
	}
	return &BackendError{
		Type: typ,
		Err:  err,
		Msg:  msg,
		Op:   op,
	}
}

// AsType reports whether err wraps a *BackendError whose Type matches.
func AsType(err error, t BackendErrorType) bool {
	var be *BackendError
	return errors.As(err, &be) && be.Type == t
}

func (e *BackendError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	if e.Type != "" {
		if e.Op != "" {
			return fmt.Sprintf("%s (%s)", e.Type, e.Op)
		}
		return string(e.Type)
	}
	if e.Msg != "" {
		return e.Msg
	}
	if e.Op != "" {
		return fmt.Sprintf("backend error (%s)", e.Op)
	}
	return "backend error"
}

func (e *BackendError) Unwrap() error {
	return e.Err
}
