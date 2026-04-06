package compute

import (
	"errors"
	"fmt"
)

// ComputeErrorType categorises compute-layer failures.
type ComputeErrorType string

const (
	// Backend-level error types (formerly BackendErrorType).
	ComputeErrNoHardware         ComputeErrorType = "no hardware initialized"
	ComputeErrCompleteSaturation ComputeErrorType = "complete saturation"
	ComputeErrNoComputeResource  ComputeErrorType = "no compute resource"
	ComputeErrNoValues           ComputeErrorType = "no values"
	ComputeErrPoolEnqueueFailed  ComputeErrorType = "pool enqueue failed"
	ComputeErrInlineJobFailed    ComputeErrorType = "inline job failed"
	ComputeErrSubstrateEjected   ComputeErrorType = "substrate ejected"

	// Pool-level error types (formerly PoolErrorType).
	ComputeErrPoolFail       ComputeErrorType = "pool failure"
	ComputeErrPoolInvalidJob ComputeErrorType = "invalid job"
)

// ComputeError is the unified error type for the compute package,
// replacing both BackendError and PoolError.
type ComputeError struct {
	Subsystem string           // "pool", "backend", etc.
	Op        string           // operation that failed
	Err       error            // underlying cause
	Msg       string           // human-readable summary
	Type      ComputeErrorType // failure category
}

// NewComputeError builds a ComputeError.
func NewComputeError(subsystem string, typ ComputeErrorType, err error, op string) *ComputeError {
	msg := string(typ)
	if err != nil {
		msg += ": " + err.Error()
	}
	return &ComputeError{
		Subsystem: subsystem,
		Op:        op,
		Err:       err,
		Msg:       msg,
		Type:      typ,
	}
}

// AsComputeType reports whether err wraps a *ComputeError whose Type matches.
func AsComputeType(err error, t ComputeErrorType) bool {
	var ce *ComputeError
	return errors.As(err, &ce) && ce.Type == t
}

func (e *ComputeError) Error() string {
	if e == nil {
		return ""
	}
	if e.Op != "" {
		return fmt.Sprintf("[%s] %s (%s)", e.Subsystem, e.Msg, e.Op)
	}
	return fmt.Sprintf("[%s] %s", e.Subsystem, e.Msg)
}

func (e *ComputeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
