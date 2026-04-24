//go:build cuda && cgo

package cuda

/*
#cgo LDFLAGS: -L${SRCDIR} -lbackend -lcudart
#include <stdint.h>
int cuda_device_count();
void cleanup_cuda_pools();

int nearest_affinity_cuda(int device_id, void* query_host, void* candidates_host, uint32_t count, uint64_t* best_packed_result_host);
*/
import "C"
import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/primitive"
)

//go:generate nvcc -lib backend.cu -o libbackend.a -std=c++11

/*
Backend dispatches Value-native GPU kernels on NVIDIA CUDA devices.
The in-band VM (UniversalBitwise) runs in-process; geometric_cuda and
other kernels stay on device.
*/
type Backend struct {
	initOnce    sync.Once
	deviceCount int
	deviceIdx   int
	ctx         context.Context
	cancel      context.CancelFunc
	inflight    atomic.Int64
	emaNs       atomic.Uint64
}

type backendOption func(*Backend)

func NewBackend(idx int, opts ...backendOption) *Backend {
	ctx, cancel := context.WithCancel(context.Background())
	backend := &Backend{
		deviceIdx: idx,
		ctx:       ctx,
		cancel:    cancel,
	}

	for _, opt := range opts {
		opt(backend)
	}

	return backend
}

func (backend *Backend) Name() string { return "cuda" }

func (backend *Backend) Close() {
	if backend.cancel != nil {
		backend.cancel()
	}

	C.cleanup_cuda_pools()
}

func (backend *Backend) init() {
	backend.initOnce.Do(func() {
		backend.deviceCount = int(C.cuda_device_count())

		if backend.deviceCount < 0 {
			backend.deviceCount = 0
		}
	})
}

func Available() int {
	b := NewBackend(0)
	b.init()

	return b.deviceCount
}

const cudaCommunityBreakEven = 16

func (backend *Backend) Pressure() (inflight int64, emaNs uint64) {
	return backend.inflight.Load(), backend.emaNs.Load()
}

func (backend *Backend) CanProfit(kind kernel.JobKind, size int) bool {
	backend.init()
	if backend.deviceCount == 0 {
		return false
	}
	switch kind {
	case kernel.JobKindCommunity:
		return size >= cudaCommunityBreakEven
	default:
		return false
	}
}

func (backend *Backend) recordService(start time.Time) {
	elapsed := uint64(time.Since(start).Nanoseconds())
	for {
		old := backend.emaNs.Load()
		next := old - (old >> 3) + (elapsed >> 3)
		if old == 0 {
			next = elapsed
		}
		if backend.emaNs.CompareAndSwap(old, next) {
			return
		}
	}
}

func (backend *Backend) UniversalBitwise(a, b, dst *primitive.Value) {
	backend.inflight.Add(1)
	start := time.Now()
	defer func() {
		backend.inflight.Add(-1)
		backend.recordService(start)
	}()
	kernel.RunUniversalBitwise(a, b, dst)
}

func (backend *Backend) AssignFirstFit(
	communityORs [][primitive.AffinityWords]uint64,
	valueAffinities [][primitive.AffinityWords]uint64,
	hammingBudget uint32,
	saturationCap uint32,
) []int32 {
	backend.inflight.Add(1)
	start := time.Now()
	defer func() {
		backend.inflight.Add(-1)
		backend.recordService(start)
	}()
	out, err := backend.BatchFirstFit(communityORs, valueAffinities, hammingBudget, saturationCap)
	if err != nil {
		return nil
	}
	return out
}

func (backend *Backend) ExecuteCommunity(community []*primitive.Value) []*primitive.Value {
	if len(community) == 0 {
		return nil
	}
	backend.inflight.Add(1)
	start := time.Now()
	defer func() {
		backend.inflight.Add(-1)
		backend.recordService(start)
	}()

	return cpu.ExecuteCommunity(community)
}
