package cpu

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestExecuteKernelGoRot8(t *testing.T) {
	Convey("Given a truth-table instruction with B byte rotation metadata", t, func() {
		var owner [128]uint64
		var peer [128]uint64

		owner[72] = ^uint64(0)
		peer[0] = 0x0807060504030201
		peer[1] = 0x100f0e0d0c0b0a09

		owner[ProgramStartWord] = packTestInstruction(
			0x5,
			0,
			1,
			0,
			2,
			32,
			1,
			72,
			1,
			1,
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
		0x5,
		0,
		1,
		0,
		2,
		32,
		1,
		72,
		1,
		1,
	)

	backend := &Backend{}
	community := []*[128]uint64{&peer}

	b.ReportAllocs()

	for b.Loop() {
		owner[32] = 0
		backend.executeKernelGo(&owner, ^uint64(0), community, 1, 0)
	}
}

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
