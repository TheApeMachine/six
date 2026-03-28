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
)

//go:generate nvcc -lib backend.cu -o libbackend.a -std=c++11

/*
Backend dispatches Value-native GPU kernels on NVIDIA CUDA devices.
*/
type Backend struct {
	initOnce    sync.Once
	deviceCount int
	deviceIdx   int
	ctx         context.Context
	cancel      context.CancelFunc
}

/*
NewBackend returns a CUDA kernel Backend.
*/
func NewBackend(idx int) *Backend {
	ctx, cancel := context.WithCancel(context.Background())
	return &Backend{
		deviceIdx: idx,
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Context returns the backend-scoped context canceled by Shutdown.
func (backend *Backend) Context() context.Context {
	return backend.ctx
}

// Shutdown cancels the backend context so work passed to Schedule observes
// ctx.Done. Global CUDA pool memory in the C layer is shared across device
// indices; this does not free device buffers.
func (backend *Backend) Shutdown() {
	if backend.cancel != nil {
		backend.cancel()
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

	const maxIdx = 127
	const nWords = 128

	pc := core.Cfg.RegPC
	w := core.Cfg.ProgramIndex
	fwIdx := core.Cfg.FW

	if pc < 0 || pc > maxIdx || w < 0 || w > maxIdx || fwIdx < 0 || fwIdx > maxIdx {
		return
	}

	p := pc
	f := c[fwIdx]

	if f == 0 {
		return
	}

	fi := int(f)
	if fi < 0 || fi >= len(core.Cfg.Firmware) {
		return
	}

	if c[p] != 0 {
		return
	}

	g := core.Cfg.Firmware[fi]
	if len(g) > 0 {
		if w+4 > maxIdx {
			return
		}
		numWrites := (len(g) + 1) / 2
		maxJ := w + 4 + numWrites - 1
		if maxJ > maxIdx {
			return
		}
	}

	for i, j := 0, w+4; i < len(g) && j < nWords; i, j = i+2, j+1 {
		v := uint64(g[i])
		if i+1 < len(g) {
			v |= uint64(g[i+1]) << 32
		}
		c[j] = v
	}

	c[fwIdx] = 0
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

// Schedule runs the job with Context(); cancellation is tied to Shutdown.
func (backend *Backend) Schedule(job func(ctx context.Context) error) error {
	return job(backend.ctx)
}
