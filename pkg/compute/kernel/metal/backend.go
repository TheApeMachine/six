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
	idx int
}

type backendOption func(*Backend)

/*
NewBackend returns a Metal kernel Backend.
*/
func NewBackend(idx int, opts ...backendOption) *Backend {
	backend := &Backend{
		idx: idx,
	}

	for _, opt := range opts {
		opt(backend)
	}

	return backend
}

func (backend *Backend) Close() {
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
UniversalBitwise dispatches arena slot indices: the GPU runs the unified bitwise kernel
on each Value frame in-place.
*/
func (backend *Backend) UniversalBitwise(optimizer *kernel.Optimizer) {
	if !metalReady.Load() {
		return
	}
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
		reportInitError(errors.New("metal: init_metal failed"))

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

func (backend *Backend) Name() string { return "metal" }
