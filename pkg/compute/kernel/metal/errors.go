package metal

import "github.com/theapemachine/six/pkg/compute/kernel"

// NewMetalKernelError is a convenience constructor for Metal kernel errors.
func NewMetalKernelError(typ kernel.KernelErrorType, err error, op string) *kernel.KernelError {
	return kernel.NewKernelError("metal", typ, err, op, 0)
}

