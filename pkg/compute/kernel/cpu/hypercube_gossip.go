package cpu

import (
	"math/bits"
	"unsafe"

	"github.com/theapemachine/six/pkg/primitive"
)

const (
	ProgramStartWord  = 16
	ProgramWords      = 16
	SpawnRegisterWord = 70
	// ReferenceWord is the absolute frame offset of properties.reference
	// (properties region start = 56, REFERENCE = property index 11).
	ReferenceWord = 67
)

/*
HypercubeGossip is the Host Orchestrator.
It prepares the spatial layout and dimension limits, then dispatches
the execution to the hardware kernel.
*/
func (backend *Backend) HypercubeGossip(
	owner *primitive.Value, community []*primitive.Value,
) ([]*primitive.Value, error) {
	if owner == nil || len(community) == 0 {
		return nil, nil
	}

	ownerFrame := (*[128]uint64)(unsafe.Pointer(owner))

	// Map the community into raw memory frames for the ALU. ownerIdx is
	// sentinel-valued when the owner is not itself a community member —
	// the kernel uses that to know it should not skip any positional
	// slot during gossip folds.
	communityFrames := make([]*[128]uint64, len(community))
	ownerIdx := ^uint64(0)

	for i, v := range community {
		communityFrames[i] = (*[128]uint64)(unsafe.Pointer(v))
		if v == owner {
			ownerIdx = uint64(i)
		}
	}

	// Calculate the boundary of the hypercube
	communitySize := uint64(len(community))
	dimCount := uint64(bits.Len64(communitySize - 1))

	// Dispatcher: executeKernel asm when programAsmCompatible allows;
	// otherwise executeKernelGo. Compatibility is currently permissive;
	// tighten when new opcodes require Go-only lowering.
	var stageIdx []uint64
	var childFrame [128]uint64
	var childActive bool

	if backend.programAsmCompatible(ownerFrame) {
		var stageBuf [128]uint64
		var stageCount uint64
		executeKernel(backend, ownerFrame, ownerIdx, communityFrames, communitySize, dimCount, &stageBuf, &stageCount)

		if stageCount > 0 {
			stageIdx = make([]uint64, stageCount)
			copy(stageIdx, stageBuf[:stageCount])
		}
	} else {
		stageIdx, childFrame, childActive = backend.executeKernelGo(
			ownerFrame, ownerIdx, communityFrames, communitySize, dimCount,
		)
	}

	// stageIdx is produced by legacy stage(B) firmware. Current recruitment
	// narrows lanes through SELECTED/reference tags in compute.Backend.
	_ = stageIdx

	var spawned []*primitive.Value
	spawned = append(spawned, backend.materializeChild(childFrame, childActive)...)

	spawnCount := ownerFrame[SpawnRegisterWord]

	if spawnCount > 0 {
		ownerFrame[SpawnRegisterWord] = 0 // Clear register

		for range spawnCount {
			child := primitive.AllocValue()

			if child != nil {
				childFrame := (*[128]uint64)(unsafe.Pointer(child))
				copy(childFrame[:], ownerFrame[:])

				child.StampID()
				childFrame[SpawnRegisterWord] = 0
				child.SetSchedulingNext(0)
				child.SetStatus(primitive.PENDING)
				if child.HasProgram() {
					child.SetSchedulingNext(child.ID())
					child.SetStatus(primitive.READY)
				}

				spawned = append(spawned, child)
			}
		}
	}

	return spawned, nil
}

func (backend *Backend) materializeChild(frame [128]uint64, active bool) []*primitive.Value {
	if !active {
		return nil
	}

	child := primitive.AllocValue()
	if child == nil {
		return nil
	}

	childWords := (*[128]uint64)(unsafe.Pointer(child))
	copy(childWords[:], frame[:])
	child.StampID()
	childWords[SpawnRegisterWord] = 0

	child.SetSchedulingNext(0)
	child.SetStatus(primitive.PENDING)
	if child.HasProgram() {
		child.SetSchedulingNext(child.ID())
		child.SetStatus(primitive.READY)
	}

	return []*primitive.Value{child}
}

/*
GeometricFrame satisfies kernel.Substrate by delegating to the package
free-function dispatcher (asm or scalar fallback selected at build).
*/
func (backend *Backend) GeometricFrame(value unsafe.Pointer, opcode uint64) bool {
	return GeometricFrame(value, opcode)
}
