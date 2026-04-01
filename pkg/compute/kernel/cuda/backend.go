//go:build cuda && cgo

package cuda

/*
#cgo LDFLAGS: -L${SRCDIR} -lbackend -lcudart
#include <stdint.h>
int cuda_device_count();
void cleanup_cuda_pools();

int unified_bitwise_cuda(int device_id, void* a_host, const void* b_host, uint32_t num_values);
*/
import "C"
import (
	"context"
	"errors"
	"sync"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
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
	observer    kernel.Observer
}

type backendOption func(*Backend)

/*
NewBackend returns a CUDA kernel Backend.
*/
func NewBackend(idx int, opts ...backendOption) *Backend {
	ctx, cancel := context.WithCancel(context.Background())
	backend := &Backend{
		deviceIdx: idx,
		ctx:       ctx,
		cancel:    cancel,
		observer:  kernel.NoopObserver{},
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
	core.PreloadPendingFirmware(c[:])
}

func preloadFirmwareFrameBatch(a unsafe.Pointer, count int) {
	for i := 0; i < count; i++ {
		c := (*[128]uint64)(unsafe.Pointer(uintptr(a) + uintptr(i)*1024))
		preloadFirmwareFrame(c)
	}
}

/*
UniversalBitwise dispatches a batch of Values to the compiled CUDA kernel.

The opcode is no longer passed explicitly — each Value carries its own
64-op program in Region 3 (bits 4352–4607). The unified_bitwise_kernel
reads that program and executes up to 64 ticks per Value, halting at the
first zero opcode. The batch may therefore be heterogeneous: each Value
runs its own independent program in parallel.
*/
func (backend *Backend) UniversalBitwise(frames []unsafe.Pointer) error {

	if len(frames) == 0 {
		return nil
	}

	if len(frames) == 1 && frames[0] != nil {
		preloadFirmwareFrameBatch(frames[0], 1)

		if C.unified_bitwise_cuda(
			C.int(backend.deviceIdx),
			frames[0],
			frames[0],
			C.uint32_t(1),
		) != 0 {
			err := NewCUDAError(
				CUDAErrorDispatchFailed,
				errors.New("failed to dispatch unified bitwise operation"),
				"UniversalBitwise",
				1,
			)
			backend.observer.Error(
				"cuda.Backend.UniversalBitwise",
				err,
				"device_idx", backend.deviceIdx,
			)
			return err
		}

		return nil
	}

	slabA := kernel.PackValueFrames(frames)
	slabB := append([]byte(nil), slabA...)

	preloadFirmwareFrameBatch(unsafe.Pointer(&slabA[0]), len(frames))

	if C.unified_bitwise_cuda(
		C.int(backend.deviceIdx),
		unsafe.Pointer(&slabA[0]),
		unsafe.Pointer(&slabB[0]),
		C.uint32_t(len(frames)),
	) != 0 {
		err := NewCUDAError(
			CUDAErrorDispatchFailed,
			errors.New("failed to dispatch unified bitwise operation"),
			"UniversalBitwise",
			1,
		)
		backend.observer.Error(
			"cuda.Backend.UniversalBitwise",
			err,
			"device_idx", backend.deviceIdx,
		)
		return err
	}

	kernel.UnpackValueFrames(frames, slabA)

	return nil
}

// Schedule runs the job with Context(); cancellation is tied to Shutdown.
func (backend *Backend) Schedule(job func(ctx context.Context) error) error {
	return job(backend.ctx)
}
