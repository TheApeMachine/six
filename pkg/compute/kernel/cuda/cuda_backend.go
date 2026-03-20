//go:build cuda && cgo

package cuda

/*
#cgo LDFLAGS: -L${SRCDIR} -lresolver -lcudart
#include <stdint.h>
int cuda_device_count();
int resolve_resonance_cuda(
    int device_id,
    const void* graph_nodes_ptr,
    uint32_t num_nodes,
    const void* active_context_ptr,
    uint64_t* out_result
);
int resolve_phasedial_cuda(int device_id, const void* cache_nodes_ptr, uint32_t num_nodes, const void* query_dial_ptr, void* similarities_ptr);
int encode_phasedial_cuda(int device_id, const void* structural_phases_ptr, const void* primes_ptr, uint32_t num_values, void* out_dial_ptr);
int seq_toroidal_mean_phase_cuda(int device_id, const void* value_blocks_ptr, uint32_t num_values, double* out_theta, double* out_phi);
int weighted_circular_mean_cuda(int device_id, const void* value_blocks_ptr, uint32_t num_values, double* out_phase, double* out_concentration);
int solve_bvp_cuda(int device_id, const void* start_blocks_ptr, const void* goal_blocks_ptr, uint16_t* out_scale, uint16_t* out_translate, double* out_distance);
*/
import "C"
import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel/internal/resolve"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/numeric/geometry"
	"github.com/theapemachine/six/pkg/system/pool"
	"github.com/theapemachine/six/pkg/system/transport"
)

//go:generate nvcc -lib resolver.cu -o libresolver.a -std=c++11

type CUDABackend struct {
	*transport.Stream
	initOnce    sync.Once
	deviceCount int
}

func (backend *CUDABackend) init() {
	backend.initOnce.Do(func() {
		backend.deviceCount = int(C.cuda_device_count())
		if backend.deviceCount < 0 {
			backend.deviceCount = 0
		}
	})
}

func (backend *CUDABackend) Available() bool {
	backend.init()
	return backend.deviceCount > 0
}

func (backend *CUDABackend) Resolve(
	graphNodes unsafe.Pointer,
	numNodes int,
	context unsafe.Pointer,
) (uint64, error) {
	if numNodes <= 0 {
		return 0, CUDAErrorResolveFailed
	}
	if graphNodes == nil {
		return 0, CUDAErrorResolveFailed
	}
	if context == nil {
		return 0, CUDAErrorResolveFailed
	}

	if !backend.Available() {
		return 0, CUDAErrorUnavailable
	}

	if numNodes < 1024 {
		nodes := unsafe.Slice((*geometry.GFRotation)(graphNodes), numNodes)
		ctx := (*geometry.GFRotation)(context)
		return resolve.PackedNearest(nodes, *ctx), nil
	}
	if uint64(numNodes) > uint64(^uint32(0)) {
		return 0, CUDAErrorResolveFailed
	}

	if backend.deviceCount == 1 {
		var packed C.uint64_t
		status := C.resolve_resonance_cuda(
			0,
			graphNodes,
			C.uint32_t(numNodes),
			context,
			&packed,
		)
		if status != 0 {
			return 0, CUDAErrorResolveFailed
		}
		return uint64(packed), nil
	}

	workerPool := pool.New(context.Background(), 1, backend.deviceCount, &pool.Config{})
	defer workerPool.Close()

	resultChans := make([]chan *pool.Result, 0, backend.deviceCount)

	chunkSize := (numNodes + backend.deviceCount - 1) / backend.deviceCount
	stride := unsafe.Sizeof(geometry.GFRotation{})

	for dev := 0; dev < backend.deviceCount; dev++ {
		start := dev * chunkSize
		if start >= numNodes {
			break
		}
		end := start + chunkSize
		if end > numNodes {
			end = numNodes
		}

		deviceID := dev
		offset := start
		length := end - start
		resultChans = append(resultChans, workerPool.Schedule(
			fmt.Sprintf("cuda-resolve-%d-%d", deviceID, offset),
			func(ctx context.Context) (any, error) {
				shardPtr := unsafe.Pointer(uintptr(graphNodes) + uintptr(offset)*stride)

				var packed C.uint64_t
				status := C.resolve_resonance_cuda(
					C.int(deviceID),
					shardPtr,
					C.uint32_t(length),
					context,
					&packed,
				)
				if status != 0 {
					return uint64(0), CUDAErrorResolveFailed
				}

				return numeric.RebasePackedID(uint64(packed), offset), nil
			},
		))
	}

	var best uint64

	for _, resultCh := range resultChans {
		result := <-resultCh
		if result.Error != nil {
			return 0, result.Error
		}

		rebased, ok := result.Value.(uint64)
		if !ok {
			return 0, CUDAErrorResolveFailed
		}

		if rebased > best {
			best = rebased
		}
	}

	return best, nil
}

