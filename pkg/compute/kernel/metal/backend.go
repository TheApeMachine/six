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
	"github.com/theapemachine/six/pkg/core"
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

func resolveFirmwareRegister(sel uint64) (core.FirmwareType, bool) {
	switch sel {
	case core.FirmwareRegisterLearn:
		return core.FirmwareTypeLearn, true
	case core.FirmwareRegisterTombstone:
		return core.FirmwareTypeTombstone, true
	case core.FirmwareRegisterViral:
		return core.FirmwareTypeViral, true
	case core.FirmwareRegisterBuild:
		return core.FirmwareTypeBuild, true
	default:
		return 0, false
	}
}

func payloadProgramPCStart() uint64 {
	return uint64(core.PayloadProgramPCOffset)
}

func clearPayloadProgram(c *[128]uint64) {
	if c == nil {
		return
	}
	for slot := int(payloadProgramPCStart()); slot < core.Cfg.MaxPC; slot++ {
		wordIdx := core.Cfg.ProgramIndex + slot/2
		if wordIdx < 0 || wordIdx >= len(c) {
			break
		}
		shift := uint((slot % 2) * 32)
		mask := uint64(0xFFFFFFFF) << shift
		c[wordIdx] &^= mask
	}
}

func installProgramAtSlot(c *[128]uint64, startSlot int, program []uint32) {
	if c == nil || startSlot < 0 || startSlot >= core.Cfg.MaxPC {
		return
	}
	for i, instr := range program {
		slot := startSlot + i
		if slot >= core.Cfg.MaxPC {
			break
		}
		wordIdx := core.Cfg.ProgramIndex + slot/2
		if wordIdx < 0 || wordIdx >= len(c) {
			break
		}
		shift := uint((slot % 2) * 32)
		mask := uint64(0xFFFFFFFF) << shift
		c[wordIdx] = (c[wordIdx] &^ mask) | (uint64(instr) << shift)
	}
}

func frameReadyForFirmwareLoad(c *[128]uint64) bool {
	if c == nil {
		return false
	}
	pc := c[core.Cfg.RegPC]
	if pc == 0 || pc >= uint64(core.Cfg.MaxPC) {
		return true
	}
	wordPos := core.Cfg.ProgramIndex + int(pc/2)
	if wordPos < 0 || wordPos >= len(c) {
		return true
	}
	shift := uint((pc % 2) * 32)
	return uint32(c[wordPos]>>shift) == 0
}

func preloadFirmwareFrame(c *[128]uint64) {
	if c == nil {
		return
	}
	ft, ok := resolveFirmwareRegister(c[core.Cfg.FW])
	if !ok || !frameReadyForFirmwareLoad(c) {
		return
	}
	prog := core.Cfg.Firmware[ft]
	if len(prog) == 0 {
		c[core.Cfg.FW] = core.FirmwareRegisterNone
		return
	}
	clearPayloadProgram(c)
	installProgramAtSlot(c, int(payloadProgramPCStart()), prog)
	c[core.Cfg.FW] = core.FirmwareRegisterNone
	c[core.Cfg.RegPC] = payloadProgramPCStart()
}

func preloadFirmwareFrameBatch(a unsafe.Pointer, count int) {
	for i := 0; i < count; i++ {
		c := (*[128]uint64)(unsafe.Pointer(uintptr(a) + uintptr(i)*1024))
		preloadFirmwareFrame(c)
	}
}

/*
UniversalBitwise dispatches a batch of Values to the compiled Metal kernel.

The opcode is no longer passed externally — each Value carries its own
64-op program in Region 3 (words 68–71). The unified_bitwise_kernel reads
that program and executes up to 64 ticks per Value, halting at opcode 0.
The batch may therefore be heterogeneous: each Value runs its own independent
program in parallel on the GPU.
*/
func (backend *Backend) UniversalBitwise(a, b unsafe.Pointer, count int) error {
	backend.observer.Trace("metal.Backend.UniversalBitwise", "a", a, "b", b)
	preloadFirmwareFrameBatch(a, count)

	if !metalReady.Load() {
		return NewMetalError(
			MetalErrorUnavailable,
			errors.New("failed to load metal backend"),
			"UniversalBitwise",
		)
	}

	if C.unified_bitwise_metal(a, b, C.uint32_t(count)) != 0 {
		return NewMetalError(MetalErrorDispatchFailed, nil, "UniversalBitwise")
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
		reportInitError(NewMetalError(MetalErrorInitFailed, nil, "init_metal"))
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
