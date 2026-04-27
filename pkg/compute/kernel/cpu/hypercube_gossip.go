package cpu

import (
	"math/bits"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/primitive"
)

const (
	ProgramStartWord  = 16
	ProgramWords      = 16
	SpawnRegisterWord = 70
	// ReferenceWord is the absolute frame offset of properties.reference
	// (properties region start = 56, REFERENCE = property index 11).
	// stage(B) instructions push the bound B into staging[ownerFrame[ReferenceWord]].
	ReferenceWord = 67
)

/*
HypercubeGossip is the Host Orchestrator.
It prepares the spatial layout and dimension limits, then dispatches
the execution to the hardware kernel.
*/
func (backend *Backend) HypercubeGossip(
	owner *primitive.Value, community []*primitive.Value,
) ([]*primitive.Value, []kernel.StageRequest, error) {
	if owner == nil || len(community) == 0 {
		return nil, nil, nil
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

	// DISPATCH TO KERNEL
	// Predicate (folded compare) instructions live only in the Go kernel;
	// frames that use them bypass the asm fast path so semantics stay in
	// one place. Pure truth-table frames stay on asm. The Go kernel is
	// also the only path that observes stage-bit instructions, so any
	// program using stage(...) takes that path through frameHasPredicate.
	var stageIdx []uint64
	if frameHasPredicate(ownerFrame) {
		stageIdx = backend.executeKernelGo(
			ownerFrame, ownerIdx, communityFrames, communitySize, dimCount,
		)
	} else {
		executeKernel(
			backend, ownerFrame, ownerIdx, communityFrames, communitySize, dimCount,
		)
	}

	// Translate kernel-side index requests into Value-pointer pairs the
	// compute backend can hand to StageInto.
	var staged []kernel.StageRequest
	if len(stageIdx) > 0 {
		ownerRef := ownerFrame[ReferenceWord]
		staged = make([]kernel.StageRequest, 0, len(stageIdx))
		for _, idx := range stageIdx {
			if idx < communitySize {
				staged = append(staged, kernel.StageRequest{
					OwnerID: ownerRef,
					Value:   community[idx],
				})
			}
		}
	}

	// HOST MEMORY ALLOCATION (Post-Processing)
	var spawned []*primitive.Value
	spawnCount := ownerFrame[SpawnRegisterWord]

	if spawnCount > 0 {
		ownerFrame[SpawnRegisterWord] = 0 // Clear register

		for range spawnCount {
			child := primitive.AllocValue()

			if child != nil {
				childFrame := (*[128]uint64)(unsafe.Pointer(child))
				copy(childFrame[:], ownerFrame[:])

				child.StampID()
				child.ClearProgram()
				child.SetStatus(primitive.PENDING)
				childFrame[SpawnRegisterWord] = 0
				spawned = append(spawned, child)
			}
		}
	}

	return spawned, staged, nil
}

/*
GeometricFrame satisfies kernel.Substrate by delegating to the package
free-function dispatcher (asm or scalar fallback selected at build).
*/
func (backend *Backend) GeometricFrame(value unsafe.Pointer, opcode uint64) bool {
	return GeometricFrame(value, opcode)
}