type CUDAError string

const (
	CUDAErrorUnavailable   CUDAError = "cuda backend unavailable"
	CUDAErrorResolveFailed CUDAError = "cuda backend resolve failed"
)

func (err CUDAError) Error() string {
	return string(err)
}

func (backend *CUDABackend) ResolvePhaseDial(
	cacheNodes unsafe.Pointer,
	numNodes int,
	queryDial unsafe.Pointer,
	similarities unsafe.Pointer,
) error {
	if !backend.Available() {
		return CUDAErrorUnavailable
	}
	if numNodes <= 0 {
		return CUDAErrorResolveFailed
	}
	status := C.resolve_phasedial_cuda(0, cacheNodes, C.uint32_t(numNodes), queryDial, similarities)
	if status != 0 {
		return CUDAErrorResolveFailed
	}
	return nil
}

func (backend *CUDABackend) EncodePhaseDial(
	structuralPhases unsafe.Pointer,
	numValues int,
	outDial unsafe.Pointer,
) error {
	if !backend.Available() {
		return CUDAErrorUnavailable
	}
	if numValues <= 0 {
		return CUDAErrorResolveFailed
	}
	primes := make([]float32, 512)
	for i, p := range numeric.Primes {
		primes[i] = float32(p)
	}
	status := C.encode_phasedial_cuda(0, structuralPhases, unsafe.Pointer(&primes[0]), C.uint32_t(numValues), outDial)
	if status != 0 {
		return CUDAErrorResolveFailed
	}
	return nil
}

func (backend *CUDABackend) SeqToroidalMeanPhase(
	valueBlocks unsafe.Pointer,
	numValues int,
) (theta float64, phi float64, err error) {
	if !backend.Available() {
		return 0, 0, CUDAErrorUnavailable
	}
	if numValues <= 0 {
		return 0, 0, nil
	}
	var outTheta, outPhi C.double
	status := C.seq_toroidal_mean_phase_cuda(0, valueBlocks, C.uint32_t(numValues), &outTheta, &outPhi)
	if status != 0 {
		return 0, 0, CUDAErrorResolveFailed
	}
	return float64(outTheta), float64(outPhi), nil
}

func (backend *CUDABackend) WeightedCircularMean(
	valueBlocks unsafe.Pointer,
	numValues int,
) (phase float64, concentration float64, err error) {
	if !backend.Available() {
		return 0, 0, CUDAErrorUnavailable
	}
	if numValues <= 0 {
		return 0, 0, nil
	}
	var outPhase, outConcentration C.double
	status := C.weighted_circular_mean_cuda(0, valueBlocks, C.uint32_t(numValues), &outPhase, &outConcentration)
	if status != 0 {
		return 0, 0, CUDAErrorResolveFailed
	}
	return float64(outPhase), float64(outConcentration), nil
}

func (backend *CUDABackend) SolveBVP(
	startBlocks unsafe.Pointer,
	goalBlocks unsafe.Pointer,
) (scale uint16, translate uint16, distance float64, err error) {
	if !backend.Available() {
		return 0, 0, 0, CUDAErrorUnavailable
	}
	var outScale, outTranslate C.uint16_t
	var outDistance C.double
	status := C.solve_bvp_cuda(0, startBlocks, goalBlocks, &outScale, &outTranslate, &outDistance)
	if status != 0 {
		return 0, 0, 0, CUDAErrorResolveFailed
	}
	return uint16(outScale), uint16(outTranslate), float64(outDistance), nil
}
