package cuda

import "github.com/theapemachine/six/pkg/compute/kernel"

// NewCUDAKernelError is a convenience constructor for CUDA kernel errors.
func NewCUDAKernelError(typ kernel.KernelErrorType, err error, op string, batchSize uint32) *kernel.KernelError {
	return kernel.NewKernelError("cuda", typ, err, op, batchSize)
}
