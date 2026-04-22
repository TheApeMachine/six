package cpu

import "github.com/theapemachine/six/pkg/compute/kernel"

// NewCPUKernelError is a convenience constructor for CPU kernel errors.
func NewCPUKernelError(typ kernel.KernelErrorType, err error, op string) *kernel.KernelError {
	return kernel.NewKernelError("cpu", typ, err, op, 0)
}

