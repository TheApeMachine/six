package compute

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/compute/kernel/metal"
	"github.com/theapemachine/six/pkg/compute/stepwise"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

type bitwiseJob struct {
	a, b unsafe.Pointer
	done chan error
}

type batchBuffers struct {
	flatA []uint64
	flatB []uint64
}

/*
accelSlot tracks per-device pressure for heterogeneous dispatch. inflight counts
batches not yet finished on that accelerator; emaPerFrameNs is an exponential moving
average of observed nanoseconds per frame from the last batches (cold start uses 0).
*/
type accelSlot struct {
	sub           kernel.Substrate
	inflight      int32
	emaPerFrameNs uint64
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

Ingress splits into two FIFOs so accelerator batching never interleaves frames
that must execute the in-band CPU interpreter with SIMD‑only GPU kernels — a
single program-bearing frame previously forced the entire batch onto the CPU
after packing on the GPU path.

Accelerators are chosen by least in-flight depth, then lowest EMA service time,
rather than round‑robin.
*/
type Backend struct {
	ctx      context.Context
	cancel   context.CancelFunc
	hardware []kernel.Substrate
	cpuIdx   int
	accel    []accelSlot
	pool     *Pool
	observer kernel.Observer

	jobQueueAccel chan bitwiseJob
	jobQueueCPU   chan bitwiseJob
	batchSize     int
	batchWindow   time.Duration
	bufferPool    sync.Pool
	closeOnce     sync.Once
}

// BackendOption configures the multi-substrate router.
type BackendOption func(*Backend)

/*
NewBackend initializes the unified Load Balancer by probing for
all available compute substrates and layering them by speed priority.
Accelerators are registered before the CPU fallback so the fast path is used
first when more than one substrate is available.
*/
func NewBackend(ctx context.Context, opts ...BackendOption) *Backend {
	ctx, cancel := context.WithCancel(ctx)

	backend := &Backend{
		ctx:         ctx,
		cancel:      cancel,
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
	if backend.jobQueueAccel == nil {
		capHint := core.Cfg.System.QueueSize
		if backend.batchSize > capHint {
			capHint = backend.batchSize * 2
		}
		backend.jobQueueAccel = make(chan bitwiseJob, capHint)
		backend.jobQueueCPU = make(chan bitwiseJob, capHint)
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

	backend.cpuIdx = -1

	for i, hw := range backend.hardware {
		if _, ok := hw.(*cpu.Backend); ok {
			backend.cpuIdx = i
			continue
		}
		backend.accel = append(backend.accel, accelSlot{sub: hw})
	}

	if err := validate.Require(map[string]any{
		"ctx":           backend.ctx,
		"cancel":        backend.cancel,
		"hardware":      backend.hardware,
		"jobQueueAccel": backend.jobQueueAccel,
		"jobQueueCPU":   backend.jobQueueCPU,
	}); err != nil {
		errnie.Error(err)
		return nil
	}

	go backend.runAccelLoop()
	go backend.runCPULoop()
	return backend
}

func (backend *Backend) runAccelLoop() {

	for {
		select {
		case <-backend.ctx.Done():
			return
		case job, ok := <-backend.jobQueueAccel:
			if !ok {
				return
			}
			batch := backend.gatherBatch(backend.jobQueueAccel, job)
			backend.executeAccelBatch(batch)
		}
	}
}

func (backend *Backend) runCPULoop() {

	for {
		select {
		case <-backend.ctx.Done():
			return
		case job, ok := <-backend.jobQueueCPU:
			if !ok {
				return
			}
			batch := backend.gatherBatch(backend.jobQueueCPU, job)
			backend.executeCPUBatch(batch)
		}
	}
}

func (backend *Backend) gatherBatch(q chan bitwiseJob, first bitwiseJob) []bitwiseJob {
	batch := make([]bitwiseJob, 1, backend.batchSize)
	batch[0] = first

	if backend.batchSize <= 1 {
		return batch
	}

	if backend.batchWindow == 0 {
		for len(batch) < backend.batchSize {
			select {
			case job, ok := <-q:
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
		case job, ok := <-q:
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

func (backend *Backend) pickAccelerator() int {

	if len(backend.accel) == 1 {
		return 0
	}

	best := 0
	bestInfl := int32(1<<30 - 1)
	bestEma := uint64(^uint64(0))

	for i := range backend.accel {
		infl := backend.accel[i].inflight
		ema := backend.accel[i].emaPerFrameNs
		if ema == 0 {
			ema = 1
		}
		if infl < bestInfl || (infl == bestInfl && ema < bestEma) {
			best = i
			bestInfl = infl
			bestEma = ema
		}
	}

	return best
}

func (backend *Backend) recordAccelSample(idx int, elapsed time.Duration, frames int) {

	if frames <= 0 || idx < 0 || idx >= len(backend.accel) {
		return
	}

	sample := uint64(elapsed.Nanoseconds()) / uint64(frames)
	if sample == 0 {
		sample = 1
	}

	slot := &backend.accel[idx]
	old := slot.emaPerFrameNs

	if old == 0 {
		slot.emaPerFrameNs = sample
		return
	}

	slot.emaPerFrameNs = (old*7 + sample) / 8
}

func (backend *Backend) executeAccelBatch(batch []bitwiseJob) {

	if len(batch) == 0 {
		return
	}

	if len(backend.accel) == 0 {
		err := NewBackendError(BackendErrorNoHardware, nil, "UniversalBitwise")
		for _, job := range batch {
			job.done <- err
		}
		return
	}

	idx := backend.pickAccelerator()
	slot := &backend.accel[idx]
	slot.inflight++
	start := time.Now()

	defer func() {
		slot.inflight--
		backend.recordAccelSample(idx, time.Since(start), len(batch))
	}()

	hw := slot.sub

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

func (backend *Backend) executeCPUBatch(batch []bitwiseJob) {

	if len(batch) == 0 {
		return
	}

	if backend.cpuIdx < 0 || backend.cpuIdx >= len(backend.hardware) {
		err := NewBackendError(BackendErrorNoHardware, nil, "UniversalBitwise")
		for _, job := range batch {
			job.done <- err
		}
		return
	}

	cpuKernel, ok := backend.hardware[backend.cpuIdx].(*cpu.Backend)
	if !ok || cpuKernel == nil {
		err := NewBackendError(BackendErrorNoHardware, nil, "UniversalBitwise")
		for _, job := range batch {
			job.done <- err
		}
		return
	}

	stepwiseIdx := make([]int, 0, len(batch))
	legacyIdx := make([]int, 0, len(batch))

	for i := range batch {
		if stepwise.DetectEmbeddedStepwise((*[stepwise.FrameWords]uint64)(batch[i].a)) {
			stepwiseIdx = append(stepwiseIdx, i)
			continue
		}

		legacyIdx = append(legacyIdx, i)
	}

	if len(legacyIdx) == 0 {
		for _, i := range stepwiseIdx {
			batch[i].done <- stepwise.RunEmbeddedPair(
				(*[stepwise.FrameWords]uint64)(batch[i].a),
				(*[stepwise.FrameWords]uint64)(batch[i].b),
			)
		}

		return
	}

	if len(stepwiseIdx) == 0 {
		if len(batch) == 1 {
			err := cpuKernel.UniversalBitwise(batch[0].a, batch[0].b, 1)
			batch[0].done <- err
			return
		}

		frames := make([]cpu.BitwiseFrame, len(batch))
		for i := range batch {
			frames[i] = cpu.BitwiseFrame{
				A: batch[i].a,
				B: batch[i].b,
			}
		}

		err := cpuKernel.UniversalBitwiseFrames(frames)
		for i := range batch {
			batch[i].done <- err
		}

		return
	}

	for _, i := range stepwiseIdx {
		batch[i].done <- stepwise.RunEmbeddedPair(
			(*[stepwise.FrameWords]uint64)(batch[i].a),
			(*[stepwise.FrameWords]uint64)(batch[i].b),
		)
	}

	for _, i := range legacyIdx {
		batch[i].done <- cpuKernel.UniversalBitwise(batch[i].a, batch[i].b, 1)
	}

	return
}

func frameRequiresProgramExecution(ptr unsafe.Pointer) bool {
	if ptr == nil || core.Cfg == nil {
		return false
	}

	words := core.Cfg.Value.Words
	if words <= 0 {
		words = primitive.Words
	}

	frame := unsafe.Slice((*uint64)(ptr), words)

	fwIdx := core.Cfg.Value.Region.Registers.FW
	if fwIdx >= 0 && fwIdx < len(frame) && frame[fwIdx] > 0 {
		return true
	}

	startWord := core.Cfg.Value.Region.Program.Start
	if startWord < 0 || startWord >= len(frame) {
		return false
	}

	progWords := int((core.Cfg.Value.Region.Program.Bits + 63) / 64)
	if progWords <= 0 {
		if startWord == 0 {
			startWord = stepwise.DefaultProgramWordBase
			if startWord >= len(frame) {
				return false
			}
		}

		progWords = len(frame) - startWord
	}

	if progWords <= 0 {
		return false
	}

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

	var q chan bitwiseJob
	if frameRequiresProgramExecution(a) {
		// Program execution (legacy RISC or stepwise) always uses the CPU queue.
		// Accelerators still run the legacy unified bitwise kernel for SIMD-only work.
		q = backend.jobQueueCPU
	} else if len(backend.accel) > 0 {
		q = backend.jobQueueAccel
	} else {
		q = backend.jobQueueCPU
	}

	select {
	case <-backend.ctx.Done():
		return backend.ctx.Err()
	case q <- job:
		return <-done
	}
}

/*
Queue executes the frame at self against a private full-frame copy so inbound
token work participates in the same fold semantics as UniversalBitwise(self, partner)
without compute importing primitive (tests already import compute).
*/
func (backend *Backend) Queue(self unsafe.Pointer) error {

	if backend == nil {
		return errors.New("compute.Backend.Queue: nil backend")
	}
	if self == nil {
		return errors.New("compute.Backend.Queue: nil frame pointer")
	}

	words := core.Cfg.Value.Words
	if words <= 0 {
		return errors.New("compute.Backend.Queue: invalid value.words in config")
	}

	partner := make([]uint64, words)
	src := unsafe.Slice((*uint64)(self), words)
	copy(partner, src)

	return backend.UniversalBitwise(self, unsafe.Pointer(&partner[0]))
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
		if backend.jobQueueAccel != nil {
			close(backend.jobQueueAccel)
		}
		if backend.jobQueueCPU != nil {
			close(backend.jobQueueCPU)
		}
	})
}
