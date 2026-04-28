//go:build cuda && cgo

package cuda

/*
#cgo LDFLAGS: -L${SRCDIR} -lbackend -lcudart
#include <stdint.h>
int cuda_device_count();
void cleanup_cuda_pools();

typedef struct {
    uint64_t kind;
    uint64_t start;
    uint64_t span;
    uint64_t threshold;
    uint64_t and_word;
    uint64_t threshold_b;
} predicate_device_spec_t;

int hypercube_gossip_cuda(
    int                         device_id,
    uint64_t*                   value_frames_host,
    uint8_t*                    active_host,
    uint32_t                    value_count,
    uint32_t                    owner_index,
    predicate_device_spec_t*    predicates_host,
    uint64_t*                   spawn_frames_host,
    uint64_t*                   spawn_ids_host,
    uint8_t*                    spawn_active_host
);

int hypercube_gossip_v2_cuda(
    int        device_id,
    uint64_t*  value_frames_host,
    uint8_t*   active_host,
    uint32_t   value_count,
    uint32_t   owner_index,
    uint32_t*  stage_indices_host,
    uint32_t*  stage_count_host,
    uint64_t*  spawn_frames_host,
    uint64_t*  spawn_ids_host,
    uint8_t*   spawn_active_host
);

int geometric_cuda(
    int device_id,
    void* a_host,
    uint32_t num_values
);
*/
import "C"
import (
	"context"
	"fmt"
	"sync"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/primitive"
)

//go:generate nvcc -lib backend.cu -o libbackend.a -std=c++11

