package compute

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/compute/kernel/metal"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
)

const (
	batchSize          = 10000
	defaultQueueSize   = 20000
	defaultBatchWindow = 500 * time.Microsecond
	frameWords         = 128
	frameBytes         = frameWords * 8
)

type bitwiseJob struct {
	a, b unsafe.Pointer
	done chan error
}

type batchBuffers struct {
	flatA []uint64
	flatB []uint64
}

var (
	defaultBackendOnce sync.Once
	defaultBackendMu   sync.Mutex
	defaultBackendInst *Backend
	defaultObserver    kernel.Observer = kernel.NoopObserver{}
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
	pool     *Pool
	nextHW   uint32 // round-robin substrate index
	observer kernel.Observer

	jobQueue    chan bitwiseJob
	batchSize   int
	batchWindow time.Duration
	bufferPool  sync.Pool
	closeOnce   sync.Once
}

// BackendOption configures the multi-substrate router.
type BackendOption func(*Backend)

/*
NewBackend initializes the unified Load Balancer by probing for
all available compute substrates and layering them by speed priority.
Accelerators are registered before the CPU fallback so the fast path is used
first when more than one substrate is available.
*/
func NewBackend(opts ...BackendOption) *Backend {
	backend := &Backend{
		hardware:    make([]kernel.Substrate, 0),
		observer:    kernel.NoopObserver{},
		batchSize:   batchSize,
		batchWindow: defaultBatchWindow,
	}

	for _, opt := range opts {
		opt(backend)
	}

	backend.observer = kernel.NormalizeObserver(backend.observer)

	if backend.ctx == nil {
		backend.ctx, backend.cancel = context.WithCancel(context.Background())
	}

	if backend.batchSize <= 0 {
		backend.batchSize = 1
	}
	if backend.batchWindow < 0 {
		backend.batchWindow = 0
	}
	if backend.jobQueue == nil {
		capHint := defaultQueueSize
		if backend.batchSize > capHint {
			capHint = backend.batchSize * 2
		}
		backend.jobQueue = make(chan bitwiseJob, capHint)
	}
	backend.bufferPool.New = func() any {
		return &batchBuffers{}
	}

	if backend.pool != nil {
		backend.pool.SetDropObserver(func(err error) {
			if err == nil {
				return
			}
			backend.observer.Error(
				"compute.pool.saturation",
				err,
				"dropped_total", backend.pool.DroppedErrors(),
				"saturation_total", backend.pool.SaturationEvents(),
			)
		})
	}

	for idx := 0; idx < cuda.Available(); idx++ {
		errnie.Info("compute.backend: CUDA substrate registered")
		backend.hardware = append(backend.hardware, cuda.NewBackend(
			idx,
			cuda.BackendWithObserver(backend.observer),
		))
	}

	for idx := 0; idx < metal.Available(); idx++ {
		errnie.Info("compute.backend: Metal substrate registered")
		backend.hardware = append(backend.hardware, metal.NewBackend(
			idx,
			metal.BackendWithObserver(backend.observer),
		))
	}

	errnie.Info("compute.backend: CPU substrate registered")
	backend.hardware = append(backend.hardware, cpu.NewBackend(
		cpu.BackendWithObserver(backend.observer),
	))

	if err := validate.Require(map[string]any{
		"ctx":      backend.ctx,
		"cancel":   backend.cancel,
		"hardware": backend.hardware,
		"jobQueue": backend.jobQueue,
	}); err != nil {
		errnie.Error(err)
		return nil
	}

	go backend.runBitwiseLoop()
	return backend
}

func defaultBackend() *Backend {
	defaultBackendOnce.Do(func() {
		pool, err := NewPool(
			PoolWithContext(context.Background()),
			PoolWithProcs(10),
		)
		if err != nil {
			panic(err)
		}

		defaultBackendMu.Lock()
		observer := defaultObserver
		defaultBackendMu.Unlock()

		defaultBackendInst = NewBackend(
			WithContext(context.Background()),
			WithPool(pool),
			WithKernelObserver(observer),
		)
	})

	defaultBackendMu.Lock()
	backend := defaultBackendInst
	defaultBackendMu.Unlock()
	return backend
}

func (backend *Backend) runBitwiseLoop() {
	for {
		select {
		case <-backend.ctx.Done():
			return
		case job, ok := <-backend.jobQueue:
			if !ok {
				return
			}
			batch := backend.gatherBatch(job)
			backend.executeBatch(batch)
		}
	}
}

func (backend *Backend) gatherBatch(first bitwiseJob) []bitwiseJob {
	batch := make([]bitwiseJob, 1, backend.batchSize)
	batch[0] = first

	if backend.batchSize <= 1 {
		return batch
	}

	if backend.batchWindow == 0 {
		for len(batch) < backend.batchSize {
			select {
			case job, ok := <-backend.jobQueue:
				if !ok {
					return batch
				}
				batch = append(batch, job)
			default:
				return batch
			}
		}
		return batch
	}

	timer := time.NewTimer(backend.batchWindow)
	defer timer.Stop()

	for len(batch) < backend.batchSize {
		select {
		case <-backend.ctx.Done():
			return batch
		case job, ok := <-backend.jobQueue:
			if !ok {
				return batch
			}
			batch = append(batch, job)
		case <-timer.C:
			return batch
		}
	}

	return batch
}

