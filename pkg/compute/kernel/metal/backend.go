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
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/theapemachine/six/pkg/primitive"
)

//go:generate xcrun -sdk macosx metal -std=metal3.1 -mmacosx-version-min=14.0 -I. -c backend.metal -o backend.air
//go:generate xcrun -sdk macosx metallib backend.air -o backend.metallib

//go:embed backend.metallib
var backendMetallib []byte

const (
	frameWords        = primitive.WordCount
	maxMetalSpawn     = 1024
	spawnRegisterWord = 70
)

var metalReady atomic.Bool
var metalRuntimeInit sync.Once
var metalRuntimeErr error

var metalArenaInit sync.Once
var metalArenaErr error

func ensureMetalRuntime() error {
	metalRuntimeInit.Do(func() {
		tmpFile, err := os.CreateTemp("", "backend-*.metallib")

		if err != nil {
			metalRuntimeErr = err

			return
		}

		name := tmpFile.Name()

		defer func() {
			_ = os.Remove(name)
		}()

		if _, err := tmpFile.Write(backendMetallib); err != nil {
			tmpFile.Close()
			metalRuntimeErr = err

			return
		}

		if err := tmpFile.Close(); err != nil {
			metalRuntimeErr = err

			return
		}

		cPath := C.CString(name)
		defer C.free(unsafe.Pointer(cPath))

		if res := C.init_metal(cPath); res != 0 {
			metalRuntimeErr = errors.New("metal: init_metal failed")

			return
		}

		metalReady.Store(true)
	})

	return metalRuntimeErr
}

func ensureMetalArena() error {
	if err := ensureMetalRuntime(); err != nil {
		return err
	}

	metalArenaInit.Do(func() {
		primitive.EnsureArenaPinnedForGPU()

		base, byteLen := primitive.ArenaBasePointer()
		if base == nil || byteLen == 0 {
			metalArenaErr = errors.New("metal: empty value arena")

			return
		}

		if C.init_metal_arena(
			base,
			C.size_t(byteLen),
			(*C.uint32_t)(unsafe.Pointer(primitive.ArenaLinearNextPtr())),
		) != 0 {
			metalArenaErr = errors.New("metal: init_metal_arena failed")
		}
	})

	return metalArenaErr
}

/*
Backend runs the in-band Value kernels on Apple Silicon (shared memory).
It satisfies the full kernel.Substrate contract by executing resident packed
AST programs directly against the pinned Value arena.

Pressure tracks inflight dispatches plus an EMA over per-job service
time (ns) so compute.Backend can pick the lowest-loaded substrate.
*/
type Backend struct {
	idx int
}

type backendOption func(*Backend)

/*
NewBackend returns a Metal kernel Backend.
*/
func NewBackend(idx int, opts ...backendOption) *Backend {
	backend := &Backend{
		idx: idx,
	}

	for _, opt := range opts {
		opt(backend)
	}

	return backend
}

func (backend *Backend) Close() error {
	return nil
}

/*
Available returns the number of Metal-capable GPUs present on this system,
or an error if the Metal runtime failed to initialize.
*/
func Available() int {
	if err := ensureMetalRuntime(); err != nil || !metalReady.Load() {
		return 0
	}

	return int(C.count_metal_devices())
}

