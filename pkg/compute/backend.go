package compute

import (
	"context"
	"errors"
	"fmt"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/compute/kernel/metal"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
)

var substrate *Backend
var ctx context.Context
var cancel context.CancelFunc

func init() {
	var err error

	ctx, cancel = context.WithCancel(context.Background())

	pool, err := NewPool(
		PoolWithContext(ctx),
		PoolWithProcs(10),
	)

	if err != nil {
		panic(err)
	}

	substrate = NewBackend(
		WithContext(context.Background()),
		WithPool(pool),
	)
}

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
	pool     *Pool
	nextHW   uint32 // round-robin substrate index
	observer kernel.Observer
}

// BackendOption configures the multi-substrate router
type BackendOption func(*Backend)

/*
NewBackend initializes the unified Load Balancer by probing for
all available compute substrates and layering them by speed priority.
*/
func NewBackend(opts ...BackendOption) *Backend {
	substrate = &Backend{
		hardware: make([]kernel.Substrate, 0),
		observer: kernel.NoopObserver{},
	}

	for _, opt := range opts {
		opt(substrate)
	}

	substrate.observer = kernel.NormalizeObserver(substrate.observer)

	errnie.Info("compute.backend: CPU substrate registered")
	substrate.hardware = append(substrate.hardware, cpu.NewBackend(
		cpu.BackendWithObserver(substrate.observer),
	))

	// Available() returns a device count; iterate 0..n-1 explicitly.
	for idx := 0; idx < cuda.Available(); idx++ {
		errnie.Info("compute.backend: CUDA substrate registered")
		substrate.hardware = append(substrate.hardware, cuda.NewBackend(
			idx,
			cuda.BackendWithObserver(substrate.observer),
		))
	}

	for idx := 0; idx < metal.Available(); idx++ {
		errnie.Info("compute.backend: Metal substrate registered")
		substrate.hardware = append(substrate.hardware, metal.NewBackend(
			idx,
			metal.BackendWithObserver(substrate.observer),
		))
	}

	if substrate.ctx == nil {
		substrate.ctx, substrate.cancel = context.WithCancel(context.Background())
	}

	if err := validate.Require(map[string]any{
		"ctx":      substrate.ctx,
		"cancel":   substrate.cancel,
		"hardware": substrate.hardware,
	}); err != nil {
		errnie.Error(err)
		return nil
	}

	return substrate
}

func UniversalBitwise(a, b unsafe.Pointer) error {
	return substrate.hardware[0].UniversalBitwise(a, b)
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
func WithPool(p *Pool) BackendOption {
	return func(backend *Backend) {
		backend.pool = p
	}
}

// WithKernelObserver injects a kernel observer for all discovered backends.
func WithKernelObserver(observer kernel.Observer) BackendOption {
	return func(backend *Backend) {
		backend.observer = kernel.NormalizeObserver(observer)
	}
}

// SetKernelObserver updates the observer for the active global substrate.
func SetKernelObserver(observer kernel.Observer) {
	if substrate == nil {
		return
	}
	normalized := kernel.NormalizeObserver(observer)
	substrate.observer = normalized

	for _, hw := range substrate.hardware {
		if aware, ok := hw.(kernel.ObserverAware); ok {
			aware.SetObserver(normalized)
		}
	}
}

type errnieKernelObserver struct{}

func (errnieKernelObserver) Trace(event string, keyvals ...any) {
	errnie.Trace(event, keyvals...)
}

func (errnieKernelObserver) Error(event string, err error, keyvals ...any) {
	if err == nil {
		return
	}
	kv := make([]any, 0, len(keyvals)+2)
	kv = append(kv, "event", event)
	kv = append(kv, keyvals...)
	_ = errnie.Error(err, kv...)
}

// NewErrnieKernelObserver returns an async observer that forwards to errnie.
func NewErrnieKernelObserver(queueSize int) kernel.Observer {
	return kernel.NewAsyncObserver(errnieKernelObserver{}, queueSize)
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