func (backend *Backend) executeBatch(batch []bitwiseJob) {
	if len(batch) == 0 {
		return
	}

	if len(backend.hardware) == 0 {
		err := NewBackendError(BackendErrorNoHardware, nil, "UniversalBitwise")
		for _, job := range batch {
			job.done <- err
		}
		return
	}

	hw := backend.nextSubstrate()
	if len(batch) == 1 {
		err := hw.UniversalBitwise(batch[0].a, batch[0].b, 1)
		batch[0].done <- err
		return
	}

	buf := backend.acquireBuffers(len(batch))
	defer backend.releaseBuffers(buf)

	for i, job := range batch {
		offset := i * frameWords
		copy(
			unsafe.Slice((*byte)(unsafe.Pointer(&buf.flatA[offset])), frameBytes),
			unsafe.Slice((*byte)(job.a), frameBytes),
		)
		copy(
			unsafe.Slice((*byte)(unsafe.Pointer(&buf.flatB[offset])), frameBytes),
			unsafe.Slice((*byte)(job.b), frameBytes),
		)
	}

	err := hw.UniversalBitwise(
		unsafe.Pointer(&buf.flatA[0]),
		unsafe.Pointer(&buf.flatB[0]),
		len(batch),
	)

	for i, job := range batch {
		offset := i * frameWords
		copy(
			unsafe.Slice((*byte)(job.a), frameBytes),
			unsafe.Slice((*byte)(unsafe.Pointer(&buf.flatA[offset])), frameBytes),
		)
		copy(
			unsafe.Slice((*byte)(job.b), frameBytes),
			unsafe.Slice((*byte)(unsafe.Pointer(&buf.flatB[offset])), frameBytes),
		)
		job.done <- err
	}
}

func (backend *Backend) nextSubstrate() kernel.Substrate {
	if len(backend.hardware) == 1 {
		return backend.hardware[0]
	}
	idx := atomic.AddUint32(&backend.nextHW, 1) - 1
	return backend.hardware[idx%uint32(len(backend.hardware))]
}

func (backend *Backend) acquireBuffers(batchLen int) *batchBuffers {
	buf := backend.bufferPool.Get().(*batchBuffers)
	need := batchLen * frameWords

	if cap(buf.flatA) < need {
		buf.flatA = make([]uint64, need)
	} else {
		buf.flatA = buf.flatA[:need]
		clear(buf.flatA)
	}

	if cap(buf.flatB) < need {
		buf.flatB = make([]uint64, need)
	} else {
		buf.flatB = buf.flatB[:need]
		clear(buf.flatB)
	}

	return buf
}

func (backend *Backend) releaseBuffers(buf *batchBuffers) {
	if buf == nil {
		return
	}
	backend.bufferPool.Put(buf)
}

func (backend *Backend) UniversalBitwise(a, b unsafe.Pointer) error {
	if backend == nil {
		return errors.New("compute.Backend.UniversalBitwise: nil backend")
	}
	if len(backend.hardware) == 0 {
		return NewBackendError(BackendErrorNoHardware, nil, "UniversalBitwise")
	}

	done := make(chan error, 1)
	job := bitwiseJob{a: a, b: b, done: done}

	select {
	case <-backend.ctx.Done():
		return backend.ctx.Err()
	case backend.jobQueue <- job:
		return <-done
	}
}

// UniversalBitwise is the package-level compatibility entrypoint used by the
// Value substrate. The first call lazily constructs the default backend.
func UniversalBitwise(a, b unsafe.Pointer) error {
	return defaultBackend().UniversalBitwise(a, b)
}

/*
WithContext sets the context for the backend.
*/
func WithContext(ctx context.Context) BackendOption {
	return func(backend *Backend) {
		if ctx == nil {
			ctx = context.Background()
		}
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

// WithBatchSize overrides the maximum number of frames folded together in one
// batched hardware dispatch.
func WithBatchSize(n int) BackendOption {
	return func(backend *Backend) {
		if n > 0 {
			backend.batchSize = n
		}
	}
}

// WithBatchWindow sets the maximum time the gather loop will wait for more
// work before dispatching the current batch.
func WithBatchWindow(window time.Duration) BackendOption {
	return func(backend *Backend) {
		if window >= 0 {
			backend.batchWindow = window
		}
	}
}

// SetKernelObserver updates the observer for the active default backend. When
// called before the default backend is initialized, the observer is retained
// and applied during lazy construction.
func SetKernelObserver(observer kernel.Observer) {
	normalized := kernel.NormalizeObserver(observer)

	defaultBackendMu.Lock()
	defaultObserver = normalized
	backend := defaultBackendInst
	defaultBackendMu.Unlock()

	if backend == nil {
		return
	}

	backend.observer = normalized
	for _, hw := range backend.hardware {
		if aware, ok := hw.(kernel.ObserverAware); ok {
			aware.SetObserver(normalized)
		}
	}
	if backend.pool != nil {
		backend.pool.SetDropObserver(func(err error) {
			if err == nil {
				return
			}
			backend.observer.Error(
				"compute.pool.saturation",
				err,
				"dropped_total", backend.pool.DroppedErrors(),
				"saturation_total", backend.pool.SaturationEvents(),
			)
		})
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

// Close stops the backend's batching worker. It is primarily intended for
// explicitly constructed backends used in tests.
func (backend *Backend) Close() {
	if backend == nil {
		return
	}
	backend.closeOnce.Do(func() {
		if backend.cancel != nil {
			backend.cancel()
		}
		if backend.jobQueue != nil {
			close(backend.jobQueue)
		}
	})
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
