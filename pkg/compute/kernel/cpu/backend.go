package cpu

import (
	"context"
	"runtime"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/primitive"
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

func (backend *Backend) Shutdown() {
	if backend == nil || backend.cancel == nil {
		return
	}

	backend.cancel()
}

func Available() int                  { return runtime.NumCPU() }
func (backend *Backend) Name() string { return "cpu" }

func (backend *Backend) exactBinaryWord(op uint64, a uint64, b uint64) uint64 {
	m0 := uint64(0)
	if op&0x1 != 0 {
		m0 = ^uint64(0)
	}
	m1 := uint64(0)
	if op&0x2 != 0 {
		m1 = ^uint64(0)
	}
	m2 := uint64(0)
	if op&0x4 != 0 {
		m2 = ^uint64(0)
	}
	m3 := uint64(0)
	if op&0x8 != 0 {
		m3 = ^uint64(0)
	}
	return (a & b & m0) |
		(a & ^b & m1) |
		(^a & b & m2) |
		(^a & ^b & m3)
}

func (backend *Backend) exactBinary(frameWords *[128]uint64, op uint64, aStart int, aSpan int, bStart int, bSpan int, dstStart int, dstSpan int) {
	if frameWords == nil || aSpan <= 0 || bSpan <= 0 || dstSpan <= 0 {
		return
	}
	limit := aSpan
	if bSpan < limit {
		limit = bSpan
	}
	if dstSpan < limit {
		limit = dstSpan
	}
	if limit <= 0 || aStart < 0 || bStart < 0 || dstStart < 0 {
		return
	}
	if aStart+limit > len(frameWords) || bStart+limit > len(frameWords) || dstStart+limit > len(frameWords) {
		return
	}
	for idx := 0; idx < limit; idx++ {
		frameWords[dstStart+idx] = backend.exactBinaryWord(op, frameWords[aStart+idx], frameWords[bStart+idx])
	}
}

const (
	regionEntryWords = 6
	maxRegionEntries = 10
	regionTableWords = regionEntryWords * maxRegionEntries
)

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
func (backend *Backend) Execute(indices []uint32) error {
	if len(indices) == 0 {
		return nil
	}

	ptrs := make([]unsafe.Pointer, 0, len(indices))

	for _, idx := range indices {
		value := primitive.ValueAt(idx)
		if value == nil {
			return NewCPUKernelError(kernel.KernelErrNilPointer, nil, "Execute")
		}

		ptrs = append(ptrs, unsafe.Pointer(&value[0]))
	}

	return backend.ExecutePointers(ptrs)
}

/*
ExecutePointers runs the CPU ALU on arbitrary host pointers. Used for heap
Values and tests; arena dispatch should prefer Execute with indices so GPU
substrates can stay pointer-free on the PCIe path.
*/
func (backend *Backend) ExecutePointers(frames []unsafe.Pointer) error {
	if len(frames) == 0 {
		return nil
	}

	for _, ptr := range frames {
		if ptr == nil {
			return NewCPUKernelError(kernel.KernelErrNilPointer, nil, "ExecutePointers")
		}

		func() {
			frameWords := (*[128]uint64)(ptr)
			value := (*uint64)(ptr)

			defer kernel.FinishFramePostALU(frameWords)

			rawOpcode := frameWords[kernel.ProgramOpcodeWord] & 0xFF
			opcode := rawOpcode & kernel.OpcodeBooleanMask
			contract := (frameWords[kernel.ProgramModeWord] >> kernel.ProgramContractShift) & 0xFF
			batchCount := frameWords[kernel.NearestAffinityBatchWord]

			if batchCount > uint64(kernel.MaxNearestAffinityCandidates) {
				batchCount = uint64(kernel.MaxNearestAffinityCandidates)
			}

			if kernel.IsGeometricOpcode(rawOpcode) {
				if geometricFrame(value, rawOpcode) {
					return
				}
			}

			if kernel.IsCopyMaskMergeOpcode(rawOpcode) {
				kernel.ApplyCopyMaskMerge(frameWords)

				return
			}

			if kernel.IsEmitCloneOpcode(rawOpcode) {
				primitive.EmitCloneHost((*primitive.Value)(ptr))

				return
			}

			if rawOpcode == kernel.OpcodeRegionProgram {
				for offset := 0; offset < regionTableWords; offset += regionEntryWords {
					op := frameWords[kernel.AssetStartWord+offset]
					if op == 0 && offset > 0 {
						break
					}

					rotationTable := frameWords[kernel.AssetStartWord+offset+1]
					if rotationTable == 0 {
						continue
					}

					mode := int(frameWords[kernel.AssetStartWord+offset+2] & 0xFF)
					aStart, aSpan := kernel.UnpackRegionRef(frameWords[kernel.AssetStartWord+offset+3])
					bStart, bSpan := kernel.UnpackRegionRef(frameWords[kernel.AssetStartWord+offset+4])
					dstStart, dstSpan := kernel.UnpackRegionRef(frameWords[kernel.AssetStartWord+offset+5])

					universalBitwiseV2(
						value,
						aStart, aSpan,
						bStart, bSpan,
						dstStart, dstSpan,
						mode,
						rotationTable,
					)
				}

				return
			}

			if opcode == kernel.OpcodeXOR && batchCount > 0 {
				nearestBatchReduce(frameWords, batchCount)

				return
			}

			rotationTable := frameWords[kernel.ProgramRotTabWord]
			mode := int(frameWords[kernel.ProgramModeWord] & 0xFF)
			aStart, aSpan := kernel.UnpackRegionRef(frameWords[kernel.ProgramSrcAWord])
			bStart, bSpan := kernel.UnpackRegionRef(frameWords[kernel.ProgramSrcBWord])
			dstStart, dstSpan := kernel.UnpackRegionRef(frameWords[kernel.ProgramDstWord])

			if contract == kernel.ProgramContractExactBinary {
				backend.exactBinary(frameWords, opcode, aStart, aSpan, bStart, bSpan, dstStart, dstSpan)

				return
			}

			// An all-zero rotation table means the frame has no work for the
			// universal bitwise lane. Skip it instead of running a sweep that
			// would XOR zero bytes into dst[0..min(8,span)) for nothing.
			if rotationTable == 0 {
				return
			}

			universalBitwiseV2(
				value,
				aStart, aSpan,
				bStart, bSpan,
				dstStart, dstSpan,
				mode,
				rotationTable,
			)
		}()
	}

	return nil
}
