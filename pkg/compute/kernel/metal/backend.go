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
	"os"
	"sync/atomic"
	"unsafe"

	"github.com/theapemachine/six/pkg/errnie"
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
	idx int
}

/*
NewBackend returns a Metal kernel Backend.
*/
func NewBackend(idx int) *Backend {
	return &Backend{
		idx: idx,
	}
}

/*
Available returns the number of Metal-capable GPUs present on this system,
or an error if the Metal runtime failed to initialize.
*/
func Available() int {
	return int(C.count_metal_devices())
}

/*
UniversalBitwise dispatches a batch of Values to the compiled Metal kernel.

The opcode is no longer passed externally — each Value carries its own
64-op program in Region 3 (words 68–71). The unified_bitwise_kernel reads
that program and executes up to 64 ticks per Value, halting at opcode 0.
The batch may therefore be heterogeneous: each Value runs its own independent
program in parallel on the GPU.
*/
func (backend *Backend) UniversalBitwise(a, b unsafe.Pointer) error {
	if !metalReady.Load() {
		return NewMetalError(
			MetalErrorUnavailable,
			errors.New("failed to load metal backend"),
			"UniversalBitwise",
		)
	}

	if C.unified_bitwise_metal(a, b) != 0 {
		return NewMetalError(MetalErrorDispatchFailed, nil, "UniversalBitwise")
	}
	return nil
}

func init() {
	tmpFile, err := os.CreateTemp("", "backend-*.metallib")

	if err != nil {
		errnie.Error(err)
		return
	}

	name := tmpFile.Name()

	defer func() {
		_ = os.Remove(name)
	}()

	if _, err := tmpFile.Write(backendMetallib); err != nil {
		tmpFile.Close()
		errnie.Error(err)
		return
	}

	if err := tmpFile.Close(); err != nil {
		errnie.Error(err)
		return
	}

	cPath := C.CString(name)
	defer C.free(unsafe.Pointer(cPath))

	if res := C.init_metal(cPath); res != 0 {
		errnie.Error(NewMetalError(MetalErrorInitFailed, nil, "init_metal"))
		return
	}

	metalReady.Store(true)
}

/*
MetalErrorType is a typed error for Metal backend failures.
*/
type MetalErrorType string

const (
	MetalErrorUnavailable    MetalErrorType = "metal backend unavailable"
	MetalErrorInitFailed     MetalErrorType = "metal backend init failed"
	MetalErrorDispatchFailed MetalErrorType = "metal backend dispatch failed"
)

type MetalError struct {
	Err error
	Msg string
	Op  string
}

func NewMetalError(merr MetalErrorType, err error, op string) *MetalError {
	return &MetalError{
		Err: err,
		Msg: string(merr),
		Op:  op,
	}
}

/*
Error implements the error interface for MetalErrorType.
*/
func (err MetalError) Error() string {
	return err.Msg
}

func (backend *Backend) Schedule(job func(ctx context.Context) error) {
	_ = job(context.Background())
}
