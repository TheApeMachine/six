//go:build darwin && cgo

package metal

/*
#cgo CXXFLAGS: -x objective-c++
#cgo LDFLAGS: -framework Metal -framework Foundation
#include "metal.h"
#include <stdlib.h>
*/
import "C"
import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
)

/*
metalMu serializes all CGO calls into Metal. Metal command buffer
submission from concurrent goroutines triggers a fatal
"semasleep on Darwin signal stack" crash. A single mutex here is
the cheapest correct fix — the GPU work itself is still parallel
on-device; we only serialize the host-side dispatch.
*/
var metalMu sync.Mutex
var batchBuffer []byte

//go:generate xcrun -sdk macosx metal -std=metal3.1 -mmacosx-version-min=14.0 -c backend.metal -o backend.air
//go:generate xcrun -sdk macosx metallib backend.air -o backend.metallib

//go:embed backend.metallib
var backendMetallib []byte

var metalReady atomic.Bool

/*
Backend dispatches Value-native GPU kernels on Apple Silicon.
All buffers use StorageModeShared (unified memory) so there is no
host-to-device copy — the CPU and GPU share the same physical RAM.
*/
type Backend struct {
	idx      int
	observer kernel.Observer
}

type backendOption func(*Backend)

/*
NewBackend returns a Metal kernel Backend.
*/
func NewBackend(idx int, opts ...backendOption) *Backend {
	backend := &Backend{
		idx:      idx,
		observer: kernel.NoopObserver{},
	}
	for _, opt := range opts {
		opt(backend)
	}
	backend.observer = kernel.NormalizeObserver(backend.observer)
	return backend
}

// BackendWithObserver injects a kernel observer used for optional trace/error
// reporting. Pass nil to disable.
func BackendWithObserver(observer kernel.Observer) backendOption {
	return func(backend *Backend) {
		backend.observer = kernel.NormalizeObserver(observer)
	}
}

// SetObserver updates the backend observer at runtime.
func (backend *Backend) SetObserver(observer kernel.Observer) {
	backend.observer = kernel.NormalizeObserver(observer)
}

/*
Available returns the number of Metal-capable GPUs present on this system,
or an error if the Metal runtime failed to initialize.
*/
func Available() int {
	return int(C.count_metal_devices())
}

