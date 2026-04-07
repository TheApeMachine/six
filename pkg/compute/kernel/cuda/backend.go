//go:build cuda && cgo

package cuda

/*
#cgo LDFLAGS: -L${SRCDIR} -lbackend -lcudart
#include <stdint.h>
int cuda_device_count();
void cleanup_cuda_pools();

int unified_bitwise_cuda(int device_id, void* a_host, uint32_t num_values);
int nearest_affinity_cuda(int device_id, void* query_host, void* candidates_host, uint32_t count, uint32_t* distances_host);
*/
import "C"
import (
	"context"
	"errors"
	"sync"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
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

/*
UniversalBitwise dispatches a batch of Values to the compiled CUDA kernel.

Each Value carries its own 32-bit in-band program. The CUDA kernel executes the
configured program slot sweep against the frame itself only, matching the
self-only CPU backend contract.
*/
func (backend *Backend) UniversalBitwise(frames []unsafe.Pointer) error {

	if len(frames) == 0 {
		return nil
	}

	if len(frames) == 1 && frames[0] != nil {
		if C.unified_bitwise_cuda(
			C.int(backend.deviceIdx),
			frames[0],
			C.uint32_t(1),
		) != 0 {
			err := NewCUDAKernelError(
				kernel.KernelErrDispatchFailed,
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

	if C.unified_bitwise_cuda(
		C.int(backend.deviceIdx),
		unsafe.Pointer(&slabA[0]),
		C.uint32_t(len(frames)),
	) != 0 {
		err := NewCUDAKernelError(
			kernel.KernelErrDispatchFailed,
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

/*
BatchDistances computes Hamming distances from query to count candidate
affinity vectors on the CUDA GPU. One thread per candidate with __popcll.
*/
func (backend *Backend) BatchDistances(
	query unsafe.Pointer,
	candidates unsafe.Pointer,
	count int,
	distances []uint32,
) error {
	if C.nearest_affinity_cuda(
		C.int(backend.deviceIdx),
		query,
		candidates,
		C.uint32_t(count),
		(*C.uint32_t)(unsafe.Pointer(&distances[0])),
	) != 0 {
		return NewCUDAKernelError(
			kernel.KernelErrDispatchFailed,
			errors.New("batch distances dispatch failed"),
			"BatchDistances",
			count,
		)
	}

	return nil
}

/*
NearestAffinity computes Hamming distances from query to all candidates
on the GPU and returns per-candidate distances. The caller reduces argmin.
*/
func (backend *Backend) NearestAffinity(
	query unsafe.Pointer, candidates unsafe.Pointer, count int,
) ([]uint32, error) {
	distances := make([]uint32, count)

	if C.nearest_affinity_cuda(
		C.int(backend.deviceIdx),
		query,
		candidates,
		C.uint32_t(count),
		(*C.uint32_t)(unsafe.Pointer(&distances[0])),
	) != 0 {
		return nil, NewCUDAKernelError(
			kernel.KernelErrDispatchFailed,
			errors.New("nearest_affinity dispatch failed"),
			"NearestAffinity",
			count,
		)
	}

	return distances, nil
}

// Schedule runs the job with Context(); cancellation is tied to Shutdown.
func (backend *Backend) Schedule(job func(ctx context.Context) error) error {
	return job(backend.ctx)
}

func (backend *Backend) Name() string { return "cuda" }
