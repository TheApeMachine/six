package compute

import (
	"context"
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

type QueueType uint

const (
	PRIORITY QueueType = iota
	NORMAL
)

const hardwareEMAAlphaShift = 2

type hardwareMetrics struct {
	inflight        atomic.Int64
	emaServiceNanos atomic.Uint64
}

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
	ctx                      context.Context
	cancel                   context.CancelFunc
	hardware                 []kernel.Substrate
	pool                     *Pool
	batchSize                int
	batchWindow              time.Duration
	queues                   map[QueueType]chan unsafe.Pointer
	hardwareState            []hardwareMetrics
	droppedPriorityFollowUps atomic.Uint64
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
		batchSize:   core.Cfg.System.BatchSize,
		batchWindow: core.Cfg.System.BatchWindow,
		queues: map[QueueType]chan unsafe.Pointer{
			PRIORITY: make(chan unsafe.Pointer, 1024),
			NORMAL:   make(chan unsafe.Pointer, 1024),
		},
	}

	for _, opt := range opts {
		opt(backend)
	}

	if err := validate.Require(map[string]any{
		"ctx":         backend.ctx,
		"cancel":      backend.cancel,
		"queues":      backend.queues,
		"batchSize":   backend.batchSize,
		"batchWindow": backend.batchWindow,
	}); err != nil {
		errnie.Error(err)
		return nil
	}

	for idx := 0; idx < cuda.Available(); idx++ {
		errnie.Info("compute.backend: CUDA substrate registered")
		backend.hardware = append(backend.hardware, cuda.NewBackend(idx))
	}

	for idx := 0; idx < metal.Available(); idx++ {
		errnie.Info("compute.backend: Metal substrate registered")
		backend.hardware = append(backend.hardware, metal.NewBackend(idx))
	}

	errnie.Info("compute.backend: CPU substrate registered")
	backend.hardware = append(backend.hardware, cpu.NewBackend(backend.ctx))
	backend.ensureHardwareMetrics()

	if err := validate.Require(map[string]any{
		"hardware": backend.hardware,
	}); err != nil {
		errnie.Error(err)
		return nil
	}

	return backend.start()
}

func (backend *Backend) start() *Backend {
	go backend.runQueue(PRIORITY)
	go backend.runQueue(NORMAL)

	return backend
}

func (backend *Backend) runQueue(queueType QueueType) {
	queue := backend.queues[queueType]

	for {
		select {
		case <-backend.ctx.Done():
			return
		case value := <-queue:
			batch := backend.gatherBatch(queue, value)
			if len(batch) == 0 {
				continue
			}

			if err := backend.Schedule(func(ctx context.Context) error {
				return backend.executeBatch(batch)
			}); err != nil {
				_ = errnie.Error(err)
			}
		}
	}
}

