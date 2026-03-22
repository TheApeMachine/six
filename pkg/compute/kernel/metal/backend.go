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

	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/transport"
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
	*transport.Stream
}

/*
NewBackend returns a Metal kernel Backend.
*/
func NewBackend() *Backend {
	return &Backend{
		Stream: transport.NewStream(),
	}
}

/*
Available returns the number of Metal-capable GPUs present on this system,
or an error if the Metal runtime failed to initialize.
*/
func (backend *Backend) Available() (int, error) {
	if !metalReady.Load() {
		return 0, MetalErrorUnavailable
	}

	return int(C.count_metal_devices()), nil
}

/*
BitwiseOr dispatches bitwise OR (LCM) across N Value pairs.
*/
func (backend *Backend) BitwiseOr(a, b, dst unsafe.Pointer, numValues uint32) error {
	if !metalReady.Load() {
		return MetalErrorUnavailable
	}

	if C.bitwise_or_metal(a, b, dst, C.uint32_t(numValues)) != 0 {
		return MetalErrorDispatchFailed
	}

	return nil
}

/*
BitwiseAnd dispatches bitwise AND (GCD) across N Value pairs.
*/
func (backend *Backend) BitwiseAnd(a, b, dst unsafe.Pointer, numValues uint32) error {
	if !metalReady.Load() {
		return MetalErrorUnavailable
	}

	if C.bitwise_and_metal(a, b, dst, C.uint32_t(numValues)) != 0 {
		return MetalErrorDispatchFailed
	}

	return nil
}

/*
BitwiseXor dispatches bitwise XOR (symmetric difference) across N Value pairs.
*/
func (backend *Backend) BitwiseXor(a, b, dst unsafe.Pointer, numValues uint32) error {
	if !metalReady.Load() {
		return MetalErrorUnavailable
	}

	if C.bitwise_xor_metal(a, b, dst, C.uint32_t(numValues)) != 0 {
		return MetalErrorDispatchFailed
	}

	return nil
}

/*
BitwiseAndNot dispatches material nonimplication (A & ~B) across N Value pairs.
*/
func (backend *Backend) BitwiseAndNot(a, b, dst unsafe.Pointer, numValues uint32) error {
	if !metalReady.Load() {
		return MetalErrorUnavailable
	}

	if C.bitwise_and_not_metal(a, b, dst, C.uint32_t(numValues)) != 0 {
		return MetalErrorDispatchFailed
	}

	return nil
}

/*
BitwiseNand dispatches NAND across N Value pairs.
*/
func (backend *Backend) BitwiseNand(a, b, dst unsafe.Pointer, numValues uint32) error {
	if !metalReady.Load() {
		return MetalErrorUnavailable
	}

	if C.bitwise_nand_metal(a, b, dst, C.uint32_t(numValues)) != 0 {
		return MetalErrorDispatchFailed
	}

	return nil
}

/*
BitwiseNor dispatches NOR across N Value pairs.
*/
func (backend *Backend) BitwiseNor(a, b, dst unsafe.Pointer, numValues uint32) error {
	if !metalReady.Load() {
		return MetalErrorUnavailable
	}

	if C.bitwise_nor_metal(a, b, dst, C.uint32_t(numValues)) != 0 {
		return MetalErrorDispatchFailed
	}

	return nil
}

/*
BitwiseXnor dispatches XNOR across N Value pairs.
*/
func (backend *Backend) BitwiseXnor(a, b, dst unsafe.Pointer, numValues uint32) error {
	if !metalReady.Load() {
		return MetalErrorUnavailable
	}

	if C.bitwise_xnor_metal(a, b, dst, C.uint32_t(numValues)) != 0 {
		return MetalErrorDispatchFailed
	}

	return nil
}

/*
BitwiseConverseNonimplication dispatches converse nonimplication (B & ~A) across N Value pairs.
*/
func (backend *Backend) BitwiseConverseNonimplication(a, b, dst unsafe.Pointer, numValues uint32) error {
	if !metalReady.Load() {
		return MetalErrorUnavailable
	}

	if C.bitwise_converse_nonimplication_metal(a, b, dst, C.uint32_t(numValues)) != 0 {
		return MetalErrorDispatchFailed
	}

	return nil
}

/*
BitwiseNot dispatches unary NOT across N Values.
*/
func (backend *Backend) BitwiseNot(a, dst unsafe.Pointer, numValues uint32) error {
	if !metalReady.Load() {
		return MetalErrorUnavailable
	}

	if C.bitwise_not_metal(a, dst, C.uint32_t(numValues)) != 0 {
		return MetalErrorDispatchFailed
	}

	return nil
}

/*
MotorApply derives motor(A) and applies it to B for N Value pairs.
*/
func (backend *Backend) MotorApply(a, b, dst unsafe.Pointer, numValues uint32) error {
	if !metalReady.Load() {
		return MetalErrorUnavailable
	}

	if C.motor_apply_metal(a, b, dst, C.uint32_t(numValues)) != 0 {
		return MetalErrorDispatchFailed
	}

	return nil
}

/*
MotorInvert derives inverse motor(A) and applies it to B for N Value pairs.
*/
func (backend *Backend) MotorInvert(a, b, dst unsafe.Pointer, numValues uint32) error {
	if !metalReady.Load() {
		return MetalErrorUnavailable
	}

	if C.motor_invert_metal(a, b, dst, C.uint32_t(numValues)) != 0 {
		return MetalErrorDispatchFailed
	}

	return nil
}

/*
MotorCompose composes motor(A) then motor(B) and applies to B for N Value pairs.
*/
func (backend *Backend) MotorCompose(a, b, dst unsafe.Pointer, numValues uint32) error {
	if !metalReady.Load() {
		return MetalErrorUnavailable
	}

	if C.motor_compose_metal(a, b, dst, C.uint32_t(numValues)) != 0 {
		return MetalErrorDispatchFailed
	}

	return nil
}

/*
RollLeft circular-shifts all core bits left by shift positions for N Values.
*/
func (backend *Backend) RollLeft(src, dst unsafe.Pointer, shift uint32, numValues uint32) error {
	if !metalReady.Load() {
		return MetalErrorUnavailable
	}

	if C.roll_left_metal(src, dst, C.uint32_t(shift), C.uint32_t(numValues)) != 0 {
		return MetalErrorDispatchFailed
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
		errnie.Error(MetalErrorInitFailed)
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

/*
Error implements the error interface for MetalErrorType.
*/
func (err MetalErrorType) Error() string {
	return string(err)
}
