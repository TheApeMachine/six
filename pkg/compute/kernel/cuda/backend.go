//go:build cuda && cgo

package cuda

/*
#cgo LDFLAGS: -L${SRCDIR} -lbackend -lcudart
#include <stdint.h>
int cuda_device_count();
void cleanup_cuda_pools();

int unified_bitwise_cuda(int device_id, void* a_host, uint32_t num_values);
int geometric_cuda(int device_id, void* a_host, uint32_t num_values);
int nearest_affinity_cuda(int device_id, void* query_host, void* candidates_host, uint32_t count, uint32_t* distances_host);
*/
import "C"
import (
	"context"
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/primitive"
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
Execute dispatches frames to the appropriate CUDA kernel based on
the opcode in each Value's program region (word 16). Batch distance
frames (opcode 0x6 with count at word 124) route to the fused
XOR+popcount kernel. Geometric opcodes route to the PGA kernel.
All other frames go through the unified bitwise kernel.
*/
func (backend *Backend) Execute(indices []uint32) error {
	if len(indices) == 0 {
		return nil
	}

	for _, slot := range indices {
		value := primitive.ValueAt(slot)
		if value == nil {
			return fmt.Errorf("cuda.Backend.Execute: primitive.ValueAt(%d) returned nil", slot)
		}

		ptr := unsafe.Pointer(&value[0])
		v := (*[128]uint64)(ptr)
		rawOpcode := v[kernel.ProgramStartWord] & 0xFF
		opcode := rawOpcode & kernel.OpcodeBooleanMask
		batchCount := v[kernel.NearestAffinityBatchWord]

		if batchCount > uint64(kernel.MaxNearestAffinityCandidates) {
			batchCount = uint64(kernel.MaxNearestAffinityCandidates)
		}

		primitive.HydrateLearnerPeers(value)

		if kernel.IsGeometricOpcode(rawOpcode) {
			if C.geometric_cuda(
				C.int(backend.deviceIdx),
				ptr,
				C.uint32_t(1),
			) != 0 {
				err := NewCUDAKernelError(
					kernel.KernelErrDispatchFailed,
					errors.New("geometric dispatch failed"),
					"Execute",
					1,
				)

				kv := append(
					[]any{"device_idx", backend.deviceIdx},
					kernel.CorrelationKeyvalsFlat(ptr)...,
				)

				backend.observer.Error("cuda.Backend.Execute", err, kv...)

				return err
			}

			kernel.FinishFramePostALU(v)

			continue
		}

		if kernel.IsCopyMaskMergeOpcode(rawOpcode) {
			kernel.ApplyCopyMaskMerge(v)
			kernel.FinishFramePostALU(v)

			continue
		}

		if kernel.IsEmitCloneOpcode(rawOpcode) {
			primitive.EmitCloneHost(value)

			continue
		}

		if opcode == kernel.OpcodeXOR && batchCount > 0 {
			distances := (*[256]uint32)(unsafe.Pointer(&v[kernel.SignalsStartWord]))

			if C.nearest_affinity_cuda(
				C.int(backend.deviceIdx),
				unsafe.Pointer(&v[0]),
				unsafe.Pointer(&v[kernel.NearestAffinityCandidatesStartWord]),
				C.uint32_t(batchCount),
				(*C.uint32_t)(unsafe.Pointer(&distances[0])),
			) != 0 {
				err := NewCUDAKernelError(
					kernel.KernelErrDispatchFailed,
					errors.New("batch distance dispatch failed"),
					"Execute",
					int(batchCount),
				)

				kv := append(
					[]any{"device_idx", backend.deviceIdx},
					kernel.CorrelationKeyvalsFlat(ptr)...,
				)

				backend.observer.Error("cuda.Backend.Execute", err, kv...)

				return err
			}

			bestIdx := uint64(0)
			bestDist := uint64(distances[0])

			for idx := uint64(1); idx < batchCount; idx++ {
				dist := uint64(distances[idx])

				if dist < bestDist {
					bestDist = dist
					bestIdx = idx
				}
			}

			v[kernel.SignalsStartWord+kernel.SignalBestIdxOffset] = bestIdx
			v[kernel.SignalsStartWord+kernel.SignalBestDistOffset] = bestDist

			kernel.FinishFramePostALU(v)

			continue
		}

		if C.unified_bitwise_cuda(
			C.int(backend.deviceIdx),
			ptr,
			C.uint32_t(1),
		) != 0 {
			err := NewCUDAKernelError(
				kernel.KernelErrDispatchFailed,
				errors.New("unified bitwise dispatch failed"),
				"Execute",
				1,
			)

			kv := append(
				[]any{"device_idx", backend.deviceIdx},
				kernel.CorrelationKeyvalsFlat(ptr)...,
			)

			backend.observer.Error("cuda.Backend.Execute", err, kv...)

			return err
		}

		kernel.FinishFramePostALU(v)
	}

	return nil
}

/*
ExecutePointers resolves pointers to arena indices; non-arena frames error.
*/
func (backend *Backend) ExecutePointers(frames []unsafe.Pointer) error {
	indices, err := primitive.IndicesFromPointers(frames)
	if err != nil {
		return err
	}

	return backend.Execute(indices)
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
