package kernel

import "fmt"

// KernelError wraps a compute-kernel failure with the backend name, operation,
// batch size, and underlying cause. It satisfies errors.Is / errors.As via Unwrap.
type KernelError struct {
	Backend string // "cuda", "metal", "cpu"
	Op      string // "BitwiseOr", "MotorCompose", etc.
	N       uint32 // batch size at time of failure
	Err     error  // underlying cause (sentinel or OS-level)
}

func (e *KernelError) Error() string {
	if e.N > 0 {
		return fmt.Sprintf("%s: %s (n=%d): %v", e.Backend, e.Op, e.N, e.Err)
	}
	return fmt.Sprintf("%s: %s: %v", e.Backend, e.Op, e.Err)
}

func (e *KernelError) Unwrap() error { return e.Err }
