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
Backend runs the in-band Value kernels on Apple Silicon (shared memory).
It satisfies the full kernel.Substrate contract — the per-Value
optimizer path runs on CPU (single-Value GPU dispatch is wasteful), but
ExecuteCommunity / AssignFirstFit dispatch to the GPU pipelines built
on init.

Pressure tracks inflight dispatches plus an EMA over per-job service
time (ns) so compute.Backend can pick the lowest-loaded substrate.
*/
type Backend struct {
	idx      int
	inflight atomic.Int64
	emaNs    atomic.Uint64
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
Pressure reports inflight dispatches plus an EMA of per-job service
time in nanoseconds.
*/
func (backend *Backend) ExecuteCommunity(community []*primitive.Value) []*primitive.Value {
	return nil
}

func (backend *Backend) GeometricFrame(value unsafe.Pointer, opcode uint64) bool {
	return false
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
