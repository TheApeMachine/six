package compute

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/compute/kernel/metal"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
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
	nextHW   uint32 // round-robin substrate index
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

	// Available() returns a device count; iterate 0..n-1 explicitly.
	for idx := 0; idx < cuda.Available(); idx++ {
		errnie.Info("compute.backend: CUDA substrate registered")
		backend.hardware = append(backend.hardware, cuda.NewBackend(idx))
	}

	for idx := 0; idx < metal.Available(); idx++ {
		errnie.Info("compute.backend: Metal substrate registered")
		backend.hardware = append(backend.hardware, metal.NewBackend(idx))
	}

	errnie.Info("compute.backend: CPU substrate registered")
	backend.hardware = append(backend.hardware, cpu.NewBackend())

	for _, opt := range opts {
		opt(backend)
	}

	if backend.ctx == nil {
		backend.ctx, backend.cancel = context.WithCancel(context.Background())
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
func (backend *Backend) UniversalBitwise(a, b unsafe.Pointer) error {
	if len(backend.hardware) == 0 {
		err := NewBackendError(BackendErrorNoHardware, nil, "compute.backend.UniversalBitwise")
		_ = errnie.Error(err)
		return fmt.Errorf("compute.backend.UniversalBitwise: %w", err)
	}

	sub := backend.pickSubstrate(a)
	if sub == nil {
		err := NewBackendError(BackendErrorCompleteSaturation, nil, "compute.backend.UniversalBitwise")
		_ = errnie.Error(err)
		return fmt.Errorf("compute.backend.UniversalBitwise: %w", err)
	}
	return sub.UniversalBitwise(a, b)
}

// pickSubstrate rotates across local substrates. Values tagged by QUIC ingress
// (WANIngressScarred) stick to the first registered device to avoid ping-pong
// routing costs across heterogeneous transports elsewhere in the mesh.
func (backend *Backend) pickSubstrate(hostFrame unsafe.Pointer) kernel.Substrate {
	n := len(backend.hardware)
	if n == 0 {
		return nil
	}
	if hostFrame != nil {
		v := (*primitive.Value)(hostFrame)
		if v.WANIngressScarred() {
			return backend.hardware[0]
		}
	}
	i := atomic.AddUint32(&backend.nextHW, 1) - 1
	return backend.hardware[int(i)%n]
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
Schedule pushes work onto the pool when configured; otherwise runs the job
inline with backend.ctx. Returns nil on success, or a wrapped error on pool
enqueue failure / context cancellation / inline job failure.
*/
func (backend *Backend) Schedule(job func(ctx context.Context) error) error {
	if backend.pool != nil {
		if err := backend.pool.Schedule(backend.ctx, job); err != nil {
			_ = errnie.Error(err)
			return fmt.Errorf("compute.Backend.Schedule: %w", err)
		}
		return nil
	}
	if err := job(backend.ctx); err != nil {
		_ = errnie.Error(err)
		return fmt.Errorf("compute.Backend.Schedule: %w", err)
	}
	return nil
}

type BackendErrorType string

const (
	BackendErrorNoHardware         BackendErrorType = "no hardware initialized"
	BackendErrorCompleteSaturation BackendErrorType = "complete saturation"
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
