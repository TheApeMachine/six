package kernel

import "fmt"

// KernelErrorType categorises kernel failures.
type KernelErrorType string

const (
	KernelErrUnavailable    KernelErrorType = "backend unavailable"
	KernelErrInitFailed     KernelErrorType = "backend init failed"
	KernelErrDispatchFailed KernelErrorType = "backend dispatch failed"
	KernelErrNilPointer     KernelErrorType = "nil value pointer"
)

// KernelError wraps a compute-kernel failure with the subsystem name, operation,
// batch size, and underlying cause. It satisfies errors.Is / errors.As via Unwrap.
type KernelError struct {
	Subsystem string          // "cuda", "metal", "cpu"
	Op        string          // "BitwiseOr", "MotorCompose", etc.
	Err       error           // underlying cause (sentinel or OS-level)
	Msg       string          // human-readable summary
	N         uint32          // batch size at time of failure (0 when N/A)
	Type      KernelErrorType // failure category
}

// NewKernelError builds a KernelError with a formatted message.
func NewKernelError(subsystem string, typ KernelErrorType, err error, op string, n uint32) *KernelError {
	msg := fmt.Sprintf("%s: %s", subsystem, typ)
	if err != nil {
		msg += ": " + err.Error()
	}
	return &KernelError{
		Subsystem: subsystem,
		Op:        op,
		Err:       err,
		Msg:       msg,
		N:         n,
		Type:      typ,
	}
}

func (e *KernelError) Error() string {
	if e == nil {
		return ""
	}
	if e.N > 0 {
		return fmt.Sprintf("%s: %s (n=%d): %s", e.Subsystem, e.Op, e.N, e.msgOrCause())
	}
	return fmt.Sprintf("%s: %s: %s", e.Subsystem, e.Op, e.msgOrCause())
}

func (e *KernelError) msgOrCause() string {
	if e.Msg != "" {
		return e.Msg
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "(no error)"
}

func (e *KernelError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

