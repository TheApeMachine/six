package macro

import (
	"sync"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
)

func TestMacroIndex(t *testing.T) {
	gc.Convey("Given a fresh MacroIndex", t, func() {
		idx := NewMacroIndexServer()

		gc.Convey("FindOpcode should return false for an unknown shift", func() {
			_, found := idx.FindOpcode(numeric.Phase(42))
			gc.So(found, gc.ShouldBeFalse)
		})

		gc.Convey("RecordOpcode should create a new opcode with UseCount 1", func() {
			idx.RecordOpcode(numeric.Phase(42))

			op, found := idx.FindOpcode(numeric.Phase(42))
			gc.So(found, gc.ShouldBeTrue)
			gc.So(op.Rotation, gc.ShouldEqual, numeric.Phase(42))
			gc.So(op.UseCount, gc.ShouldEqual, uint64(1))
			gc.So(op.Hardened, gc.ShouldBeFalse)
		})

		gc.Convey("RecordOpcode should increment UseCount on repeated recording", func() {
			for range 4 {
				idx.RecordOpcode(numeric.Phase(77))
			}

			op, found := idx.FindOpcode(numeric.Phase(77))
			gc.So(found, gc.ShouldBeTrue)
			gc.So(op.UseCount, gc.ShouldEqual, uint64(4))
			gc.So(op.Hardened, gc.ShouldBeFalse)
		})

		gc.Convey("RecordOpcode should harden after threshold (>5 uses)", func() {
			for range 6 {
				idx.RecordOpcode(numeric.Phase(99))
			}

			op, found := idx.FindOpcode(numeric.Phase(99))
			gc.So(found, gc.ShouldBeTrue)
			gc.So(op.UseCount, gc.ShouldEqual, uint64(6))
			gc.So(op.Hardened, gc.ShouldBeTrue)
		})

		gc.Convey("RecordOpcode at exactly 5 uses should NOT be hardened", func() {
			for range 5 {
				idx.RecordOpcode(numeric.Phase(88))
			}

			op, _ := idx.FindOpcode(numeric.Phase(88))
			gc.So(op.UseCount, gc.ShouldEqual, uint64(5))
			gc.So(op.Hardened, gc.ShouldBeFalse)
		})

		gc.Convey("Different shifts should create distinct opcodes", func() {
			idx.RecordOpcode(numeric.Phase(10))
			idx.RecordOpcode(numeric.Phase(20))

			op10, found10 := idx.FindOpcode(numeric.Phase(10))
			op20, found20 := idx.FindOpcode(numeric.Phase(20))

			gc.So(found10, gc.ShouldBeTrue)
			gc.So(found20, gc.ShouldBeTrue)
			gc.So(op10.Rotation, gc.ShouldNotEqual, op20.Rotation)
		})
	})
}

func TestMacroIndexAvailableHardened(t *testing.T) {
	gc.Convey("Given an index with mixed soft and hardened opcodes", t, func() {
		idx := NewMacroIndexServer()

		// Soft opcode (2 uses)
		idx.RecordOpcode(numeric.Phase(10))
		idx.RecordOpcode(numeric.Phase(10))

		// Hardened opcode (7 uses)
		for range 7 {
			idx.RecordOpcode(numeric.Phase(20))
		}

		// Another hardened opcode (8 uses)
		for range 8 {
			idx.RecordOpcode(numeric.Phase(30))
		}

		gc.Convey("AvailableHardened should return only hardened opcodes", func() {
			tools := idx.AvailableHardened()
			gc.So(len(tools), gc.ShouldEqual, 2)

			rotations := map[numeric.Phase]bool{}
			for _, tool := range tools {
				gc.So(tool.Hardened, gc.ShouldBeTrue)
				rotations[tool.Rotation] = true
			}

			gc.So(rotations[numeric.Phase(20)], gc.ShouldBeTrue)
			gc.So(rotations[numeric.Phase(30)], gc.ShouldBeTrue)
		})

		gc.Convey("AvailableHardened on empty index should return nil", func() {
			empty := NewMacroIndexServer()
			gc.So(empty.AvailableHardened(), gc.ShouldBeNil)
		})
	})
}

