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

	"github.com/theapemachine/six/pkg/compute/program"
	"github.com/theapemachine/six/pkg/primitive"
)

//go:generate xcrun -sdk macosx metal -std=metal3.1 -mmacosx-version-min=14.0 -I. -c backend.metal -o backend.air
//go:generate xcrun -sdk macosx metallib backend.air -o backend.metallib

//go:embed backend.metallib
var backendMetallib []byte

var metalReady atomic.Bool

var metalArenaInit sync.Once
var metalArenaErr error

func ensureMetalArena() error {
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
	cleanupMetalPools()
	return nil
}

/*
Available returns the number of Metal-capable GPUs present on this system,
or an error if the Metal runtime failed to initialize.
*/
func Available() int {
	if !metalReady.Load() {
		return 0
	}

	return int(C.count_metal_devices())
}

func (backend *Backend) HypercubeGossip(value *primitive.Value, community []*primitive.Value) ([]*primitive.Value, error) {
	n := len(community)
	if n == 0 {
		return nil, nil
	}

	if err := ensureMetalArena(); err != nil {
		return nil, err
	}

	const invalidIndex = ^uint32(0)

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
	if value != nil {
		for idx, candidate := range community {
			if candidate == value {
				ownerIndex = uint32(idx)
				break
			}
		}
	}

	spawnValues, spawnIndices, spawnIDs := metalSpawnBuffers(value, community)
	spawnActive := make([]uint8, n)
	predicateSpecs := program.PredicateDeviceSpecs()

	active := false
	for _, idx := range indices {
		if idx != invalidIndex {
			active = true
			break
		}
	}
	if !active {
		return nil, nil
	}

	res := C.hypercube_gossip_metal_indices(
		(*C.uint32_t)(unsafe.Pointer(&indices[0])),
		C.uint32_t(n),
		C.uint32_t(ownerIndex),
		(*C.predicate_device_spec_t)(unsafe.Pointer(&predicateSpecs[0])),
		(*C.uint32_t)(unsafe.Pointer(&spawnIndices[0])),
		(*C.uint64_t)(unsafe.Pointer(&spawnIDs[0])),
		(*C.uint8_t)(unsafe.Pointer(&spawnActive[0])),
	)
	if res != 0 {
		primitive.CloseAll(spawnValues)
		return nil, fmt.Errorf("metal: hypercube_gossip_metal_indices failed with code %d", int(res))
	}

	return collectActiveSpawned(spawnValues, spawnActive), nil
}

func (backend *Backend) GeometricFrame(value unsafe.Pointer, opcode uint64) bool {
	if err := ensureMetalArena(); err != nil {
		return false
	}

	target := (*primitive.Value)(value)
	frame := (*[128]uint64)(value)
	prev := frame[16]
	frame[16] = opcode
	defer func() {
		frame[16] = prev
	}()

	idx, ok := primitive.ArenaIndex(target)
	if !ok {
		return false
	}

	res := C.geometric_metal_indices((*C.uint32_t)(unsafe.Pointer(&idx)), 1)
	return res == 0
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
		reportInitError(errors.New("metal: init_metal failed"))

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

func (backend *Backend) Name() string { return "metal" }

func metalSpawnBuffers(value *primitive.Value, community []*primitive.Value) ([]*primitive.Value, []uint32, []uint64) {
	const invalidIndex = ^uint32(0)

	spawnIndices := make([]uint32, len(community))
	spawnIDs := make([]uint64, len(community))
	for idx := range spawnIndices {
		spawnIndices[idx] = invalidIndex
	}

	if !communityMaySpawn(value, community) {
		return nil, spawnIndices, spawnIDs
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
		slot, ok := primitive.ArenaIndex(spawn)
		if !ok {
			spawn.Close()
			continue
		}

		spawnValues[idx] = spawn
		spawnIndices[idx] = slot
		spawnIDs[idx] = spawn.ID()
	}

	return spawnValues, spawnIndices, spawnIDs
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

		_, _, _, _, _, _, _, _, topology, _, _, _, _ := program.DecodeInstruction(instr)
		if topology == program.TopologySpawn {
			return true
		}
	}

	return false
}

func collectActiveSpawned(spawnValues []*primitive.Value, spawnActive []uint8) []*primitive.Value {
	if len(spawnValues) == 0 {
		return nil
	}

	spawned := make([]*primitive.Value, 0, len(spawnValues))
	for idx, spawn := range spawnValues {
		if spawn == nil {
			continue
		}
		if idx < len(spawnActive) && spawnActive[idx] != 0 {
			spawned = append(spawned, spawn)
			continue
		}

		spawn.Close()
	}

	return spawned
}
