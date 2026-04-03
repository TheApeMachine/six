package compute

import (
	"context"
	"math/bits"
	"strconv"
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
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
)

type QueueType uint

const (
	PRIORITY QueueType = iota
	NORMAL
)

const hardwareEMAAlphaShift = 2

// circuitBreakerThreshold is the number of consecutive failures before
// a substrate is temporarily ejected from the dispatch order. This
// prevents a broken GPU from silently degrading every batch to the CPU
// fallback while the operator has no visibility into the failure.
const circuitBreakerThreshold = 5

// circuitBreakerProbeInterval is the base interval between recovery probes
// for ejected substrates. Each probe attempt doubles the interval (exponential
// backoff) to avoid flapping.
const circuitBreakerProbeInterval = 5 * time.Second

type hardwareMetrics struct {
	inflight            atomic.Int64
	emaServiceNanos     atomic.Uint64
	consecutiveFailures atomic.Int64
	ejected             atomic.Bool
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

/*
MemoryLoadHook resolves LGPXMemoryLoadMark requests after UniversalBitwise.
The query is the uint64 read from the query word index encoded in the slot;
return ok false when no neighbor exists.
*/
type MemoryLoadHook func(queryAffinity uint64) (primitive.Value, bool)

/*
MemoryLoadEnqueueHook returns spatial neighbors for active LSM fetch (opcode 5
with meta flag). Frames are cloned before enqueue so the cold store stays read-only.
*/
type MemoryLoadEnqueueHook func(queryAffinity uint64) []primitive.Value

/*
SemanticAffinityReinsert is invoked when RefreshSemanticAffinityKey changes
the in-frame Affinity word so the control plane can re-index the Value.
*/
type SemanticAffinityReinsert func(value *primitive.Value)

/*
SleepSampleFunc returns scratch frame pairs for offline consolidation (cloned
frames with fresh IDs — see cluster.ControlPlane.SampleSleepScratchPairs).
*/
type SleepSampleFunc func(maxPairs int) [][2]*primitive.Value

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
	memoryLoad               MemoryLoadHook
	memoryEnqueue            MemoryLoadEnqueueHook
	semanticReinsert         SemanticAffinityReinsert
	sleepSample              SleepSampleFunc
	evolution                EvolutionStage
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
WithMemoryLoad registers a hook that satisfies in-band LGPXMemoryLoadMark
requests (see pkg/compute/kernel/cpu extended ISA).
*/
func WithMemoryLoad(hook MemoryLoadHook) BackendOption {
	return func(backend *Backend) {
		backend.memoryLoad = hook
	}
}

/*
WithMemoryEnqueue registers neighbors for active fetch: pending opcode-5 marks
with the meta flag enqueue cloned Values on PRIORITY instead of inlining one word.
*/
func WithMemoryEnqueue(hook MemoryLoadEnqueueHook) BackendOption {
	return func(backend *Backend) {
		backend.memoryEnqueue = hook
	}
}

/*
WithSemanticAffinityReinsert registers a callback after UniversalBitwise when
semantic affinity refresh moves the routing key.
*/
func WithSemanticAffinityReinsert(fn SemanticAffinityReinsert) BackendOption {
	return func(backend *Backend) {
		backend.semanticReinsert = fn
	}
}

/*
WithSleepSample registers a provider of scratch pairs for periodic sleep
consolidation (fixed-interval ticker via core.SubstrateSleepIdle).
*/
func WithSleepSample(fn SleepSampleFunc) BackendOption {
	return func(backend *Backend) {
		backend.sleepSample = fn
	}
}

func (backend *Backend) drainMemoryLoads(frames []unsafe.Pointer) {

	if backend.memoryLoad == nil && backend.memoryEnqueue == nil {
		return
	}

	primitive.ProcessMemoryLoadRequests(
		frames,
		backend.memoryLoad,
		backend.memoryEnqueue,
		backend.enqueuePriorityUnsafe,
	)
}

func (backend *Backend) enqueuePriorityUnsafe(ptr unsafe.Pointer) {

	if backend == nil || ptr == nil {
		return
	}

	queue, ok := backend.queues[PRIORITY]
	if !ok {
		return
	}

	select {
	case queue <- ptr:
	default:
		errnie.Warn(
			"compute.backend: dropped PRIORITY active-fetch frame",
		)
	}
}

/*
runUniversalWithSettling runs UniversalBitwise, drains memory loads, and
optionally repeats until the token region Hamming delta falls below epsilon.
*/
func (backend *Backend) runUniversalWithSettling(group []unsafe.Pointer) error {

	err := backend.universalBitwisePreferredWithFallback(group)
	if err != nil {
		return err
	}

	backend.drainMemoryLoads(group)

	maxPass := core.Cfg.System.TokenSettleMaxPasses
	if maxPass <= 0 {
		maxPass = core.DefaultTokenSettleMaxPasses
	}

	epsilon := core.Cfg.System.TokenSettleEpsilonBits
	tokenBits := int(core.Cfg.Value.Region.Tokens.Bits)
	tokenWords := int((tokenBits + 63) / 64)
	base := core.Cfg.Value.Region.Tokens.Start

	if tokenWords <= 0 || base < 0 {
		return nil
	}

	for pass := 1; pass <= maxPass; pass++ {
		snapshots := make([][]uint64, len(group))

		for index, ptr := range group {
			if ptr == nil {
				continue
			}

			frame := (*[128]uint64)(ptr)
			frameLen := len(frame)
			end := base + tokenWords

			if base < 0 || base >= frameLen || end > frameLen {
				errnie.Warn(
					"compute.backend: token snapshot slice out of bounds; skipping frame",
					"base", base,
					"tokenWords", tokenWords,
					"frameLen", frameLen,
				)

				continue
			}

			buf := make([]uint64, tokenWords)
			copy(buf, frame[base:end])
			snapshots[index] = buf
		}

		err = backend.universalBitwisePreferredWithFallback(group)
		if err != nil {
			return err
		}

		backend.drainMemoryLoads(group)

		settled := true

		for index, ptr := range group {
			if ptr == nil {
				continue
			}

			prev := snapshots[index]
			if len(prev) == 0 {
				continue
			}

			frame := (*[128]uint64)(ptr)
			ham := 0

			for word := 0; word < tokenWords; word++ {
				ham += bits.OnesCount64(
					frame[base+word] ^ prev[word],
				)
			}

			if ham > epsilon {
				settled = false

				break
			}
		}

		if settled {
			break
		}
	}

	return nil
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

	// Wire the evolution lifecycle as a separate stage that intercepts
	// frames after hardware dispatch. This keeps the load balancer free
	// of algorithmic concerns (crossover, signal emission).
	backend.evolution = NewEvolutionManager(backend.onEmit, backend.queues)

	return backend.start()
}

func (backend *Backend) start() *Backend {
	go backend.runUnifiedQueue()

	if backend.sleepSample != nil {
		go backend.runSleepLoop()
	}

	go backend.runCircuitBreakerProbe()

	return backend
}

func (backend *Backend) runSleepLoop() {

	ticker := time.NewTicker(core.SubstrateSleepIdle)
	defer ticker.Stop()

	for {
		select {
		case <-backend.ctx.Done():
			return
		case <-ticker.C:
			backend.runSleepConsolidation()
		}
	}
}

func (backend *Backend) runSleepConsolidation() {

	if backend.sleepSample == nil || backend.evolution == nil {
		return
	}

	maxPairs := core.Cfg.System.SleepMaxPairs
	if maxPairs <= 0 {
		return
	}

	pairs := backend.sleepSample(maxPairs)
	if len(pairs) == 0 {
		return
	}

	for _, pair := range pairs {
		if pair[0] == nil || pair[1] == nil {
			continue
		}

		ptrs := []unsafe.Pointer{
			unsafe.Pointer(pair[0]),
			unsafe.Pointer(pair[1]),
		}

		frameGroups := backend.groupFramesByProgram(ptrs)

		for _, group := range frameGroups {
			if len(group) == 0 {
				continue
			}

			if err := backend.runUniversalWithSettling(group); err != nil {
				errnie.Warn(
					"compute.backend: sleep UniversalBitwise failed",
					"err", err,
				)
			}
		}

		backend.evolution.ProcessBatch(frameGroups, ptrs)

		primitive.ReleaseFrame((*[128]uint64)(unsafe.Pointer(pair[0])))
		primitive.ReleaseFrame((*[128]uint64)(unsafe.Pointer(pair[1])))
	}
}

func compactUnsafePointers(frames []unsafe.Pointer) []unsafe.Pointer {

	out := frames[:0]
	for _, ptr := range frames {
		if ptr != nil {
			out = append(out, ptr)
		}
	}

	return out
}

/*
runCircuitBreakerProbe periodically probes ejected substrates with a minimal
synthetic frame. On success the substrate is readmitted to the dispatch order.
Probes use exponential backoff per substrate to avoid flapping.
*/
func (backend *Backend) runCircuitBreakerProbe() {
	// Per-substrate backoff multiplier (doubles on each failed probe).
	backoff := make([]int, len(backend.hardware))
	for i := range backoff {
		backoff[i] = 1
	}

	ticker := time.NewTicker(circuitBreakerProbeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-backend.ctx.Done():
			return
		case <-ticker.C:
		}

		for i := range backend.hardware {
			if !backend.hardwareState[i].ejected.Load() {
				continue
			}

			// Exponential backoff: only probe when tick count aligns.
			// backoff[i] starts at 1 and doubles on failure, so the first
			// probe fires immediately after ejection.
			if backoff[i] > 1 {
				backoff[i]--
				continue
			}

			// Build a minimal synthetic frame (all zeros — NOP program).
			var probe [128]uint64
			probePtr := unsafe.Pointer(&probe)

			err := backend.hardware[i].UniversalBitwise([]unsafe.Pointer{probePtr})
			if err != nil {
				// Still broken — double the backoff (cap at 64× base interval).
				b := backoff[i] * 2
				if b > 64 {
					b = 64
				}
				backoff[i] = b
				continue
			}

			// Probe succeeded — readmit the substrate.
			backend.hardwareState[i].ejected.Store(false)
			backend.hardwareState[i].consecutiveFailures.Store(0)
			backoff[i] = 1

			substrateName := backend.hardware[i].Name()
			errnie.Info(
				"compute.backend: substrate readmitted after successful probe",
				"substrate", substrateName,
			)
			telemetry.Emit(telemetry.Event{
				Component: "Substrate",
				Action:    "Readmitted",
				Data: telemetry.EventData{
					Stage:   substrateName,
					Message: substrateName + " readmitted after successful probe",
				},
			})
		}
	}
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

		// Retry with backpressure instead of silently dropping the batch.
		// If the pool is saturated, wait briefly and retry up to 3 times
		// before falling back to inline execution.
		var schedErr error
		for attempt := 0; attempt < 3; attempt++ {
			schedErr = backend.Schedule(func(ctx context.Context) error {
				return backend.executeBatch(batch)
			})
			if schedErr == nil {
				break
			}
			// Brief backpressure pause before retry.
			select {
			case <-backend.ctx.Done():
				return
			case <-time.After(time.Duration(attempt+1) * time.Millisecond):
			}
		}
		if schedErr != nil {
			// All retries failed — execute inline rather than dropping data.
			errnie.Warn("compute.backend: pool saturated, executing batch inline")
			if err := backend.executeBatch(batch); err != nil {
				_ = errnie.Error(err)
			}
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

	if ew := core.Cfg.System.EvolutionBatchWindow; ew > coalesce {
		coalesce = ew
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

		// Circuit breaker: skip ejected substrates entirely.
		if metrics.ejected.Load() {
			continue
		}

		metrics.inflight.Add(1)
		start := time.Now()
		runErr = backend.hardware[hardwareIndex].UniversalBitwise(group)
		elapsed := time.Since(start)
		metrics.inflight.Add(-1)
		backend.recordHardwareServiceTime(hardwareIndex, elapsed)

		if runErr == nil {
			// Reset failure counter on success.
			metrics.consecutiveFailures.Store(0)

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

		// Track consecutive failures and eject if threshold is exceeded.
		failures := metrics.consecutiveFailures.Add(1)
		if failures >= int64(circuitBreakerThreshold) && !metrics.ejected.Load() {
			metrics.ejected.Store(true)
			substrateName := backend.hardware[hardwareIndex].Name()
			errnie.Error(NewBackendError(
				BackendErrorSubstrateEjected, runErr,
				substrateName+" ejected after "+
					strconv.FormatInt(failures, 10)+" consecutive failures",
			))
			telemetry.Emit(telemetry.Event{
				Component: "Substrate",
				Action:    "Ejected",
				Data: telemetry.EventData{
					Stage:   substrateName,
					Message: substrateName + " ejected: chronic dispatch failure",
				},
			})
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

	// ── Phase 1: Hardware dispatch ──────────────────────────────────
	// Group by program for SIMD optimization, then dispatch each group
	// to the best available substrate (CUDA → Metal → CPU fallback).
	// This is the ONLY thing the load balancer does with the frames.
	frameGroups := backend.groupFramesByProgram(frames)

	for _, group := range frameGroups {
		if len(group) == 0 {
			continue
		}

		if err := backend.runUniversalWithSettling(group); err != nil {
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
	}

	affWord := core.Cfg.Value.Region.Affinity.Start

	for _, ptr := range frames {
		if ptr == nil {
			continue
		}

		value := (*primitive.Value)(ptr)

		oldAff := uint64(0)
		if affWord >= 0 && affWord < len(*value) {
			oldAff = (*value)[affWord]
		}

		value.RefreshSemanticAffinityKey()

		newAff := uint64(0)
		if affWord >= 0 && affWord < len(*value) {
			newAff = (*value)[affWord]
		}

		if backend.semanticReinsert != nil && oldAff != newAff {
			backend.semanticReinsert(value)
		}
	}

	// ── Phase 2: Evolution lifecycle ────────────────────────────────
	// Delegate to the EvolutionStage for crossover and signal emission.
	// The backend does NOT know about HolographicCrossover, parentBias,
	// or EmitFromSignals — that's the evolution stage's domain.
	if backend.evolution != nil {
		backend.evolution.ProcessBatch(frameGroups, frames)
	}

	idWord := core.Cfg.Value.Region.ID.Start

	for index := range frames {
		ptr := frames[index]
		if ptr == nil {
			continue
		}

		frame := (*[128]uint64)(ptr)

		primitive.ApplyThermodynamicDecay(frame)
		if primitive.ThermodynamicIsExhausted(frame) {
			vid := frame[idWord]

			primitive.MacroGraphDiscard(vid)

			value := (*primitive.Value)(frame)
			_ = value.InstallFirmware(core.FirmwareTypeTombstone)

			primitive.ReleaseFrame(frame)
			frames[index] = nil
		}
	}

	frames = compactUnsafePointers(frames)

	// ── Phase 3: Telemetry and follow-up ────────────────────────────
	prevWord := core.Cfg.Value.Region.Prev.Start
	nextWord := core.Cfg.Value.Region.Next.Start
	for _, ptr := range frames {
		if ptr == nil {
			continue
		}

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

// Evolution methods (evolveProgramsInGroup, emitSignalsInBatch) have been
// extracted to evolution.go behind the EvolutionStage interface. The Backend
// no longer owns evolutionary behavior — it delegates to backend.evolution
// after hardware dispatch completes in executeBatch.

func (backend *Backend) handleFollowUp(frames []unsafe.Pointer) {
	fwWord := core.Cfg.Value.Region.Registers.FW
	pcWord := core.Cfg.Value.Region.PC.Start

	var elites *EliteArchive

	if em, ok := backend.evolution.(*EvolutionManager); ok {
		elites = em.EliteArchive()
	}

	for _, value := range frames {
		if value == nil {
			continue
		}

		frame := (*[128]uint64)(value)

		/*
			Dynamic skill morph: FW values above FirmwareTypePrompt are treated as
			MAP-Elites host keys; the matching elite program band replaces the frame
			program and FW drops back to Learn for the next flat kernel sweep.
		*/
		if elites != nil && fwWord >= 0 && fwWord < len(frame) {
			fwVal := frame[fwWord]

			if fwVal > uint64(core.FirmwareTypePrompt) {
				if pcWord >= 0 && pcWord < len(frame) {
					bin := EliteBinFromHostKey(fwVal)
					band, haveBand := elites.LookupBand(bin)

					if haveBand && len(band) > 0 {
						applyProgramBand(frame, band)
					}

					frame[pcWord] = uint64(core.Cfg.Value.Region.Program.Start)
				}

				frame[fwWord] = core.FirmwareRegisterLearn
			}
		}

		if frameShouldSkipFollowUp(frame) {
			frame[fwWord] = 0
			// Release the frame back to the pool and clean up token metadata
			// to prevent the global sync.Map from growing indefinitely.
			primitive.ReleaseFrame(frame)
			continue
		}

		if frame[fwWord] == 0 {
			primitive.ReleaseFrame(frame)
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
			primitive.ReleaseFrame(frame)
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
		if ptr == nil {
			continue
		}

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

	bestIndex := -1
	var bestDepth int64
	var bestEMA uint64

	for index := 0; index < len(backend.hardware); index++ {
		// Skip ejected substrates — they've exceeded the failure threshold.
		if backend.hardwareState[index].ejected.Load() {
			continue
		}

		depth := backend.hardwareState[index].inflight.Load()
		ema := backend.hardwareState[index].emaServiceNanos.Load()

		if bestIndex < 0 || depth < bestDepth || (depth == bestDepth && ema < bestEMA) {
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
