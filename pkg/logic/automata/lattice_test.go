package automata

import (
	"context"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
)

/*
TestLatticeUpdateNoHardened verifies that Update records a candidate
when the MacroIndex contains no hardened opcode for the cell-neighbor delta.
*/
func TestLatticeUpdateNoHardened(t *testing.T) {
	gc.Convey("Given a lattice with an empty macro index", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		server := macro.NewMacroIndexServer(macro.MacroIndexWithContext(ctx))
		defer server.Close()

		lattice := NewLattice(LatticeWithOpcodes(server))
		cell := primitive.BaseValue(42)
		neighbor := primitive.BaseValue(99)

		gc.Convey("It should record a candidate without changing cell state", func() {
			originalCarry := cell.ResidualCarry()
			result, changed := lattice.Update(cell, []primitive.Value{neighbor})

			gc.So(changed, gc.ShouldBeFalse)
			gc.So(result.ResidualCarry(), gc.ShouldEqual, originalCarry)

			key := macro.AffineKeyFromValues(cell, neighbor)
			opcode, found := server.FindOpcode(key)
			gc.So(found, gc.ShouldBeTrue)
			gc.So(opcode.Hardened, gc.ShouldBeFalse)
		})
	})
}

/*
TestLatticeUpdateHardened verifies that Update applies a hardened opcode's
affine transform to the cell's phase when one exists for the delta.
*/
func TestLatticeUpdateHardened(t *testing.T) {
	gc.Convey("Given a lattice with a hardened opcode for the cell-neighbor delta", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		server := macro.NewMacroIndexServer(macro.MacroIndexWithContext(ctx))
		defer server.Close()

		cell := primitive.BaseValue(42)
		neighbor := primitive.BaseValue(99)

		key := macro.AffineKeyFromValues(cell, neighbor)
		opcode := macro.OpcodeForKey(key)
		opcode.Hardened = true
		opcode.UseCount = 10
		server.StoreOpcode(opcode)

		lattice := NewLattice(LatticeWithOpcodes(server))

		gc.Convey("It should apply the opcode and report state change", func() {
			result, changed := lattice.Update(cell, []primitive.Value{neighbor})

			gc.So(changed, gc.ShouldBeTrue)
			gc.So(result.ResidualCarry(), gc.ShouldEqual, uint64(opcode.Translate))
		})
	})
}

/*
TestLatticeHammingDelta verifies XOR popcount between distinct values
produces a non-zero distance.
*/
func TestLatticeHammingDelta(t *testing.T) {
	gc.Convey("Given two distinct base values", t, func() {
		lattice := NewLattice()
		cellA := primitive.BaseValue(0)
		cellB := primitive.BaseValue(255)

		gc.Convey("It should report a non-zero Hamming distance", func() {
			delta := lattice.HammingDelta(cellA, cellB)
			gc.So(delta, gc.ShouldBeGreaterThan, 0)
		})
	})
}

/*
TestLatticeHammingDeltaIdentical verifies zero distance for identical values.
*/
func TestLatticeHammingDeltaIdentical(t *testing.T) {
	gc.Convey("Given two identical base values", t, func() {
		lattice := NewLattice()
		cellA := primitive.BaseValue(42)
		cellB := primitive.BaseValue(42)

		gc.Convey("It should report zero Hamming distance", func() {
			delta := lattice.HammingDelta(cellA, cellB)
			gc.So(delta, gc.ShouldEqual, 0)
		})
	})
}

/*
BenchmarkLatticeUpdate measures Update throughput on the candidate recording path.
*/
func BenchmarkLatticeUpdate(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := macro.NewMacroIndexServer(macro.MacroIndexWithContext(ctx))
	defer server.Close()

	lattice := NewLattice(LatticeWithOpcodes(server))
	cell := primitive.BaseValue(42)
	neighbors := []primitive.Value{primitive.BaseValue(99)}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		lattice.Update(cell, neighbors)
	}
}

/*
BenchmarkLatticeHammingDelta measures XOR popcount throughput.
*/
func BenchmarkLatticeHammingDelta(b *testing.B) {
	lattice := NewLattice()
	cellA := primitive.BaseValue(42)
	cellB := primitive.BaseValue(99)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		lattice.HammingDelta(cellA, cellB)
	}
}
