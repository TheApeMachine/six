package compute

import (
	"io"

	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/compute/kernel/metal"
	"github.com/theapemachine/six/pkg/errnie"
)

type Backend struct {
	kernel io.ReadWriteCloser
}

func NewBackend() *Backend {
	return &Backend{
		kernel: func() io.ReadWriteCloser {
			if cuda.Available() > 0 {
				errnie.Info("Using CUDA backend")
				return cuda.NewBackend()
			}

			if metal.Available() > 0 {
				errnie.Info("Using Metal backend")
				return metal.NewBackend()
			}

			errnie.Info("Using CPU backend")
			return cpu.NewBackend()
		}(),
	}
}

func (backend *Backend) Read(p []byte) (n int, err error) {
	return backend.kernel.Read(p)
}

func (backend *Backend) Write(p []byte) (n int, err error) {
	return backend.kernel.Write(p)
}

func (backend *Backend) Close() error {
	return backend.kernel.Close()
}