/*
Backend dispatches Value-native GPU kernels on NVIDIA CUDA devices.
HypercubeGossip executes the resident packed AST on device; spawned frames are
returned to Go only for arena ownership and ID publication.
*/
type Backend struct {
	initOnce    sync.Once
	deviceCount int
	deviceIdx   int
	ctx         context.Context
	cancel      context.CancelFunc
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

func (backend *Backend) Close() error {
	if backend.cancel != nil {
		backend.cancel()
	}

	C.cleanup_cuda_pools()
	return nil
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

func (backend *Backend) HypercubeGossip(value *primitive.Value, community []*primitive.Value) ([]*primitive.Value, []kernel.StageRequest, error) {
	n := len(community)
	if n == 0 {
		return nil, nil, nil
	}

	frames := make([]primitive.Value, n)
	active := make([]uint8, n)

	for i, member := range community {
		if member == nil {
			continue
		}

		frames[i] = *member
		active[i] = 1
	}

	ownerIndex := ^uint32(0)
	if value != nil {
		for idx, candidate := range community {
			if candidate == value {
				ownerIndex = uint32(idx)
				break
			}
		}
	}

	stageIndices := make([]uint32, n)
	var stageCount uint32

	spawnValues, spawnFrames, spawnIDs := cudaSpawnBuffers(value, community)
	spawnActive := make([]uint8, n)

	res := C.hypercube_gossip_v2_cuda(
		C.int(backend.deviceIdx),
		(*C.uint64_t)(unsafe.Pointer(&frames[0])),
		(*C.uint8_t)(unsafe.Pointer(&active[0])),
		C.uint32_t(n),
		C.uint32_t(ownerIndex),
		(*C.uint32_t)(unsafe.Pointer(&stageIndices[0])),
		(*C.uint32_t)(unsafe.Pointer(&stageCount)),
		(*C.uint64_t)(unsafe.Pointer(&spawnFrames[0])),
		(*C.uint64_t)(unsafe.Pointer(&spawnIDs[0])),
		(*C.uint8_t)(unsafe.Pointer(&spawnActive[0])),
	)
	if res != 0 {
		primitive.CloseAll(spawnValues)
		return nil, nil, fmt.Errorf("cuda: hypercube_gossip_v2_cuda failed with code %d", int(res))
	}

	for idx, member := range community {
		if member == nil {
			continue
		}

		*member = frames[idx]
	}

	// Translate kernel-side index requests into Value-pointer pairs the
	// compute backend can hand to StageInto. The owner's reference word
	// names the staging lane.
	var staged []kernel.StageRequest
	if stageCount > 0 && value != nil {
		ownerRef := uint64((*[128]uint64)(unsafe.Pointer(value))[ReferenceWord])

		staged = make([]kernel.StageRequest, 0, stageCount)
		for idx := uint32(0); idx < stageCount && idx < uint32(n); idx++ {
			peerIdx := stageIndices[idx]
			if int(peerIdx) >= n || community[peerIdx] == nil {
				continue
			}

			staged = append(staged, kernel.StageRequest{
				OwnerID: ownerRef,
				Value:   community[peerIdx],
			})
		}
	}

	var spawned []*primitive.Value
	for idx, child := range spawnValues {
		if child == nil {
			continue
		}

		if idx < len(spawnActive) && spawnActive[idx] != 0 {
			*child = spawnFrames[idx]
			spawned = append(spawned, child)
			continue
		}

		child.Close()
		spawnValues[idx] = nil
	}

	return spawned, staged, nil
}

// ReferenceWord mirrors the absolute frame word for the REFERENCE
// property (propertiesStart + REFERENCE offset). Hardcoded to keep this
// file standalone — the canonical source is pkg/primitive/properties.go.
const ReferenceWord = 67

// HypercubeGossipLegacy preserves the original CUDA dispatch path so
// the kernel can be reactivated once the .cu kernel is rewritten for
// the new ALU. It is intentionally unused by the runtime today.
//
//nolint:unused
func (backend *Backend) hypercubeGossipLegacy(value *primitive.Value, community []*primitive.Value) ([]*primitive.Value, error) {
	n := len(community)
	if n == 0 {
		return nil, nil
	}

	frames := make([]primitive.Value, n)
	active := make([]uint8, n)
	for i, v := range community {
		if v == nil {
			continue
		}

		frames[i] = *v
		active[i] = 1
	}

	ownerIndex := ^uint32(0)
	if value != nil {
		for idx, candidate := range community {
			if candidate == value {
				ownerIndex = uint32(idx)
				break
			}
		}
	}

	spawnValues, spawnFrames, spawnIDs := cudaSpawnBuffers(value, community)
	spawnActive := make([]uint8, n)

	res := C.hypercube_gossip_cuda(
		C.int(backend.deviceIdx),
		(*C.uint64_t)(unsafe.Pointer(&frames[0])),
		(*C.uint8_t)(unsafe.Pointer(&active[0])),
		C.uint32_t(n),
		C.uint32_t(ownerIndex),
		nil,
		(*C.uint64_t)(unsafe.Pointer(&spawnFrames[0])),
		(*C.uint64_t)(unsafe.Pointer(&spawnIDs[0])),
		(*C.uint8_t)(unsafe.Pointer(&spawnActive[0])),
	)
	if res != 0 {
		primitive.CloseAll(spawnValues)
		return nil, fmt.Errorf("cuda: hypercube_gossip_cuda failed with code %d", int(res))
	}

	for i, v := range community {
		if v == nil {
			continue
		}

		*v = frames[i]
	}

	for idx, spawn := range spawnValues {
		if spawn == nil {
			continue
		}
		if idx < len(spawnActive) && spawnActive[idx] != 0 {
			*spawn = spawnFrames[idx]
			continue
		}

		spawn.Close()
		spawnValues[idx] = nil
	}

	spawned := make([]*primitive.Value, 0, len(spawnValues))
	for _, spawn := range spawnValues {
		if spawn != nil {
			spawned = append(spawned, spawn)
		}
	}

	return spawned, nil
}

func (backend *Backend) GeometricFrame(value unsafe.Pointer, opcode uint64) bool {
	frame := (*[128]uint64)(value)
	prev := frame[primitive.ProgramStartWord]
	frame[primitive.ProgramStartWord] = opcode
	defer func() {
		frame[primitive.ProgramStartWord] = prev
	}()

	res := C.geometric_cuda(C.int(backend.deviceIdx), value, 1)
	return res == 0
}

func cudaSpawnBuffers(value *primitive.Value, community []*primitive.Value) ([]*primitive.Value, []primitive.Value, []uint64) {
	spawnFrames := make([]primitive.Value, len(community))
	spawnIDs := make([]uint64, len(community))

	if !communityMaySpawn(value, community) {
		return nil, spawnFrames, spawnIDs
	}

	spawnValues := make([]*primitive.Value, len(community))
	for idx, source := range community {
		if source == nil {
			continue
		}

		spawn := primitive.AllocValue()
		if spawn == nil {
			continue
		}
		spawn.StampID()

		spawnValues[idx] = spawn
		spawnIDs[idx] = spawn.ID()
	}

	return spawnValues, spawnFrames, spawnIDs
}

func communityMaySpawn(value *primitive.Value, community []*primitive.Value) bool {
	if value != nil {
		return valueMaySpawn(value)
	}

	for _, candidate := range community {
		if valueMaySpawn(candidate) {
			return true
		}
	}

	return false
}

func valueMaySpawn(value *primitive.Value) bool {
	if value == nil {
		return false
	}

	frame := (*[primitive.WordCount]uint64)(unsafe.Pointer(value))
	for pc := 0; pc < primitive.ProgramWords; pc++ {
		instr := frame[primitive.ProgramStartWord+pc]
		if instr == 0 {
			continue
		}

		// Detect spawn intent via the new ALU's emit bit (instr[54]).
		if (instr>>54)&1 == 1 {
			return true
		}
	}

	return false
}
