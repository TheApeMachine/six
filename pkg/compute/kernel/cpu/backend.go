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
Execute walks each Value frame and routes it through the right substrate
path: geometric opcodes go to the PGA wordblock, OpcodeXOR with a positive
batch count goes to the batched nearest-affinity reducer, and everything
else is treated as a universal bitwise program whose operand lanes are
described by the packed region words at program[3..5].

The caller does not stage or write back anything. universalBitwiseV2 reads
srcA / srcB and writes dst directly out of the Value under the mode bit
the compiler packed into program[2], so regions the program does not name
survive every pass bit-for-bit.
*/
func (backend *Backend) Execute(frames []unsafe.Pointer) error {
	if len(frames) == 0 {
		return nil
	}

	for _, ptr := range frames {
		if ptr == nil {
			return NewCPUKernelError(kernel.KernelErrNilPointer, nil, "Execute")
		}

		value := (*uint64)(ptr)
		frameWords := (*[128]uint64)(ptr)

		rawOpcode := frameWords[kernel.ProgramOpcodeWord] & 0xFF
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

		rotationTable := frameWords[kernel.ProgramRotTabWord]

		// An all-zero rotation table means the frame has no work for the
		// universal bitwise lane. Skip it instead of running a sweep that
		// would XOR zero bytes into dst[0..min(8,span)) for nothing.
		if rotationTable == 0 {
			continue
		}

		mode := int(frameWords[kernel.ProgramModeWord] & 0xFF)
		aStart, aSpan := kernel.UnpackRegionRef(frameWords[kernel.ProgramSrcAWord])
		bStart, bSpan := kernel.UnpackRegionRef(frameWords[kernel.ProgramSrcBWord])
		dstStart, dstSpan := kernel.UnpackRegionRef(frameWords[kernel.ProgramDstWord])

		universalBitwiseV2(
			value,
			aStart, aSpan,
			bStart, bSpan,
			dstStart, dstSpan,
			mode,
			rotationTable,
		)
	}

	return nil
}
