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
	"github.com/theapemachine/six/pkg/primitive"
)

//go:generate xcrun -sdk macosx metal -std=metal3.1 -mmacosx-version-min=14.0 -I. -c backend.metal -o backend.air
//go:generate xcrun -sdk macosx metallib backend.air -o backend.metallib

//go:embed backend.metallib
var backendMetallib []byte

var metalReady atomic.Bool

var metalArenaInit sync.Once

const spawnQueueCap = 4096

var metalSpawnDrainBufPool = sync.Pool{
	New: func() any {
		pair := make([]uint32, spawnQueueCap*2)

		return &pair
	},
}

func drainMetalSpawns() {
	packedAny := metalSpawnDrainBufPool.Get()
	packed := packedAny.(*[]uint32)
	slab := *packed
	if len(slab) < spawnQueueCap*2 {
		slab = make([]uint32, spawnQueueCap*2)
	}

	parents := slab[:spawnQueueCap]
	children := slab[spawnQueueCap : spawnQueueCap*2]

	var outCount C.uint32_t

	if C.metal_drain_spawn_queue(
		(*C.uint32_t)(unsafe.Pointer(&parents[0])),
		(*C.uint32_t)(unsafe.Pointer(&children[0])),
		C.uint32_t(spawnQueueCap),
		&outCount,
		nil,
	) != 0 {
		*packed = slab
		metalSpawnDrainBufPool.Put(packed)

		return
	}

	n := int(outCount)
	if n <= 0 {
		*packed = slab
		metalSpawnDrainBufPool.Put(packed)

		return
	}

	for idx := 0; idx < n; idx++ {
		child := primitive.ValueAt(children[idx])
		if child == nil {
			continue
		}

		child.StampID()
	}

	*packed = slab
	metalSpawnDrainBufPool.Put(packed)
}

func ensureMetalArena() error {
	var initErr error

	metalArenaInit.Do(func() {
		primitive.EnsureArenaPinnedForGPU()

		base, byteLen := primitive.ArenaBasePointer()
		if base == nil || byteLen == 0 {
			initErr = errors.New("metal: empty value arena")

			return
		}

		if C.init_metal_arena(
			base,
			C.size_t(byteLen),
			(*C.uint32_t)(unsafe.Pointer(primitive.ArenaLinearNextPtr())),
		) != 0 {
			initErr = errors.New("metal: init_metal_arena failed")
		}
	})

	return initErr
}

/*
Backend runs the unified bitwise Value kernel on Apple Silicon (shared memory).
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

func (backend *Backend) Shutdown() {
	cleanupMetalPools()
}

/*
Available returns the number of Metal-capable GPUs present on this system,
or an error if the Metal runtime failed to initialize.
*/
func Available() int {
	return int(C.count_metal_devices())
}

/*
Execute dispatches arena slot indices: the GPU runs the unified bitwise kernel
on each Value frame in-place.
*/
func (backend *Backend) Execute(indices []uint32) error {
	if !metalReady.Load() {
		return NewMetalKernelError(
			kernel.KernelErrUnavailable,
			errors.New("metal backend not initialized"),
			"Execute",
		)
	}

	if len(indices) == 0 {
		return nil
	}

	if err := ensureMetalArena(); err != nil {
		return NewMetalKernelError(kernel.KernelErrInitFailed, err, "Execute")
	}

	if C.unified_bitwise_metal_indices(
		(*C.uint32_t)(unsafe.Pointer(&indices[0])),
		C.uint32_t(len(indices)),
		C.uint32_t(primitive.ArenaSlotCount),
	) != 0 {
		err := NewMetalKernelError(
			kernel.KernelErrDispatchFailed, nil, "Execute",
		)
		backend.observer.Error("metal.Backend.Execute", err)

		return err
	}

	drainMetalSpawns()

	return nil
}

/*
ExecutePointers resolves host pointers to arena indices when the storage
backs the contiguous slab; stack-allocated test frames must use AllocValue
and Execute([]uint32) instead.
*/
func (backend *Backend) ExecutePointers(frames []unsafe.Pointer) error {
	indices, err := primitive.IndicesFromPointers(frames)
	if err != nil {
		return err
	}

	return backend.Execute(indices)
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
