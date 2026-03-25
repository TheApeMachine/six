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
	"os"
	"sync/atomic"
	"unsafe"

	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
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
}

/*
NewBackend returns a Metal kernel Backend.
*/
func NewBackend() *Backend {
	return &Backend{}
}

/*
Available returns the number of Metal-capable GPUs present on this system,
or an error if the Metal runtime failed to initialize.
*/
func Available() int {
	return int(C.count_metal_devices())
}

/*
Read implements io.Reader.
*/
func (backend *Backend) Read(p []byte) (n int, err error) {
	return
}

/*
Write implements io.Writer.
*/
func (backend *Backend) Write(p []byte) (n int, err error) {
	return
}

/*
Close implements io.Closer.
*/
func (backend *Backend) Close() error {
	return nil
}

/*
UniversalBitwise reads the 4-bit opcode from each Value's instruction
region and dispatches to the compiled Metal kernel.
*/
func (backend *Backend) UniversalBitwise(a, b, dst unsafe.Pointer, n uint32) error {
	if !metalReady.Load() {
		return NewMetalError(
			MetalErrorUnavailable,
			errors.New("failed to load metal backend"),
			"UniversalBitwise", n,
		)
	}

	// Read the opcode from the first Value's instruction region.
	// All Values in a batch share the same opcode (set by the caller
	// via bitwiseViaALU or program execution).
	as := unsafe.Slice((*primitive.Value)(a), n)
	word := primitive.InstrStart >> 6
	shift := uint(primitive.InstrStart & 63)
	mask := uint64((1 << primitive.InstrBits) - 1)
	op := uint8((as[0][word] >> shift) & mask)

	if C.universal_bitwise_metal(a, b, dst, C.uint8_t(op), C.uint32_t(n)) != 0 {
		return NewMetalError(MetalErrorDispatchFailed, nil, "UniversalBitwise", n)
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
		errnie.Error(NewMetalError(MetalErrorInitFailed, nil, "init_metal", 0))
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
	N   uint32
}

func NewMetalError(merr MetalErrorType, err error, op string, n uint32) *MetalError {
	return &MetalError{
		Err: err,
		Msg: err.Error(),
		Op:  op,
		N:   n,
	}
}

/*
Error implements the error interface for MetalErrorType.
*/
func (err MetalError) Error() string {
	return err.Msg
}
