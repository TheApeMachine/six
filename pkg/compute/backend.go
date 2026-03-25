package compute

import (
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/compute/kernel/metal"
	"github.com/theapemachine/six/pkg/errnie"
)

/*
Backend is a abstraction over the supported compute kernels,
and provides a unified interface for the computation pipeline.
It can be backed by CUDA, Metal, CPU, or distributed nodes.
*/
type Backend struct {
	kernel kernel.Substrate
}

/*
NewBackend creates a new backend with the best available kernel.
It is possible to override this when using the vm.Machine shell.
*/
func NewBackend() *Backend {
	return &Backend{
		kernel: func() kernel.Substrate {
			if cuda.Available() > 0 {
				errnie.Info("Using CUDA backend")
				return cuda.NewBackend()
			}

			if metal.Available() > 0 {
				errnie.Info("Using Metal backend")
				return metal.NewBackend()
			}

			errnie.Info("Using CPU backend")
			return cpu.NewBackend(cpu.BackendWithStreamPassthrough())
		}(),
	}
}

/*
Read implements the io.Reader interface, like all other components to
provide a seamless end-to-end streaming interface that ultimately
feeds back on itself to guarantee the "always-on" mechanism.
*/
func (backend *Backend) Read(p []byte) (n int, err error) {
	return backend.kernel.Read(p)
}

/*
Write implements the io.Writer interface, like all other components to
provide a seamless end-to-end streaming interface that ultimately
feeds back on itself to guarantee the "always-on" mechanism.
*/
func (backend *Backend) Write(p []byte) (n int, err error) {
	return backend.kernel.Write(p)
}

/*
Close implements the io.Closer interface, responsible for cleanly
tearing down the compute kernel.
*/
func (backend *Backend) Close() error {
	return backend.kernel.Close()
}

/*
UniversalBitwise is a fast-track for directly calling the kernel's
hardware-accelerated bitwise operations.
*/
func (backend *Backend) UniversalBitwise(a, b, dst unsafe.Pointer, n uint32) error {
	return backend.kernel.UniversalBitwise(a, b, dst, n)
}
