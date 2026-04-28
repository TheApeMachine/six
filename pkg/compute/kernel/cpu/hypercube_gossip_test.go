package cpu

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

const (
	rot8OpcodeCopyB    = uint64(0x5)
	rot8AStart         = uint64(0)
	rot8ASpan          = uint64(1)
	rot8BStart         = uint64(0)
	rot8BSpan          = uint64(2)
	rot8DstStart       = uint64(32)
	rot8DstSpan        = uint64(1)
	rot8MaskStart      = uint64(72)
	rot8TopologyNext   = uint64(1)
	rot8BRotateOneByte = uint64(1)
)

func TestExecuteKernelGoRot8(t *testing.T) {
	Convey("Given a truth-table instruction with B byte rotation metadata", t, func() {
		var owner [128]uint64
		var peer [128]uint64

		owner[72] = ^uint64(0)
		peer[0] = 0x0807060504030201
		peer[1] = 0x100f0e0d0c0b0a09

		owner[ProgramStartWord] = packTestInstruction(
			rot8OpcodeCopyB,
			rot8AStart,
			rot8ASpan,
			rot8BStart,
			rot8BSpan,
			rot8DstStart,
			rot8DstSpan,
			rot8MaskStart,
			rot8TopologyNext,
			rot8BRotateOneByte,
		)

		Convey("When the kernel reads B", func() {
			backend := &Backend{}
			backend.executeKernelGo(&owner, ^uint64(0), []*[128]uint64{&peer}, 1, 0)

			Convey("It should rotate the selected B span by one byte before applying the opcode", func() {
				So(owner[32], ShouldEqual, uint64(0x0908070605040302))
			})
		})
	})
}

func BenchmarkExecuteKernelGoRot8(b *testing.B) {
	var owner [128]uint64
	var peer [128]uint64

	owner[72] = ^uint64(0)
	peer[0] = 0x0807060504030201
	peer[1] = 0x100f0e0d0c0b0a09

	owner[ProgramStartWord] = packTestInstruction(
		rot8OpcodeCopyB,
		rot8AStart,
		rot8ASpan,
		rot8BStart,
		rot8BSpan,
		rot8DstStart,
		rot8DstSpan,
		rot8MaskStart,
		rot8TopologyNext,
		rot8BRotateOneByte,
	)

	backend := &Backend{}
	community := []*[128]uint64{&peer}

	b.ReportAllocs()

	for b.Loop() {
		owner[32] = 0
		backend.executeKernelGo(&owner, ^uint64(0), community, 1, 0)
	}
}

/*
packTestInstruction mirrors the packed ALU word layout used by the CPU
kernel: op is 4 bits at [3:0], aStart 7 bits at [10:4], aSpan-1 7 bits at
[17:11], bStart 7 bits at [24:18], bSpan-1 7 bits at [31:25], dstStart 7
bits at [38:32], dstSpan-1 7 bits at [45:39], maskStart 7 bits at [52:46],
topology 2 bits at [56:55], and bRotate 3 bits at PredicateCondShift
[60:58]. Spans are encoded as span-1; topology is masked to two bits so the
test instruction follows the same field constraints as compiled programs.
*/
func packTestInstruction(op, aStart, aSpan, bStart, bSpan, dstStart, dstSpan, maskStart, topology, bRotate uint64) uint64 {
	var instr uint64
	instr |= op & 0xF
	instr |= (aStart & 0x7F) << 4
	instr |= ((aSpan - 1) & 0x7F) << 11
	instr |= (bStart & 0x7F) << 18
	instr |= ((bSpan - 1) & 0x7F) << 25
	instr |= (dstStart & 0x7F) << 32
	instr |= ((dstSpan - 1) & 0x7F) << 39
	instr |= (maskStart & 0x7F) << 46
	instr |= (topology & 3) << 55
	instr |= (bRotate & 7) << PredicateCondShift

	return instr
}