func (backend *Backend) gatherBatch(queue <-chan unsafe.Pointer, first unsafe.Pointer) []unsafe.Pointer {
	if first == nil {
		return nil
	}

	if backend.batchSize <= 1 {
		return []unsafe.Pointer{first}
	}

	batch := make([]unsafe.Pointer, 1, backend.batchSize)
	batch[0] = first

	if backend.batchWindow <= 0 {
		for len(batch) < backend.batchSize {
			select {
			case value := <-queue:
				if value == nil {
					continue
				}
				batch = append(batch, value)
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
		case <-timer.C:
			return batch
		case value := <-queue:
			if value == nil {
				continue
			}
			batch = append(batch, value)
		}
	}

	return batch
}

func (backend *Backend) executeBatch(frames []unsafe.Pointer) error {
	if len(frames) == 0 {
		return nil
	}

	frameGroups := backend.groupFramesByProgram(frames)

	for _, group := range frameGroups {
		if len(group) == 0 {
			continue
		}

		hardwareIndex := backend.selectHardwareIndex()
		if hardwareIndex < 0 {
			return NewBackendError(BackendErrorNoHardware, nil, "executeBatch")
		}

		metrics := &backend.hardwareState[hardwareIndex]
		metrics.inflight.Add(1)
		start := time.Now()
		err := backend.hardware[hardwareIndex].UniversalBitwise(group)
		metrics.inflight.Add(-1)
		backend.recordHardwareServiceTime(hardwareIndex, time.Since(start))
		if err != nil {
			return err
		}

		backend.handleFollowUp(group)
	}

	return nil
}

func (backend *Backend) handleFollowUp(frames []unsafe.Pointer) {
	fwWord := core.Cfg.Value.Region.Registers.FW

	for _, value := range frames {
		frame := (*[128]uint64)(value)

		if frameShouldSkipFollowUp(frame) {
			frame[fwWord] = 0
			continue
		}

		if frame[fwWord] == 0 {
			continue
		}

		select {
		case backend.queues[PRIORITY] <- value:
		default:
			dropped := backend.droppedPriorityFollowUps.Add(1)
			errnie.Warn(
				"compute.backend: dropped priority follow-up",
				"dropped_total", dropped,
				"fw", frame[fwWord],
			)
		}
	}
}

func frameShouldSkipFollowUp(frame *[128]uint64) bool {
	if frame == nil {
		return true
	}

	wiped := true

	checkRegion := func(start int, bits uint64) {
		if !wiped {
			return
		}

		nWords := int((bits + 63) / 64)

		for offset := 0; offset < nWords; offset++ {
			index := start + offset

			if index < 0 || index >= len(frame) {
				continue
			}

			if frame[index] != 0 {
				wiped = false
				return
			}
		}
	}

	checkRegion(core.Cfg.Value.Region.Tokens.Start, core.Cfg.Value.Region.Tokens.Bits)
	checkRegion(core.Cfg.Value.Region.Affinity.Start, core.Cfg.Value.Region.Affinity.Bits)
	checkRegion(core.Cfg.Value.Region.Program.Start, core.Cfg.Value.Region.Program.Bits)

	return wiped
}

func (backend *Backend) groupFramesByProgram(frames []unsafe.Pointer) [][]unsafe.Pointer {
	if len(frames) <= 1 {
		return [][]unsafe.Pointer{frames}
	}

	progStart := core.Cfg.Value.Region.Program.Start
	nProgWords := int((core.Cfg.Value.Region.Program.Bits + 63) / 64)
	type programGroup struct {
		fingerprint uint64
		frames      []unsafe.Pointer
	}

	groups := make([]programGroup, 0, len(frames))
	groupIndexByFingerprint := make(map[uint64][]int, len(frames))

	for _, ptr := range frames {
		frame := (*[128]uint64)(ptr)
		fingerprint := programFingerprint(frame, progStart, nProgWords)
		candidates := groupIndexByFingerprint[fingerprint]
		matched := false

		for _, groupIndex := range candidates {
			groupFrame := (*[128]uint64)(groups[groupIndex].frames[0])
			if sameProgramFrame(groupFrame, frame, progStart, nProgWords) {
				groups[groupIndex].frames = append(groups[groupIndex].frames, ptr)
				matched = true
				break
			}
		}

		if matched {
			continue
		}

		groupIndex := len(groups)
		groups = append(groups, programGroup{
			fingerprint: fingerprint,
			frames:      []unsafe.Pointer{ptr},
		})
		groupIndexByFingerprint[fingerprint] = append(groupIndexByFingerprint[fingerprint], groupIndex)
	}

	out := make([][]unsafe.Pointer, 0, len(groups))
	for _, group := range groups {
		out = append(out, group.frames)
	}

	return out
}

func programFingerprint(frame *[128]uint64, progStart int, nProgWords int) uint64 {
	const offset = uint64(14695981039346656037)
	const prime = uint64(1099511628211)

	if frame == nil {
		return 0
	}

	hash := offset
	for word := 0; word < nProgWords; word++ {
		index := progStart + word
		if index < 0 || index >= len(frame) {
			break
		}

		value := frame[index]
		for shift := 0; shift < 64; shift += 8 {
			hash ^= uint64(byte(value >> shift))
			hash *= prime
		}
	}

	return hash
}

func sameProgramFrame(left, right *[128]uint64, progStart int, nProgWords int) bool {
	if left == nil || right == nil {
		return false
	}

	for word := 0; word < nProgWords; word++ {
		index := progStart + word
		if index < 0 || index >= len(left) || index >= len(right) {
			return false
		}

		if left[index] != right[index] {
			return false
		}
	}

	return true
}

func (backend *Backend) ensureHardwareMetrics() {
	if len(backend.hardwareState) == len(backend.hardware) {
		return
	}

	backend.hardwareState = make([]hardwareMetrics, len(backend.hardware))
}

func (backend *Backend) selectHardwareIndex() int {
	if len(backend.hardware) == 0 {
		return -1
	}

	backend.ensureHardwareMetrics()

	bestIndex := 0
	bestDepth := backend.hardwareState[0].inflight.Load()
	bestEMA := backend.hardwareState[0].emaServiceNanos.Load()

	for index := 1; index < len(backend.hardware); index++ {
		depth := backend.hardwareState[index].inflight.Load()
		ema := backend.hardwareState[index].emaServiceNanos.Load()

		if depth < bestDepth || (depth == bestDepth && ema < bestEMA) {
			bestIndex = index
			bestDepth = depth
			bestEMA = ema
		}
	}

	return bestIndex
}

func (backend *Backend) recordHardwareServiceTime(index int, duration time.Duration) {
	if index < 0 || index >= len(backend.hardwareState) {
		return
	}

	sample := uint64(duration)
	metric := &backend.hardwareState[index].emaServiceNanos

	for {
		current := metric.Load()

		var next uint64
		if current == 0 {
			next = sample
		} else if sample >= current {
			next = current + ((sample - current) >> hardwareEMAAlphaShift)
		} else {
			next = current - ((current - sample) >> hardwareEMAAlphaShift)
		}

		if metric.CompareAndSwap(current, next) {
			return
		}
	}
}

/*
Queue a new value for execution.
This prepares the value for execution and potentially optimizes
the execution path by batching similar values together.
*/
func (backend *Backend) Queue(value unsafe.Pointer) error {
	if value == nil {
		return NewBackendError(BackendErrorNoValues, nil, "Queue")
	}

	queue, ok := backend.queues[NORMAL]

	if !ok {
		return NewBackendError(BackendErrorNoComputeResource, nil, "Queue")
	}

	queue <- value
	return nil
}

/*
Schedule pushes work onto the pool when configured; otherwise runs the job
inline with backend.ctx. Returns nil on success, or a wrapped error on pool
enqueue failure / context cancellation / inline job failure.
*/
func (backend *Backend) Schedule(job func(ctx context.Context) error) error {
	if backend.pool != nil {
		errnie.Trace("compute.backend.Schedule", "action", "scheduling job on pool")

		if err := backend.pool.Schedule(backend.ctx, job); err != nil {
			return errnie.Error(NewBackendError(
				BackendErrorPoolEnqueueFailed, err, "Schedule",
			))
		}

		return nil
	}

	if err := job(backend.ctx); err != nil {
		return errnie.Error(NewBackendError(
			BackendErrorInlineJobFailed, err, "Schedule",
		))
	}

	return nil
}
