package cpu

import (
	"context"
	"runtime"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
)

type Backend struct {
	ctx    context.Context
	cancel context.CancelFunc
}

type backendOption func(*Backend)

func NewBackend(ctx context.Context, opts ...backendOption) *Backend {
	ctx, cancel := context.WithCancel(ctx)

	backend := &Backend{
		ctx:    ctx,
		cancel: cancel,
	}

	for _, opt := range opts {
		opt(backend)
	}

	return backend
}

func Available() int                  { return runtime.NumCPU() }
func (backend *Backend) Name() string { return "cpu" }

/*
Execute walks each Value frame: geometric opcodes (wordblock assembly where
available), batch nearest-affinity when opcode is OpcodeXOR and batchCount > 0, then
universalBitwiseV2 — the same symbol implemented in wordblock_amd64.s /
wordblock_arm64.s (SIMD) or as a stub in wordblock_generic.go on other GOARCHes.
*/
func (backend *Backend) Execute(frames []unsafe.Pointer) error {
	if len(frames) == 0 {
		return nil
	}

	const numRotations = 16

	for _, ptr := range frames {
		if ptr == nil {
			return NewCPUKernelError(kernel.KernelErrNilPointer, nil, "Execute")
		}

		value := (*uint64)(ptr)
		frameWords := (*[128]uint64)(ptr)

		rawOpcode := frameWords[kernel.ProgramStartWord] & 0xFF
		opcode := rawOpcode & kernel.OpcodeBooleanMask
		batchCount := frameWords[kernel.NearestAffinityBatchWord]

		if batchCount > uint64(kernel.MaxNearestAffinityCandidates) {
			batchCount = uint64(kernel.MaxNearestAffinityCandidates)
		}

		if kernel.IsGeometricOpcode(rawOpcode) {
			if geometricFrame(value, rawOpcode) {
				continue
			}
		}

		if opcode == kernel.OpcodeXOR && batchCount > 0 {
			nearestBatchReduce(frameWords, batchCount)

			continue
		}

		universalBitwiseV2(value, numRotations)
	}

	return nil
}
