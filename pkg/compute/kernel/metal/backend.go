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
	"sync/atomic"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
)

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
XOR+popcount kernel. All others go through the unified bitwise
kernel which reads the opcode on-device.
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

	for _, ptr := range frames {
		if ptr == nil {
			continue
		}

		v := (*[128]uint64)(ptr)
		opcode := v[16] & 0xF
		batchCount := v[124]

		if opcode == 0x6 && batchCount > 0 {
			distances := (*[256]uint32)(unsafe.Pointer(&v[24]))

			if C.nearest_affinity_metal(
				unsafe.Pointer(&v[0]),
				unsafe.Pointer(&v[32]),
				C.uint32_t(batchCount),
				(*C.uint32_t)(unsafe.Pointer(&distances[0])),
			) != 0 {
				err := NewMetalKernelError(
					kernel.KernelErrDispatchFailed, nil, "Execute",
				)

				kv := kernel.CorrelationKeyvals(ptr)
				backend.observer.Error("metal.Backend.Execute", err, kv...)

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

			v[22] = bestIdx
			v[23] = bestDist

			continue
		}

		if C.unified_bitwise_metal(ptr, 1) != 0 {
			err := NewMetalKernelError(
				kernel.KernelErrDispatchFailed, nil, "Execute",
			)

			kv := kernel.CorrelationKeyvals(ptr)
			backend.observer.Error("metal.Backend.Execute", err, kv...)

			return err
		}
	}

	return nil
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

/*
batchDistances is the internal fused XOR+popcount path, called by
Execute when it detects a batch distance opcode. Kept as a method
for NearestAffinity backward compatibility.
*/
func (backend *Backend) batchDistances(
	query unsafe.Pointer,
	candidates unsafe.Pointer,
	count int,
	distances []uint32,
) error {
	if !metalReady.Load() {
		return NewMetalKernelError(
			kernel.KernelErrUnavailable,
			errors.New("metal backend not initialized"),
			"batchDistances",
		)
	}

	if C.nearest_affinity_metal(
		query,
		candidates,
		C.uint32_t(count),
		(*C.uint32_t)(unsafe.Pointer(&distances[0])),
	) != 0 {
		return NewMetalKernelError(
			kernel.KernelErrDispatchFailed, nil, "batchDistances",
		)
	}

	return nil
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
