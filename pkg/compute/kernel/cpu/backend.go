package cpu

import (
	"context"
	"runtime"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core"
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

// Program words are relative to core.Cfg.Value.Region.Program: opcode nibble,
// mode, (reserved), srcA ref, srcB ref, dst ref.
const (
	prOpcode = 0
	prMode   = 1
	prSrcA   = 3
	prSrcB   = 4
	prDst    = 5
)

func minProgramWordsForALU() int { return prDst + 1 }

// broadcastOpcodeNibble repeats the low 4-bit truth-table opcode into all 16
// rotation slots (single-opcode schedule for the universal ALU).
func broadcastOpcodeNibble(opcodeLow uint64) uint64 {
	nibble := opcodeLow & 0xF
	var packed uint64

	for rotation := 0; rotation < 16; rotation++ {
		packed |= nibble << (rotation * 4)
	}

	return packed
}

func executeValueFrame(frameWords *[128]uint64, value *uint64) {
	progStart, progN := core.Cfg.Value.Region.Program.WordExtent()
	if progN < minProgramWordsForALU() || progStart < 0 || progStart+prDst >= len(frameWords) {
		return
	}

	op := frameWords[progStart+prOpcode] & 0xF
	if op == 0 {
		return
	}

	rotationTable := broadcastOpcodeNibble(op)
	mode := int(frameWords[progStart+prMode] & 0xFF)
	aStart, aSpan := kernel.UnpackRegionRef(frameWords[progStart+prSrcA])
	bStart, bSpan := kernel.UnpackRegionRef(frameWords[progStart+prSrcB])
	dstStart, dstSpan := kernel.UnpackRegionRef(frameWords[progStart+prDst])

	universalBitwiseSweep(
		value,
		aStart, aSpan,
		bStart, bSpan,
		dstStart, dstSpan,
		mode,
		rotationTable,
	)
}

/*
Execute walks each Value and runs one universal bitwise pass: opcode low nibble
selects the truth-table op (broadcast to all rotations), mode and operand refs
come from the configured program region.
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
ExecutePointers runs the CPU ALU on host pointers (heap Values and tests).
*/
func (backend *Backend) ExecutePointers(frames []unsafe.Pointer) error {
	if len(frames) == 0 {
		return nil
	}

	for _, ptr := range frames {
		if ptr == nil {
			return NewCPUKernelError(kernel.KernelErrNilPointer, nil, "ExecutePointers")
		}

		frameWords := (*[128]uint64)(ptr)
		value := (*uint64)(ptr)

		executeValueFrame(frameWords, value)
	}

	return nil
}
