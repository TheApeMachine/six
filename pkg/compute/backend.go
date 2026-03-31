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
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
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
		batchSize:   core.Cfg.System.BatchSize,
		batchWindow: core.Cfg.System.BatchWindow,
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
		capHint := core.Cfg.System.QueueSize
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
		backend.pool.StartWorkers()
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
		backend.ctx,
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
		pool.StartWorkers()

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

	hw := backend.selectSubstrate(batch)
	if len(batch) == 1 {
		err := hw.UniversalBitwise(batch[0].a, batch[0].b, 1)
		batch[0].done <- err
		return
	}

	buf := backend.acquireBuffers(len(batch))
	defer backend.releaseBuffers(buf)

	for i, job := range batch {
		offset := i * core.Cfg.Value.Words
		copy(
			unsafe.Slice((*byte)(unsafe.Pointer(&buf.flatA[offset])), core.Cfg.Value.Bytes),
			unsafe.Slice((*byte)(job.a), core.Cfg.Value.Bytes),
		)
		copy(
			unsafe.Slice((*byte)(unsafe.Pointer(&buf.flatB[offset])), core.Cfg.Value.Bytes),
			unsafe.Slice((*byte)(job.b), core.Cfg.Value.Bytes),
		)
	}

	err := hw.UniversalBitwise(
		unsafe.Pointer(&buf.flatA[0]),
		unsafe.Pointer(&buf.flatB[0]),
		len(batch),
	)

	for i, job := range batch {
		offset := i * core.Cfg.Value.Words
		copy(
			unsafe.Slice((*byte)(job.a), core.Cfg.Value.Bytes),
			unsafe.Slice((*byte)(unsafe.Pointer(&buf.flatA[offset])), core.Cfg.Value.Bytes),
		)
		copy(
			unsafe.Slice((*byte)(job.b), core.Cfg.Value.Bytes),
			unsafe.Slice((*byte)(unsafe.Pointer(&buf.flatB[offset])), core.Cfg.Value.Bytes),
		)
		job.done <- err
	}
}

func (backend *Backend) selectSubstrate(batch []bitwiseJob) kernel.Substrate {
	if cpuHW := backend.cpuSubstrate(); cpuHW != nil {
		for _, job := range batch {
			if frameRequiresProgramExecution(job.a) {
				return cpuHW
			}
		}
	}
	return backend.nextSubstrate()
}

func (backend *Backend) cpuSubstrate() kernel.Substrate {
	for _, hw := range backend.hardware {
		if _, ok := hw.(*cpu.Backend); ok {
			return hw
		}
	}
	return nil
}

func frameRequiresProgramExecution(ptr unsafe.Pointer) bool {
	if ptr == nil {
		return false
	}
	frame := unsafe.Slice((*uint64)(ptr), core.Cfg.Value.Words)
	if core.Cfg == nil {
		return false
	}
	if frame[core.Cfg.Value.Region.Registers.FW] > 0 {
		return true
	}
	startWord := core.Cfg.Value.Region.Program.Start
	if startWord < 0 || startWord >= len(frame) {
		return false
	}
	progWords := int((core.Cfg.Value.Region.Program.Bits + 63) / 64)
	endWord := startWord + progWords
	if endWord > len(frame) {
		endWord = len(frame)
	}
	for i := startWord; i < endWord; i++ {
		if frame[i] != 0 {
			return true
		}
	}
	return false
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
	need := batchLen * core.Cfg.Value.Words

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

/*
UniversalBitwise runs pairwise firmware on the frames at a and b. For immutable
canonical Values, pass pointers to disposable full-frame copies; results are
written into those buffers (and for batched jobs, copied back to the same pointers).
*/
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

/*
UniversalBitwise is the package-level compatibility entrypoint used by the
Value substrate. The first call lazily constructs the default backend.
*/
func UniversalBitwise(a, b unsafe.Pointer) error {
	return defaultBackend().UniversalBitwise(a, b)
}

/*
SetKernelObserver updates the observer for the active default backend. When
called before the default backend is initialized, the observer is retained
and applied during lazy construction.
*/
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
