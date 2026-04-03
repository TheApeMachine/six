package cpu

import (
	"context"
	"math/bits"
	"runtime"
	"unsafe"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
)

type Backend struct {
	ctx    context.Context
	cancel context.CancelFunc
}

const universalBitwiseTileFrames = 64

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

func Available() int { return runtime.NumCPU() }

/*
UniversalBitwise executes the in-band 32-bit slot program carried by each Value.
The CPU path follows the same self-only execution contract as CUDA and Metal,
but it opportunistically uses SIMD across tiles of Values when a slot decodes
to the same instruction across the tile.
*/
func (backend *Backend) UniversalBitwise(frames []unsafe.Pointer) error {
	if len(frames) == 0 {
		return nil
	}

	for index := range frames {
		if frames[index] == nil {
			return NewBackendError(ErrNilValuePointer,
				"frame", frames[index], "i", index,
			)
		}
	}

	progStart := core.Cfg.Value.Region.Program.Start
	nProgWords := int((core.Cfg.Value.Region.Program.Bits + 63) / 64)
	totalSlots := nProgWords * 2
	tileSize := min(len(frames), universalBitwiseTileFrames)
	srcScratch := make([]uint64, tileSize)
	dstScratch := make([]uint64, tileSize)

	for start := 0; start < len(frames); start += tileSize {
		end := min(start+tileSize, len(frames))
		executeFrameTile(
			frames[start:end],
			progStart,
			totalSlots,
			srcScratch[:end-start],
			dstScratch[:end-start],
		)
	}

	return nil
}

func executeFrameTile(
	frames []unsafe.Pointer,
	progStart int,
	totalSlots int,
	srcScratch []uint64,
	dstScratch []uint64,
) {
	errnie.Debug(
		"cpu.Backend.executeFrameTile",
		"frames", frames,
		"progStart", progStart,
		"totalSlots", totalSlots,
		"srcScratch", srcScratch,
		"dstScratch", dstScratch,
	)

	if len(frames) == 0 || totalSlots <= 0 {
		return
	}

	for slot := range totalSlots {
		instr, homogeneous := tileInstruction(frames, progStart, slot)
		if homogeneous {
			if instr == 0 {
				continue
			}

			executeTileInstruction(frames, instr, srcScratch, dstScratch)
			continue
		}

		for index := range frames {
			executeScalarInstruction((*[128]uint64)(frames[index]), progStart, slot)
		}
	}
}

func tileInstruction(frames []unsafe.Pointer, progStart int, slot int) (uint32, bool) {
	first := instructionAtSlot((*[128]uint64)(frames[0]), progStart, slot)

	for index := 1; index < len(frames); index++ {
		frame := (*[128]uint64)(frames[index])
		if instructionAtSlot(frame, progStart, slot) != first {
			return first, false
		}
	}

	return first, true
}

func executeTileInstruction(
	frames []unsafe.Pointer,
	instr uint32,
	srcScratch []uint64,
	dstScratch []uint64,
) {
	op := uint8(instr & 0xF)
	srcWord := int((instr>>4)&0x3FFF) & 127
	dstWord := int((instr>>18)&0x3FFF) & 127

	for index := range frames {
		frame := (*[128]uint64)(frames[index])
		srcScratch[index] = frame[srcWord]
		dstScratch[index] = frame[dstWord]
	}

	execWordBlock(dstScratch, srcScratch, op)

	for index := range frames {
		frame := (*[128]uint64)(frames[index])
		frame[dstWord] = dstScratch[index]
	}
}

func executeScalarInstruction(frame *[128]uint64, progStart int, slot int) {
	if frame == nil {
		return
	}

	instr := instructionAtSlot(frame, progStart, slot)
	if instr == 0 {
		return
	}

	op := uint8(instr & 0xF)
	srcWord := int((instr>>4)&0x3FFF) & 127
	dstWord := int((instr>>18)&0x3FFF) & 127
	frame[dstWord] = ExecWord(op, frame[srcWord], frame[dstWord])
}

func instructionAtSlot(frame *[128]uint64, progStart int, slot int) uint32 {
	if frame == nil {
		return 0
	}

	wordIdx := progStart + slot/2
	if wordIdx < 0 || wordIdx >= len(frame) {
		return 0
	}

	shift := uint((slot % 2) * 32)
	return uint32(frame[wordIdx] >> shift)
}

/*
TruthTable applies a 4-bit opcode as the truth table it literally encodes.
Bit 0 is the output for (a=1, b=1), bit 1 for (1,0), bit 2 for (0,1),
and bit 3 for (0,0), matching cmd/cfg/config.yml.
Branchless so the compiler can emit SIMD for loops over []uint64.
*/
func TruthTable(op uint8, a, b uint64) uint64 {
	return (a & b & -uint64(op&1)) |
		(a & ^b & -uint64((op>>1)&1)) |
		(^a & b & -uint64((op>>2)&1)) |
		(^a & ^b & -uint64((op>>3)&1))
}

/*
ExecWord executes one opcode on a single lane.
Opcodes 0x0..0xF use the canonical truth-table fallback in TruthTable.
Extended opcodes are:
0x10 = popcount(x ^ y)
0x11 = logical left shift of y by (x & 63)
0x12 = logical right shift of y by (x & 63)
0x13 = x + y
*/
func ExecWord(op uint8, x, y uint64) uint64 {
	switch op {
	case 0x10:
		return uint64(bits.OnesCount64(x ^ y))
	case 0x11:
		return y << (x & 63)
	case 0x12:
		return y >> (x & 63)
	case 0x13:
		return x + y
	}

	return TruthTable(op, x, y)
}

func (backend *Backend) Shutdown() error {
	backend.cancel()
	return nil
}

func (backend *Backend) Schedule(job func(ctx context.Context) error) error {
	return job(context.Background())
}

func Popcount(value unsafe.Pointer, startBit, bitLen int) int {
	contexts := (*[128]uint64)(value)

	if bitLen <= 0 {
		return 0
	}

	startWord := startBit >> 6
	startShift := startBit & 63
	remaining := bitLen
	total := 0
	word := startWord
	shift := startShift

	for remaining > 0 {
		chunk := min(64-shift, remaining)

		var lane uint64
		lane = contexts[word] >> uint(shift)

		if shift > 0 && word+1 < 128 {
			val := contexts[word+1]
			lane |= val << uint(64-shift)
		}

		mask := uint64(1<<chunk) - 1
		if chunk == 64 {
			mask = ^uint64(0)
		}

		total += bits.OnesCount64(lane & mask)

		remaining -= chunk
		word++
		shift = 0
	}

	return total
}