/*
Execute dispatches frames to the appropriate Metal kernel based on
the opcode in each Value's program region (word 16). Batch distance
frames (opcode 0x6 with count at word 124) route to the fused
XOR+popcount kernel. Geometric opcodes route to the PGA kernel.
All other frames go through the unified bitwise kernel.
*/
func (backend *Backend) Execute(frames []unsafe.Pointer) error {
	if !metalReady.Load() {
		return NewMetalKernelError(
			kernel.KernelErrUnavailable,
			errors.New("metal backend not initialized"),
			"Execute",
		)
	}

	if len(frames) == 0 {
		return nil
	}

	metalMu.Lock()
	defer metalMu.Unlock()

	unifiedBatch := make([]unsafe.Pointer, 0, len(frames))
	var unifiedBatchCount int

	flushUnified := func() error {
		if unifiedBatchCount == 0 {
			return nil
		}

		reqBytes := unifiedBatchCount * 1024
		if cap(batchBuffer) < reqBytes {
			batchBuffer = make([]byte, reqBytes)
		}
		batchBuffer = batchBuffer[:reqBytes]

		for i, ptr := range unifiedBatch {
			copy(batchBuffer[i*1024:(i+1)*1024], unsafe.Slice((*byte)(ptr), 1024))
		}

		if C.unified_bitwise_metal(unsafe.Pointer(&batchBuffer[0]), C.uint32_t(unifiedBatchCount)) != 0 {
			err := NewMetalKernelError(
				kernel.KernelErrDispatchFailed, nil, "Execute",
			)
			backend.observer.Error("metal.Backend.Execute", err)
			return err
		}

		for i, ptr := range unifiedBatch {
			copy(unsafe.Slice((*byte)(ptr), 1024), batchBuffer[i*1024:(i+1)*1024])
		}

		unifiedBatch = unifiedBatch[:0]
		unifiedBatchCount = 0
		return nil
	}

	for _, ptr := range frames {
		if ptr == nil {
			continue
		}

		v := (*[128]uint64)(ptr)
		rawOpcode := v[kernel.ProgramStartWord] & 0xFF
		opcode := rawOpcode & kernel.OpcodeBooleanMask
		batchCount := v[kernel.NearestAffinityBatchWord]

		if batchCount > uint64(kernel.MaxNearestAffinityCandidates) {
			batchCount = uint64(kernel.MaxNearestAffinityCandidates)
		}

		if kernel.IsGeometricOpcode(rawOpcode) {
			if err := flushUnified(); err != nil {
				return err
			}

			if C.geometric_metal(ptr, 1) != 0 {
				err := NewMetalKernelError(
					kernel.KernelErrDispatchFailed,
					errors.New("geometric dispatch failed"),
					"Execute",
				)

				backend.observer.Error("metal.Backend.Execute", err, kernel.CorrelationKeyvalsFlat(ptr)...)

				return err
			}

			continue
		}

		if opcode == kernel.OpcodeXOR && batchCount > 0 {
			if err := flushUnified(); err != nil {
				return err
			}

			distances := (*[256]uint32)(unsafe.Pointer(&v[kernel.SignalsStartWord]))

			if C.nearest_affinity_metal(
				unsafe.Pointer(&v[0]),
				unsafe.Pointer(&v[kernel.NearestAffinityCandidatesStartWord]),
				C.uint32_t(batchCount),
				(*C.uint32_t)(unsafe.Pointer(&distances[0])),
			) != 0 {
				err := NewMetalKernelError(
					kernel.KernelErrDispatchFailed, nil, "Execute",
				)

				backend.observer.Error("metal.Backend.Execute", err, kernel.CorrelationKeyvalsFlat(ptr)...)

				return err
			}

			bestIdx := uint64(0)
			bestDist := uint64(distances[0])

			for idx := uint64(1); idx < batchCount; idx++ {
				dist := uint64(distances[idx])

				if dist < bestDist {
					bestDist = dist
					bestIdx = idx
				}
			}

			v[kernel.SignalsStartWord+kernel.SignalBestIdxOffset] = bestIdx
			v[kernel.SignalsStartWord+kernel.SignalBestDistOffset] = bestDist

			continue
		}

		unifiedBatch = append(unifiedBatch, ptr)
		unifiedBatchCount++
	}

	return flushUnified()
}

/*
NearestAffinity computes Hamming distances from query to all candidates
on the GPU and returns per-candidate distances. The caller reduces argmin.
*/
func (backend *Backend) NearestAffinity(
	query unsafe.Pointer, candidates unsafe.Pointer, count int,
) ([]uint32, error) {
	if !metalReady.Load() {
		return nil, NewMetalKernelError(
			kernel.KernelErrUnavailable,
			errors.New("metal backend not initialized"),
			"NearestAffinity",
		)
	}

	distances := make([]uint32, count)

	metalMu.Lock()
	defer metalMu.Unlock()

	if C.nearest_affinity_metal(
		query,
		candidates,
		C.uint32_t(count),
		(*C.uint32_t)(unsafe.Pointer(&distances[0])),
	) != 0 {
		return nil, NewMetalKernelError(
			kernel.KernelErrDispatchFailed, nil, "NearestAffinity",
		)
	}

	return distances, nil
}

func init() {
	tmpFile, err := os.CreateTemp("", "backend-*.metallib")

	if err != nil {
		reportInitError(err)
		return
	}

	name := tmpFile.Name()

	defer func() {
		_ = os.Remove(name)
	}()

	if _, err := tmpFile.Write(backendMetallib); err != nil {
		tmpFile.Close()
		reportInitError(err)
		return
	}

	if err := tmpFile.Close(); err != nil {
		reportInitError(err)
		return
	}

	cPath := C.CString(name)
	defer C.free(unsafe.Pointer(cPath))

	if res := C.init_metal(cPath); res != 0 {
		reportInitError(NewMetalKernelError(kernel.KernelErrInitFailed, nil, "init_metal"))
		return
	}

	metalReady.Store(true)
}

func reportInitError(err error) {
	if err == nil {
		return
	}
	_, _ = fmt.Fprintf(os.Stderr, "metal backend init: %v\n", err)
}

func (backend *Backend) Schedule(job func(ctx context.Context) error) error {
	return job(context.Background())
}

func (backend *Backend) Name() string { return "metal" }
