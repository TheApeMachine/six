//go:build cuda && cgo

package cuda

/*
#cgo LDFLAGS: -L${SRCDIR} -lbackend -lcudart
#include <stdint.h>
int cuda_device_count();
void cleanup_cuda_pools();

int unified_bitwise_cuda(int device_id, void* a_host, const void* b_host);
*/
import "C"
import (
	"context"
	"errors"
	"sync"
	"unsafe"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
)

//go:generate nvcc -lib backend.cu -o libbackend.a -std=c++11

/*
Backend dispatches Value-native GPU kernels on NVIDIA CUDA devices.
*/
type Backend struct {
	initOnce    sync.Once
	deviceCount int
	deviceIdx   int
}

/*
NewBackend returns a CUDA kernel Backend.
*/
func NewBackend(idx int) *Backend {
	return &Backend{
		deviceIdx: idx,
	}
}

func (backend *Backend) init() {
	backend.initOnce.Do(func() {
		backend.deviceCount = int(C.cuda_device_count())

		if backend.deviceCount < 0 {
			backend.deviceCount = 0
		}
	})
}

/*
Available returns the number of CUDA-capable GPUs.
*/
func Available() int {
	b := NewBackend(0)
	b.init()
	return b.deviceCount
}

func preloadFirmwareFrame(c *[128]uint64) {
	if c == nil {
		return
	}

	p := uint64(core.Cfg.RegPC)
	w := uint64(core.Cfg.ProgramIndex)
	f := c[uint64(core.Cfg.FW)]

	if f == 0 || int(f) >= len(core.Cfg.Firmware) || c[p] != 0 {
		return
	}

	g := core.Cfg.Firmware[f]
	for i, j := 0, w+4; i < len(g) && int(j) < len(c); i, j = i+2, j+1 {
		v := uint64(g[i])
		if i+1 < len(g) {
			v |= uint64(g[i+1]) << 32
		}
		c[j] = v
	}

	c[uint64(core.Cfg.FW)] = 0
}

/*
UniversalBitwise dispatches a batch of Values to the compiled CUDA kernel.

The opcode is no longer passed explicitly — each Value carries its own
64-op program in Region 3 (bits 4352–4607). The unified_bitwise_kernel
reads that program and executes up to 64 ticks per Value, halting at the
first zero opcode. The batch may therefore be heterogeneous: each Value
runs its own independent program in parallel.
*/
func (backend *Backend) UniversalBitwise(a, bPtr unsafe.Pointer) error {
	preloadFirmwareFrame((*[128]uint64)(a))

	if C.unified_bitwise_cuda(C.int(backend.deviceIdx), unsafe.Pointer(a), unsafe.Pointer(bPtr)) != 0 {
		return NewCUDAError(
			CUDAErrorDispatchFailed,
			errors.New("failed to dispatch unified bitwise operation"),
			"UniversalBitwise",
			1,
		)
	}

	return nil
}

func (backend *Backend) Schedule(job func(ctx context.Context) error) error {
	if err := job(context.Background()); err != nil {
		_ = errnie.Error(err)
		return err
	}
	return nil
}
