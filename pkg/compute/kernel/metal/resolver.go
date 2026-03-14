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
	"os"
	"sync/atomic"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel/internal/resolve"
	"github.com/theapemachine/six/pkg/numeric/geometry"
)

//go:generate xcrun -sdk macosx metal -std=metal3.1 -mmacosx-version-min=14.0 -c resolver.metal -o resolver.air
//go:generate xcrun -sdk macosx metallib resolver.air -o resolver.metallib

//go:embed resolver.metallib
var resolverMetallib []byte

var metalReady atomic.Bool

type MetalBackend struct{}

func (backend *MetalBackend) Available() bool {
	return metalReady.Load()
}

func (backend *MetalBackend) Resolve(
	graphNodes unsafe.Pointer,
	numNodes int,
	context unsafe.Pointer,
) (uint64, error) {
	if !backend.Available() {
		return 0, MetalErrorUnavailable
	}

	if numNodes == 0 {
		return 0, nil
	}

	if numNodes < 1024 {
		nodes := unsafe.Slice((*geometry.GFRotation)(graphNodes), numNodes)
		ctx := (*geometry.GFRotation)(context)
		return resolve.PackedNearest(nodes, *ctx), nil
	}

	if numNodes < 0 || numNodes > 4294967295 {
		return 0, MetalErrorResolveFailed
	}

	var packed C.uint64_t

	status := C.resolve_resonance_metal(
		graphNodes,
		C.uint32_t(numNodes),
		context,
		&packed,
	)

	if status != 0 {
		return 0, MetalErrorResolveFailed
	}

	return uint64(packed), nil
}

type MetalError string

const (
	MetalErrorUnavailable   MetalError = "metal backend unavailable"
	MetalErrorInitFailed    MetalError = "metal backend init failed"
	MetalErrorResolveFailed MetalError = "metal backend resolve failed"
)

func (err MetalError) Error() string {
	return string(err)
}

var initErr error

func InitError() error {
	return initErr
}

func init() {
	tmpFile, err := os.CreateTemp("", "sensorium-shader-*.metallib")
	if err != nil {
		initErr = err
		return
	}

	name := tmpFile.Name()
	defer func() {
		_ = os.Remove(name)
	}()

	if _, err := tmpFile.Write(resolverMetallib); err != nil {
		tmpFile.Close()
		initErr = err
		return
	}

	if err := tmpFile.Close(); err != nil {
		initErr = err
		return
	}

	cPath := C.CString(name)
	defer C.free(unsafe.Pointer(cPath))

	res := C.init_metal(cPath)
	if res != 0 {
		initErr = MetalErrorInitFailed
		return
	}

	metalReady.Store(true)
	initErr = nil
}
