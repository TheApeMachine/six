package cpu

import (
	"context"
	"math/bits"
	"runtime"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/firmware"
	"github.com/theapemachine/six/pkg/core"
)

const (
	// Deterministic affine LCG parameters for follow-up program selection.
	affineFollowUpMultiplier = uint64(6364136223846793005)
	affineFollowUpAddend     = uint64(1)
	affineFollowUpModulus    = 6
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

func Available() int { return runtime.NumCPU() }

/*
UniversalBitwise executes multiple value's in-band programs in parallel.
This uses the branchless and loop-less implementation programming model,
where every line of code follows the exact same execution mechanism,
and applying the opcode uses the actual truth table from the code to
shift the bits. This allows for the most efficient execution of the
in-band programs, across all hardware architectures, and ALU
implementations, keeping the entire backend homogeneous.
To not lose branching and looping capabilities, we will need a
few crucial changes to the programming model:

 1. No HALT instruction, each program is always executed from start to
    end of the program region, and the compiler will need to insert a
    NOP instruction for any lines that are empty.
 2. The final lines of the program region will need an instruction to
    shift the bits in such a way that a follow-up program can be selected.
    When the program ends, and has a follow-up of LOOP or JMP, it will be
    rescheduled onto the priority queue for another round of execution.
 3. Values ONLY operate on themselves, and there is NO partner Value.
    That means NO partner data is ever read or written at ANY stage.
    Values use their own Token region as the data to operate on.
*/
func (backend *Backend) UniversalBitwise(frames []unsafe.Pointer) error {
	n := len(frames)
	if n == 0 {
		return nil
	}

	for index := range frames {
		if frames[index] == nil {
			return NewSimdeezNutsError(ErrNilValuePointer,
				"frame", frames[index], "i", index,
			)
		}
	}

	progStart := core.Cfg.Value.Region.Program.Start
	nProgWords := int((core.Cfg.Value.Region.Program.Bits + 63) / 64)
	if hasHomogeneousImmutableProgram(frames, progStart, nProgWords) {
		executeHomogeneousProgram(frames, progStart, nProgWords)
	} else {
		executePerSlotGroups(frames, progStart, nProgWords)
	}

	// --- Affine follow-up: compute next program ID and schedule signature ---
	accWord := core.Cfg.Value.Region.State.Accumulator
	fwWord := core.Cfg.Value.Region.Registers.FW

	for i := 0; i < n; i++ {
		f := (*[128]uint64)(frames[i])
		acc := f[accWord]

		// Affine jump vector: deterministic, non-repeating orbit through programs.
		nextID := firmware.AffineNextProgramID(acc, affineFollowUpMultiplier, affineFollowUpAddend, affineFollowUpModulus)

		// Holographic schedule signature: mix program ID with data structure.
		scanStride := f[core.Cfg.Value.Region.State.Sequence]
		sig := firmware.HolographicScheduleSignature(nextID, 0, scanStride)

		// Write follow-up into the frame for the outer scheduler to read.
		f[fwWord] = sig
		f[accWord] = acc ^ sig // Evolve accumulator so the orbit advances.
	}

	return nil
}

func hasHomogeneousImmutableProgram(frames []unsafe.Pointer, progStart int, nProgWords int) bool {
	if len(frames) == 0 {
		return false
	}

	first := (*[128]uint64)(frames[0])
	if programWritesProgramRegion(first, progStart, nProgWords) {
		return false
	}

	for index := 1; index < len(frames); index++ {
		frame := (*[128]uint64)(frames[index])
		if !sameProgramRegion(first, frame, progStart, nProgWords) {
			return false
		}
	}

	return true
}

func programWritesProgramRegion(frame *[128]uint64, progStart int, nProgWords int) bool {
	if frame == nil || nProgWords <= 0 {
		return false
	}

	progEnd := progStart + nProgWords
	totalSlots := nProgWords * 2

	for slot := 0; slot < totalSlots; slot++ {
		instr := instructionAtSlot(frame, progStart, slot)
		if instr == 0 {
			continue
		}

		dstWord := int((instr >> 18) & 0x3FFF)
		if dstWord >= progStart && dstWord < progEnd {
			return true
		}
	}

	return false
}

func sameProgramRegion(left, right *[128]uint64, progStart int, nProgWords int) bool {
	if left == nil || right == nil {
		return false
	}

	for word := 0; word < nProgWords; word++ {
		index := progStart + word
		if index < 0 || index >= len(left) || index >= len(right) {
			return false
		}

		if left[index] != right[index] {
			return false
		}
	}

	return true
}

func executeHomogeneousProgram(frames []unsafe.Pointer, progStart int, nProgWords int) {
	totalSlots := nProgWords * 2
	srcScratch := make([]uint64, len(frames))
	dstScratch := make([]uint64, len(frames))
	progFrame := (*[128]uint64)(frames[0])

	for slot := 0; slot < totalSlots; slot++ {
		instr := instructionAtSlot(progFrame, progStart, slot)
		if instr == 0 {
			continue
		}

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
			(*[128]uint64)(frames[index])[dstWord] = dstScratch[index]
		}
	}
}

func executePerSlotGroups(frames []unsafe.Pointer, progStart int, nProgWords int) {
	totalSlots := nProgWords * 2
	srcScratch := make([]uint64, len(frames))
	dstScratch := make([]uint64, len(frames))
	groupIndexByInstr := make(map[uint32]int, 8)

	for slot := 0; slot < totalSlots; slot++ {
		groupInstrs := make([]uint32, 0, 8)
		groupFrameIndexes := make([][]int, 0, 8)

		clear(groupIndexByInstr)

		for frameIndex := range frames {
			frame := (*[128]uint64)(frames[frameIndex])
			instr := instructionAtSlot(frame, progStart, slot)
			if instr == 0 {
				continue
			}

			groupIndex, seen := groupIndexByInstr[instr]
			if !seen {
				groupIndex = len(groupInstrs)
				groupIndexByInstr[instr] = groupIndex
				groupInstrs = append(groupInstrs, instr)
				groupFrameIndexes = append(groupFrameIndexes, nil)
			}

			groupFrameIndexes[groupIndex] = append(groupFrameIndexes[groupIndex], frameIndex)
		}

		for groupIndex, instr := range groupInstrs {
			frameIndexes := groupFrameIndexes[groupIndex]
			executeInstructionBatch(
				frames,
				frameIndexes,
				instr,
				srcScratch[:len(frameIndexes)],
				dstScratch[:len(frameIndexes)],
			)
		}
	}
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

func executeInstructionBatch(
	frames []unsafe.Pointer,
	frameIndexes []int,
	instr uint32,
	srcScratch []uint64,
	dstScratch []uint64,
) {
	if len(frameIndexes) == 0 {
		return
	}

	op := uint8(instr & 0xF)
	srcWord := int((instr>>4)&0x3FFF) & 127
	dstWord := int((instr>>18)&0x3FFF) & 127

	for index, frameIndex := range frameIndexes {
		frame := (*[128]uint64)(frames[frameIndex])
		srcScratch[index] = frame[srcWord]
		dstScratch[index] = frame[dstWord]
	}

	execWordBlock(dstScratch, srcScratch, op)

	for index, frameIndex := range frameIndexes {
		(*[128]uint64)(frames[frameIndex])[dstWord] = dstScratch[index]
	}
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
