package compute

import (
	"context"
	"fmt"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/compute/kernel/metal"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/vm"
)

/*
Backend acts as an intelligent Multi-Substrate Load Balancer. It monitors
pressure across available local arithmetic hardware (GPU/CPU) and geometrically
overflows into the local Region (which acts as a local clustering space and
network mesh interface) if local capabilities are fully saturated.
*/
type Backend struct {
	ctx      context.Context
	cancel   context.CancelFunc
	hardware []kernel.Substrate
	pool     *vm.Pool
}

// BackendOption configures the multi-substrate router
type BackendOption func(*Backend)

/*
NewBackend initializes the unified Load Balancer by probing for
all available compute substrates and layering them by speed priority.
*/
func NewBackend(opts ...BackendOption) (*Backend, error) {
	backend := &Backend{
		hardware: make([]kernel.Substrate, 0),
	}

	for idx := range cuda.Available() {
		errnie.Info("compute.backend: CUDA substrate registered")
		backend.hardware = append(backend.hardware, cuda.NewBackend(idx))
	}

	for idx := range metal.Available() {
		errnie.Info("compute.backend: Metal substrate registered")
		backend.hardware = append(backend.hardware, metal.NewBackend(idx))
	}

	errnie.Info("compute.backend: CPU substrate registered")
	backend.hardware = append(backend.hardware, cpu.NewBackend())

	for _, opt := range opts {
		opt(backend)
	}

	if err := validate.Require(map[string]any{
		"ctx":      backend.ctx,
		"cancel":   backend.cancel,
		"hardware": backend.hardware,
	}); err != nil {
		return nil, errnie.Wrap(err)
	}

	return backend, nil
}

/*
UniversalBitwise implements the raw memory dispatcher if bypassing the stream.
*/
func (backend *Backend) UniversalBitwise(a, b, dst unsafe.Pointer, n uint32) error {
	if len(backend.hardware) == 0 {
		return errnie.Error(
			NewBackendError(
				nil,
				"compute.backend.UniversalBitwise",
				n,
			),
		)
	}

	return backend.hardware[0].UniversalBitwise(a, b, dst, n)
}

/*
WithContext sets the context for the backend.
*/
func WithContext(ctx context.Context) BackendOption {
	return func(backend *Backend) {
		backend.ctx, backend.cancel = context.WithCancel(ctx)
	}
}

/*
WithPool injects the worker pool for job scheduling.
*/
func WithPool(p *vm.Pool) BackendOption {
	return func(backend *Backend) {
		backend.pool = p
	}
}

/*
Schedule pushes abstract functional execution payloads onto the underlying worker pool.
*/
func (backend *Backend) Schedule(job func(ctx context.Context) error) {
	if backend.pool != nil {
		backend.pool.Schedule(job)
	} else {
		// Fallback for isolated tests to execute synchronously if no pool is injected
		_ = job(backend.ctx)
	}
}

type BackendErrorType string

const (
	BackendErrorNoHardware         BackendErrorType = "no hardware initialized"
	BackendErrorCompleteSaturation BackendErrorType = "complete saturation"
)

type BackendError struct {
	Err error
	Msg string
	Op  string
	N   uint32
}

func NewBackendError(err error, op string, n uint32) *BackendError {
	return &BackendError{
		Err: err,
		Msg: err.Error(),
		Op:  op,
		N:   n,
	}
}

func (e *BackendError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Msg, e.Err)
	}
	return e.Msg
}

func (e *BackendError) Unwrap() error {
	return e.Err
}