func TestMacroIndexGarbageCollect(t *testing.T) {
	gc.Convey("Given an index with single-use and multi-use opcodes", t, func() {
		idx := NewMacroIndexServer()

		// Single-use opcodes (should be pruned)
		idx.RecordOpcode(numeric.Phase(1))
		idx.RecordOpcode(numeric.Phase(2))
		idx.RecordOpcode(numeric.Phase(3))

		// Multi-use opcode (should survive)
		idx.RecordOpcode(numeric.Phase(50))
		idx.RecordOpcode(numeric.Phase(50))

		// Hardened opcode (should survive)
		for range 10 {
			idx.RecordOpcode(numeric.Phase(100))
		}

		gc.Convey("GarbageCollect should prune single-use opcodes and return count", func() {
			pruned := idx.GarbageCollect()
			gc.So(pruned, gc.ShouldEqual, 3)

			_, found1 := idx.FindOpcode(numeric.Phase(1))
			_, found2 := idx.FindOpcode(numeric.Phase(2))
			_, found3 := idx.FindOpcode(numeric.Phase(3))
			gc.So(found1, gc.ShouldBeFalse)
			gc.So(found2, gc.ShouldBeFalse)
			gc.So(found3, gc.ShouldBeFalse)
		})

		gc.Convey("Multi-use and hardened opcodes should survive GC", func() {
			idx.GarbageCollect()

			op50, found50 := idx.FindOpcode(numeric.Phase(50))
			gc.So(found50, gc.ShouldBeTrue)
			gc.So(op50.UseCount, gc.ShouldEqual, uint64(2))

			op100, found100 := idx.FindOpcode(numeric.Phase(100))
			gc.So(found100, gc.ShouldBeTrue)
			gc.So(op100.Hardened, gc.ShouldBeTrue)
		})

		gc.Convey("Second GC pass on a clean index should prune zero", func() {
			idx.GarbageCollect()
			gc.So(idx.GarbageCollect(), gc.ShouldEqual, 0)
		})
	})
}

func TestMacroIndexConcurrency(t *testing.T) {
	gc.Convey("Given concurrent writers and readers", t, func() {
		idx := NewMacroIndexServer()

		gc.Convey("Concurrent RecordOpcode and FindOpcode should not race", func() {
			var wg sync.WaitGroup

			for goroutine := range 20 {
				wg.Add(1)

				go func(shift int) {
					defer wg.Done()

					for range 100 {
						idx.RecordOpcode(numeric.Phase(shift % 50))
						idx.FindOpcode(numeric.Phase(shift % 50))
						idx.AvailableHardened()
					}
				}(goroutine)
			}

			wg.Wait()

			// If we get here without a race panic, the locking works.
			// Verify at least some opcodes exist.
			gc.So(len(idx.AvailableHardened()), gc.ShouldBeGreaterThan, 0)
		})
	})
}

// --- Benchmarks ---

func BenchmarkRecordOpcode(b *testing.B) {
	idx := NewMacroIndexServer()
	b.ResetTimer()

	for iter := 0; iter < b.N; iter++ {
		idx.RecordOpcode(numeric.Phase(iter % 256))
	}
}

func BenchmarkFindOpcode(b *testing.B) {
	idx := NewMacroIndexServer()

	for shift := range 256 {
		idx.RecordOpcode(numeric.Phase(shift))
	}

	b.ResetTimer()

	for iter := 0; iter < b.N; iter++ {
		idx.FindOpcode(numeric.Phase(iter % 256))
	}
}

func BenchmarkAvailableHardened(b *testing.B) {
	idx := NewMacroIndexServer()

	for shift := range 50 {
		for range 10 {
			idx.RecordOpcode(numeric.Phase(shift))
		}
	}

	b.ResetTimer()

	for iter := 0; iter < b.N; iter++ {
		idx.AvailableHardened()
	}
}

func BenchmarkGarbageCollect(b *testing.B) {
	b.ResetTimer()

	for iter := 0; iter < b.N; iter++ {
		b.StopTimer()

		idx := NewMacroIndexServer()

		for shift := range 200 {
			idx.RecordOpcode(numeric.Phase(shift))
		}

		b.StartTimer()
		idx.GarbageCollect()
	}
}