func (backend *Backend) HypercubeGossip(value *primitive.Value, community []*primitive.Value) ([]*primitive.Value, error) {
	n := len(community)
	if value == nil || n == 0 {
		return nil, nil
	}

	if err := ensureMetalArena(); err != nil {
		return nil, err
	}

	const invalidIndex = ^uint32(0)

	ownerSlot, ok := primitive.ArenaIndex(value)
	if !ok {
		return nil, fmt.Errorf("metal: owner value is outside the arena")
	}

	indices := make([]uint32, n)
	for idx := range indices {
		indices[idx] = invalidIndex
	}

	for idx, v := range community {
		if v == nil {
			continue
		}

		if slot, ok := primitive.ArenaIndex(v); ok {
			indices[idx] = slot
			continue
		}

		return nil, fmt.Errorf("metal: value at community index %d is outside the arena", idx)
	}

	ownerIndex := invalidIndex
	for idx, candidate := range community {
		if candidate == value {
			ownerIndex = uint32(idx)
			break
		}
	}

	stageIndices := make([]uint32, n)
	stageCount := uint32(0)

	for idx := range stageIndices {
		stageIndices[idx] = invalidIndex
	}

	res := C.hypercube_gossip_metal_indices(
		(*C.uint32_t)(unsafe.Pointer(&indices[0])),
		C.uint32_t(n),
		C.uint32_t(ownerIndex),
		C.uint32_t(ownerSlot),
		(*C.uint32_t)(unsafe.Pointer(&stageIndices[0])),
		(*C.uint32_t)(unsafe.Pointer(&stageCount)),
	)
	if res != 0 {
		return nil, fmt.Errorf("metal: hypercube_gossip_metal_indices failed with code %d", int(res))
	}

	ownerFrame := (*[frameWords]uint64)(unsafe.Pointer(value))

	spawned := backend.metalSpawnedValues(ownerFrame)

	return spawned, nil
}

/*
metalSpawnedValues materializes children requested by the Metal kernel after
the in-band program increments the owner spawn register. The owner frame is
copied into each child, then child-local identity and scheduling status are
re-derived so callers receive ready children only when a resident program was
copied into the emitted Value.
*/
func (backend *Backend) metalSpawnedValues(ownerFrame *[frameWords]uint64) []*primitive.Value {
	spawnCount := ownerFrame[spawnRegisterWord]
	if spawnCount == 0 {
		return nil
	}

	if spawnCount > maxMetalSpawn {
		fmt.Fprintf(os.Stderr, "metal: spawn count %d exceeds max %d; clamping\n", spawnCount, maxMetalSpawn)
		spawnCount = maxMetalSpawn
	}

	ownerFrame[spawnRegisterWord] = 0

	allocFailures := uint64(0)
	spawned := make([]*primitive.Value, 0, int(spawnCount))
	for spawnIdx := uint64(0); spawnIdx < spawnCount; spawnIdx++ {
		child := primitive.AllocValue()
		if child == nil {
			allocFailures++
			fmt.Fprintf(os.Stderr, "metal: AllocValue failed for spawn %d/%d\n", spawnIdx+1, spawnCount)

			continue
		}

		childFrame := (*[frameWords]uint64)(unsafe.Pointer(child))
		copy(childFrame[:], ownerFrame[:])

		child.StampID()
		childFrame[spawnRegisterWord] = 0
		if child.HasProgram() {
			child.SetSchedulingNext(child.ID())
			child.SetStatus(primitive.READY)
			spawned = append(spawned, child)

			continue
		}

		child.SetSchedulingNext(0)
		child.SetStatus(primitive.PENDING)
		spawned = append(spawned, child)
	}

	if allocFailures != 0 {
		fmt.Fprintf(os.Stderr, "metal: %d/%d spawned value allocations failed\n", allocFailures, spawnCount)
	}

	return spawned
}

func (backend *Backend) GeometricFrame(value unsafe.Pointer, opcode uint64) bool {
	if err := ensureMetalArena(); err != nil {
		return false
	}

	target := (*primitive.Value)(value)
	frame := (*[frameWords]uint64)(value)
	prev := frame[primitive.ProgramStartWord]
	frame[primitive.ProgramStartWord] = opcode
	defer func() {
		frame[primitive.ProgramStartWord] = prev
	}()

	idx, ok := primitive.ArenaIndex(target)
	if !ok {
		return false
	}

	res := C.geometric_metal_indices((*C.uint32_t)(unsafe.Pointer(&idx)), 1)
	return res == 0
}

func (backend *Backend) Name() string { return "metal" }
