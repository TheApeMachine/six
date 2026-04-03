package compute

import (
	"context"
	"math/rand"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/firmware"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/compute/kernel/metal"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/telemetry"
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
/*
EmitCallback is invoked by the backend whenever signal emission creates new
Values. The callback is responsible for inserting the Value into the spatial
index (control plane). The backend handles re-queuing for execution.
*/
type EmitCallback func(value *primitive.Value)

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
	onEmit                   EmitCallback
}

// BackendOption configures the multi-substrate router.
type BackendOption func(*Backend)

// WithEmitCallback registers a callback invoked for every child Value
// created by signal emission. The callback should insert the Value into
// the spatial index (control plane).
func WithEmitCallback(fn EmitCallback) BackendOption {
	return func(backend *Backend) {
		backend.onEmit = fn
	}
}

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
	go backend.runUnifiedQueue()

	return backend
}

/*
runUnifiedQueue gathers batches from both queues into a single execution
pipeline. PRIORITY is drained first so follow-up Values (loop/branch)
get processed ahead of new ingress, but everything ends up in the same
batch — which is critical for signal emission to pair prompts with
corpus Values.
*/
func (backend *Backend) runUnifiedQueue() {
	priority := backend.queues[PRIORITY]
	normal := backend.queues[NORMAL]

	for {
		// Block until at least one Value is available from either queue.
		var first unsafe.Pointer
		select {
		case <-backend.ctx.Done():
			return
		case first = <-priority:
		case first = <-normal:
		}

		batch := backend.gatherUnifiedBatch(priority, normal, first)
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

func (backend *Backend) gatherUnifiedBatch(priority, normal <-chan unsafe.Pointer, first unsafe.Pointer) []unsafe.Pointer {
	if first == nil {
		return nil
	}

	if backend.batchSize <= 1 {
		return []unsafe.Pointer{first}
	}

	batch := make([]unsafe.Pointer, 1, backend.batchSize)
	batch[0] = first

	// Drain priority first (non-blocking).
	for len(batch) < backend.batchSize {
		select {
		case value := <-priority:
			if value != nil {
				batch = append(batch, value)
			}
		default:
			goto drainNormal
		}
	}
	return batch

drainNormal:
	// Then drain normal (non-blocking).
	for len(batch) < backend.batchSize {
		select {
		case value := <-normal:
			if value != nil {
				batch = append(batch, value)
			}
		default:
			goto coalesce
		}
	}
	return batch

coalesce:
	// If we haven't filled the batch, wait up to coalesce window for more.
	coalesce := backend.gatherCoalesceDuration()
	if coalesce <= 0 {
		return batch
	}

	timer := time.NewTimer(coalesce)
	defer timer.Stop()

	for len(batch) < backend.batchSize {
		select {
		case <-backend.ctx.Done():
			return batch
		case <-timer.C:
			return batch
		case value := <-priority:
			if value != nil {
				batch = append(batch, value)
			}
		case value := <-normal:
			if value != nil {
				batch = append(batch, value)
			}
		}
	}

	return batch
}

func (backend *Backend) gatherCoalesceDuration() time.Duration {
	coalesce := backend.batchWindow

	if core.Cfg.System.ProgramEvolution {
		if ew := core.Cfg.System.EvolutionBatchWindow; ew > coalesce {
			coalesce = ew
		}
	}

	return coalesce
}

// gatherBatch is no longer used — replaced by gatherUnifiedBatch which
// drains both PRIORITY and NORMAL into a single batch. Kept as a
// compile-time reminder in case any caller still references it.


/*
universalBitwisePreferredWithFallback runs UniversalBitwise on the load-balanced
substrate first, then tries the rest in registration order.

NVML can report GPUs while the active Go build uses a CUDA stub whose
UniversalBitwise always errors; Metal can be unavailable on non-darwin or cgo.
Without fallback, index 0 would fail and the CPU interpreter never runs, so
queued frames appear to bypass “backend processing” entirely.
*/
func (backend *Backend) universalBitwisePreferredWithFallback(
	group []unsafe.Pointer,
) error {
	if len(backend.hardware) == 0 {
		return NewBackendError(BackendErrorNoHardware, nil, "executeBatch")
	}

	preferred := backend.selectPreferredHardwareIndexForUniversalBitwise()
	if preferred < 0 {
		return NewBackendError(BackendErrorNoHardware, nil, "executeBatch")
	}

	order := make([]int, 0, len(backend.hardware))
	seen := make(map[int]struct{}, len(backend.hardware))

	appendIndex := func(index int) {
		if index < 0 || index >= len(backend.hardware) {
			return
		}

		if _, exists := seen[index]; exists {
			return
		}

		seen[index] = struct{}{}
		order = append(order, index)
	}

	appendIndex(preferred)

	for index := range backend.hardware {
		appendIndex(index)
	}

	var runErr error

	for _, hardwareIndex := range order {
		metrics := &backend.hardwareState[hardwareIndex]
		metrics.inflight.Add(1)
		start := time.Now()
		runErr = backend.hardware[hardwareIndex].UniversalBitwise(group)
		elapsed := time.Since(start)
		metrics.inflight.Add(-1)
		backend.recordHardwareServiceTime(hardwareIndex, elapsed)

		if runErr == nil {
			substrateName := backend.hardware[hardwareIndex].Name()
			telemetry.Emit(telemetry.Event{
				Component: "Substrate",
				Action:    "Step",
				Data: telemetry.EventData{
					Stage:        substrateName,
					UbFrameCount: len(group),
					DurationMs:   int(elapsed.Milliseconds()),
					Message:      substrateName + " executed batch",
				},
			})
			return nil
		}
	}

	return runErr
}

func (backend *Backend) executeBatch(frames []unsafe.Pointer) error {
	if len(frames) == 0 {
		return nil
	}

	telemetry.Emit(telemetry.Event{
		Component: "Substrate",
		Action:    "Run",
		Data: telemetry.EventData{
			Stage:        "batch-start",
			UbFrameCount: len(frames),
			Message:      "executing batch on compute substrate",
		},
	})

	// Phase 1: Execute each program group via UniversalBitwise + evolve.
	// Grouping by program is a SIMD optimization for execution only.
	frameGroups := backend.groupFramesByProgram(frames)

	for _, group := range frameGroups {
		if len(group) == 0 {
			continue
		}

		if err := backend.universalBitwisePreferredWithFallback(group); err != nil {
			return err
		}

		telemetry.Emit(telemetry.Event{
			Component: "Substrate",
			Action:    "Run",
			Data: telemetry.EventData{
				Stage:        "group-complete",
				UbFrameCount: len(group),
			},
		})

		// Evolution is within program groups (same-program crossover).
		backend.evolveProgramsInGroup(group)
	}

	// Phase 2: Signal emission across the FULL batch (cross-group).
	// Signals are about token-region structure, not program similarity.
	// A prompt and a corpus Value with different programs can still
	// produce strong signals — this is how prompts find structure.
	backend.emitSignalsInBatch(frames)

	// Phase 3: Telemetry and follow-up on all frames.
	idWord := core.Cfg.Value.Region.ID.Start
	prevWord := core.Cfg.Value.Region.Prev.Start
	nextWord := core.Cfg.Value.Region.Next.Start
	for _, ptr := range frames {
		frame := (*[128]uint64)(ptr)
		vid := frame[idWord]
		pid := frame[prevWord]
		nid := frame[nextWord]
		if pid != 0 || nid != 0 {
			telemetry.Emit(telemetry.Event{
				Component: "Value",
				Action:    "Frame",
				Data: telemetry.EventData{
					NodeID: vid,
					FromID: pid,
					ToID:   nid,
				},
			})
		}
	}

	backend.handleFollowUp(frames)

	return nil
}

/*
emitSignalsInBatch scans signals between adjacent frame pairs across the
full batch (not per program group) after all UniversalBitwise groups have
executed. Signals are about token-region structure, not program similarity —
a prompt Value and a corpus Value with completely different programs can
produce strong signals. This is how prompts discover structure.

New children are inserted into the spatial index via onEmit and queued
for execution.
*/
func (backend *Backend) emitSignalsInBatch(group []unsafe.Pointer) {
	if len(group) < 2 {
		return
	}

	// Build a deterministic RNG from the group shape (same approach as evolve).
	seed := uint64(len(group)) * 0x517CC1B727220A95
	idWord := core.Cfg.Value.Region.ID.Start

	for index, ptr := range group {
		frame := (*[128]uint64)(ptr)
		if idWord >= 0 && idWord < len(frame) {
			seed ^= frame[idWord]
		}
		seed ^= uint64(index+1) * 0x9E3779B97F4A7C15
	}

	rng := rand.New(rand.NewSource(int64(seed ^ (seed >> 32))))

	nextWord := core.Cfg.Value.Region.Next.Start

	for pairIdx := 0; pairIdx+1 < len(group); pairIdx += 2 {
		frameA := (*[128]uint64)(group[pairIdx])
		frameB := (*[128]uint64)(group[pairIdx+1])

		a := primitive.Value(*frameA)
		b := primitive.Value(*frameB)

		children := primitive.EmitFromSignals(&a, &b, rng)
		if len(children) == 0 {
			continue
		}

		// Link parent A → first child via NextID so the chain is walkable.
		// This updates the ORIGINAL frame in-place (via unsafe pointer),
		// which is visible to waitForPrompt and subsequent reads.
		firstChildID := children[0][idWord]
		if firstChildID != 0 && frameA[nextWord] == 0 {
			frameA[nextWord] = firstChildID

			// Re-insert parent A into the spatial index so the updated
			// NextID is visible to chain walkers (FrameByValueID).
			if backend.onEmit != nil {
				parentVal := primitive.Value(*frameA)
				backend.onEmit(&parentVal)
			}
		}

		for _, child := range children {
			// Notify the spatial index (control plane) about the new Value.
			if backend.onEmit != nil {
				backend.onEmit(child)
			}

			// Queue the child for execution.
			ptr := unsafe.Pointer(child)
			select {
			case backend.queues[NORMAL] <- ptr:
			default:
				errnie.Warn(
					"compute.backend: dropped emitted child",
					"child_id", child[idWord],
				)
			}
		}

		telemetry.Emit(telemetry.Event{
			Component: "Substrate",
			Action:    "Emit",
			Data: telemetry.EventData{
				Stage:        "signal-emission",
				UbFrameCount: len(children),
				Message:      "emitted child Values from signal detection",
			},
		})
	}
}

/*
evolveProgramsInGroup performs pairwise HolographicCrossover on adjacent frames
within a UniversalBitwise batch group when system.programEvolution is enabled.

HolographicCrossover blends two parents plus a structured third-parent noise
source via majority-rule in HIE (holographic instruction encoding) space. The
parentBias parameter steers between exploration (0 = pure affine noise orbit)
and exploitation (1 = collapse to donor). SubstrateExploitScore provides the
bias: high token-region structure similarity → exploit, low → explore.

This replaces the earlier HomologousCrossover which could not bootstrap from
NOP-only programs since it only recombines existing effective instructions.

The RNG is seeded from batch shape and frame IDs so runs are reproducible for
a given queued ordering without introducing yet another global entropy source.
*/
func (backend *Backend) evolveProgramsInGroup(group []unsafe.Pointer) {
	if !core.Cfg.System.ProgramEvolution {
		return
	}

	if len(group) < 2 {
		return
	}

	seed := uint64(len(group)) * 0x9E3779B97F4A7C15
	idWord := core.Cfg.Value.Region.ID.Start

	for index, ptr := range group {
		frame := (*[128]uint64)(ptr)
		if idWord >= 0 && idWord < len(frame) {
			seed ^= frame[idWord]
		}

		seed ^= uint64(index+1) * 0x85EBCA6B
	}

	rng := rand.New(rand.NewSource(int64(seed ^ (seed >> 32))))

	for pairIdx := 0; pairIdx+1 < len(group); pairIdx += 2 {
		recipient := (*[128]uint64)(group[pairIdx])
		donor := (*[128]uint64)(group[pairIdx+1])

		// Compute parentBias from token-region structure similarity.
		// High overlap (sharp structure) → exploit (bias → 1, less noise).
		// Low overlap (NOP-only, early bootstrap) → explore (bias → 0, max noise).
		recipientValue := primitive.Value(*recipient)
		donorValue := primitive.Value(*donor)
		parentBias := primitive.SubstrateExploitScore(&recipientValue, &donorValue)

		firmware.HolographicCrossover(recipient, recipient, donor, rng, parentBias)
	}
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

/*
selectPreferredHardwareIndexForUniversalBitwise chooses the first substrate to
try for UniversalBitwise. When telemetry.universal_bitwise_slots is enabled,
the CPU path is preferred first so per-slot hooks (pkg/compute/kernel/cpu) run;
NewBackend appends cpu.NewBackend last, so that index is len(hardware)-1.
GPU substrates remain in the fallback chain if the CPU path errors.
*/
func (backend *Backend) selectPreferredHardwareIndexForUniversalBitwise() int {
	if len(backend.hardware) == 0 {
		return -1
	}

	if core.Cfg.TelemetryUniversalBitwiseSlots {
		return len(backend.hardware) - 1
	}

	return backend.selectHardwareIndex()
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
Queue a new value for execution. All new Values enter via the NORMAL
queue. The PRIORITY queue is reserved for follow-up scheduling
(loop/branch re-execution via handleFollowUp). Batch gathering drains
PRIORITY first so follow-ups get priority without being isolated from
new ingress.
*/
func (backend *Backend) Queue(value unsafe.Pointer) error {
	if value == nil {
		return NewBackendError(BackendErrorNoValues, nil, "Queue")
	}

	queue, ok := backend.queues[NORMAL]
	if !ok {
		return NewBackendError(BackendErrorNoComputeResource, nil, "Queue")
	}

	telemetry.Emit(telemetry.Event{
		Component: "Backend",
		Action:    "Queue",
		Data: telemetry.EventData{
			Stage:     "enqueue",
			QueueSize: len(queue),
			Message:   "frame queued for execution",
		},
	})

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
		errnie.Debug("compute.backend.Schedule", "action", "scheduling job on pool")

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
